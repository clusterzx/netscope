package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// testServer is a minimal SSH server answering every exec request with a captured
// script output.
type testServer struct {
	addr      *net.TCPAddr
	password  string
	clientKey xssh.PublicKey
	stdout    []byte
	stderr    []byte

	passwordTries atomic.Int64
	keyLogins     atomic.Int64
	mu            sync.Mutex
	commands      []string
}

func newTestServer(t *testing.T, stdout, stderr []byte, clientKey xssh.PublicKey) *testServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &testServer{addr: ln.Addr().(*net.TCPAddr), password: "richtig", clientKey: clientKey, stdout: stdout, stderr: stderr}
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := xssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &xssh.ServerConfig{
		PasswordCallback: func(c xssh.ConnMetadata, pw []byte) (*xssh.Permissions, error) {
			s.passwordTries.Add(1)
			if c.User() == "root" && string(pw) == s.password {
				return nil, nil
			}
			return nil, errDenied
		},
		PublicKeyCallback: func(c xssh.ConnMetadata, key xssh.PublicKey) (*xssh.Permissions, error) {
			if c.User() == "root" && s.clientKey != nil && string(key.Marshal()) == string(s.clientKey.Marshal()) {
				s.keyLogins.Add(1)
				return nil, nil
			}
			return nil, errDenied
		},
	}
	cfg.AddHostKey(hostKey)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, cfg)
		}
	}()
	return s
}

type deniedError struct{}

func (deniedError) Error() string { return "denied" }

var errDenied = deniedError{}

func (s *testServer) serve(conn net.Conn, cfg *xssh.ServerConfig) {
	defer conn.Close()
	_, chans, reqs, err := xssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go xssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(xssh.UnknownChannelType, "nur session")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				n := binary.BigEndian.Uint32(req.Payload[:4])
				s.mu.Lock()
				s.commands = append(s.commands, string(req.Payload[4:4+n]))
				s.mu.Unlock()
				_ = req.Reply(true, nil)
				_, _ = ch.Write(s.stdout)
				_, _ = ch.Stderr().Write(s.stderr)
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				return
			}
		}()
	}
}

func clientKeyPEM(t *testing.T) (string, xssh.PublicKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := xssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := xssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), sshPub
}

func TestRunWithTestServer(t *testing.T) {
	keyPEM, pub := clientKeyPEM(t)
	srv := newTestServer(t, plugintest.Fixture(t, "ubuntu2404.out"), plugintest.Fixture(t, "ubuntu2404.err"), pub)
	p := &Plugin{}
	settings := map[string]any{"credentials": []any{1, 2}, "port": srv.addr.Port}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	rc.Creds = plugintest.Creds{
		1: {ID: 1, Name: "falsch", Type: plugin.CredPassword, Public: map[string]string{"username": "root"}, Secret: map[string]string{"password": "falsch"}},
		2: {ID: 2, Name: "schlüssel", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}, Secret: map[string]string{"private_key": keyPEM}},
	}
	rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{ID: 1, CIDR: netip.MustParsePrefix("192.168.8.0/24")}}}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{
		{ID: 7, Name: "netscope", PrimaryIP: "127.0.0.1", Ports: []plugin.PortRef{{IP: "127.0.0.1", Proto: "tcp", Port: srv.addr.Port}}},
		{ID: 8, Name: "drucker", PrimaryIP: "127.0.0.2", Ports: []plugin.PortRef{{IP: "127.0.0.2", Proto: "tcp", Port: 9100}}},
	}}
	ctx := context.Background()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 1 {
		t.Fatalf("observations = %d (device without open ssh port must be skipped)", len(obs))
	}
	o := obs[0]
	if o.DeviceID != 7 || o.IP != "127.0.0.1" || !o.Present || o.Hostname != "netscope" || o.Packages == nil ||
		len(o.Packages.Packages) != 367 || o.Containers == nil || len(o.Containers.Containers) != 2 || o.OS.Name != "Ubuntu 24.04 LTS" {
		t.Errorf("observation = %+v", o)
	}
	if len(o.IPs) != 1 || o.IPs[0] != "192.168.8.123" || len(o.MACs) != 0 {
		t.Errorf("ips = %v, macs = %v", o.IPs, o.MACs)
	}
	if stats := rc.Stats(); stats["scanned"] != 1 || stats["skipped"] != 1 {
		t.Errorf("stats = %v", stats)
	}
	if srv.passwordTries.Load() == 0 || srv.keyLogins.Load() != 1 {
		t.Errorf("password tries = %d, key logins = %d", srv.passwordTries.Load(), srv.keyLogins.Load())
	}
	srv.mu.Lock()
	cmd := srv.commands[0]
	srv.mu.Unlock()
	if !strings.HasPrefix(cmd, "sh -c '") || !strings.Contains(cmd, "dpkg-query -W") || !strings.Contains(cmd, "docker ps -a") {
		t.Errorf("command = %.200s", cmd)
	}
	// the working credential is remembered, the host key pinned
	store, err := loadCredStore(filepath.Join(rc.DataDir, "credentials.json"))
	if err != nil || store.get(7) != 2 {
		t.Errorf("remembered = %d, %v", store.get(7), err)
	}
	kh, err := os.ReadFile(filepath.Join(rc.DataDir, "known_hosts"))
	if err != nil || !strings.Contains(string(kh), "[127.0.0.1]:") {
		t.Errorf("known_hosts = %q, %v", kh, err)
	}

	// second run: the remembered credential is tried first, no password attempt
	tries := srv.passwordTries.Load()
	rc2, sink2, _ := plugintest.RunContext(t, p, settings)
	rc2.DataDir, rc2.Creds, rc2.Inventory, rc2.Targets = rc.DataDir, rc.Creds, rc.Inventory, rc.Targets
	if err := p.Run(ctx, rc2); err != nil {
		t.Fatal(err)
	}
	if len(sink2.All()) != 1 || srv.passwordTries.Load() != tries {
		t.Errorf("observations = %d, password tries %d -> %d", len(sink2.All()), tries, srv.passwordTries.Load())
	}

	// a different server on the same address:port has another host key: rejected
	srv2 := newTestServer(t, plugintest.Fixture(t, "ubuntu2404.out"), nil, pub)
	rc3, sink3, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{2}, "port": srv2.addr.Port})
	rc3.DataDir, rc3.Creds, rc3.Inventory = rc.DataDir, rc.Creds, rc.Inventory
	rc3.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 7, PrimaryIP: "127.0.0.1"}}}
	knownPath := filepath.Join(rc.DataDir, "known_hosts")
	pinned := strings.ReplaceAll(string(kh), "]:"+itoa(srv.addr.Port)+" ", "]:"+itoa(srv2.addr.Port)+" ")
	if err := os.WriteFile(knownPath, []byte(pinned), 0o600); err != nil {
		t.Fatal(err)
	}
	err = p.Run(ctx, rc3)
	if err == nil || len(sink3.All()) != 0 {
		t.Fatalf("changed host key must fail the run: err = %v, observations = %d", err, len(sink3.All()))
	}

	// insecure policy ignores the pinned key
	rc4, sink4, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{2}, "port": srv2.addr.Port, "host_key_policy": "insecure"})
	rc4.DataDir, rc4.Creds, rc4.Inventory, rc4.Targets = rc.DataDir, rc.Creds, rc.Inventory, rc3.Targets
	if err := p.Run(ctx, rc4); err != nil || len(sink4.All()) != 1 {
		t.Fatalf("insecure: err = %v, observations = %d", err, len(sink4.All()))
	}
}

func TestRunAllCredentialsRejected(t *testing.T) {
	_, pub := clientKeyPEM(t)
	srv := newTestServer(t, plugintest.Fixture(t, "ubuntu2404.out"), nil, pub)
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{1}, "port": srv.addr.Port})
	rc.Creds = plugintest.Creds{
		1: {ID: 1, Name: "falsch", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}, Secret: map[string]string{"password": "nope"}},
	}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 3, PrimaryIP: "127.0.0.1"}}}
	err := p.Run(context.Background(), rc)
	if err == nil || len(sink.All()) != 0 {
		t.Fatalf("err = %v, observations = %d", err, len(sink.All()))
	}
	if rc.Stats()["failed"] != 1 {
		t.Errorf("stats = %v", rc.Stats())
	}
}

func TestRunWithoutTargets(t *testing.T) {
	p := &Plugin{}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{1}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "x", Type: plugin.CredPassword, Public: map[string]string{"username": "u"}, Secret: map[string]string{"password": "p"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	rc.Creds = plugintest.Creds{}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 3, PrimaryIP: "127.0.0.1"}}}
	if err := p.Run(context.Background(), rc); !errors.Is(err, plugin.ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	if rc.Stats()["no_credential"] != 1 || rc.Stats()["failed"] != nil {
		t.Errorf("stats = %v", rc.Stats())
	}
}

func TestPlanTargets(t *testing.T) {
	devs := []plugin.DeviceInfo{
		{ID: 1, PrimaryIP: "10.0.0.1"}, // no port data: tried
		{ID: 2, PrimaryIP: "10.0.0.2", Ports: []plugin.PortRef{{IP: "10.0.0.2", Proto: "tcp", Port: 80}}},                                            // skipped
		{ID: 3, PrimaryIP: "10.0.0.3", Ports: []plugin.PortRef{{IP: "10.0.0.33", Proto: "tcp", Port: 22}, {IP: "10.0.0.3", Proto: "udp", Port: 22}}}, // other ip
		{ID: 4, IPs: []string{"10.0.0.4"}},
		{ID: 5},
	}
	got, skipped := planTargets(devs, portPlan{port: 22, requireOpen: true})
	if skipped != 2 || len(got) != 3 || got[0].ip != "10.0.0.1" || got[1].ip != "10.0.0.33" || got[2].ip != "10.0.0.4" || got[0].port != 22 {
		t.Errorf("targets = %+v, skipped = %d", got, skipped)
	}
	got, skipped = planTargets(devs, portPlan{port: 22})
	if skipped != 1 || len(got) != 4 {
		t.Errorf("without port requirement: %+v, skipped = %d", got, skipped)
	}
}

func TestPlanTargetsPorts(t *testing.T) {
	overrides, err := parsePortOverrides([]string{" 10.0.5.0/24 = 2200 ", "10.0.5.9=2222", ""})
	if err != nil {
		t.Fatal(err)
	}
	devs := []plugin.DeviceInfo{
		// explicit port: always tried, even without open port data
		{ID: 1, PrimaryIP: "10.0.5.9", Ports: []plugin.PortRef{{IP: "10.0.5.9", Proto: "tcp", Port: 443}}},
		{ID: 2, PrimaryIP: "10.0.5.10"},
		// nmap found SSH on 2222 while 22 is closed
		{ID: 3, PrimaryIP: "10.0.0.3", Ports: []plugin.PortRef{{IP: "10.0.0.3", Proto: "tcp", Port: 2222, Service: "ssh", Product: "OpenSSH"}}},
		// 22 open and another SSH port: the configured port wins
		{ID: 4, PrimaryIP: "10.0.0.4", Ports: []plugin.PortRef{{IP: "10.0.0.4", Proto: "tcp", Port: 2022, Service: "ssh"},
			{IP: "10.0.0.4", Proto: "tcp", Port: 22, Service: "ssh"}}},
	}
	got, skipped := planTargets(devs, portPlan{port: 22, requireOpen: true, detect: true, overrides: overrides})
	ports := map[int64]int{}
	for _, g := range got {
		ports[g.dev.ID] = g.port
	}
	if skipped != 0 || ports[1] != 2222 || ports[2] != 2200 || ports[3] != 2222 || ports[4] != 22 {
		t.Errorf("ports = %v, skipped = %d", ports, skipped)
	}
	// without detection the device with SSH on 2222 only is skipped
	if _, skipped := planTargets(devs[2:3], portPlan{port: 22, requireOpen: true}); skipped != 1 {
		t.Errorf("detection off: skipped = %d", skipped)
	}
	for _, bad := range []string{"10.0.0.1", "10.0.0.1=0", "host=22", "10.0.0.0/33=22"} {
		if _, err := parsePortOverrides([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := (&Plugin{}).ValidateSettings(plugin.NewSettings(map[string]any{"port_overrides": []any{"x"}})); err == nil {
		t.Error("invalid override saved")
	}
}

func TestCredStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "credentials.json")
	s, err := loadCredStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.set(1, 5)
	s.set(2, 6)
	s.set(2, 0)
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, err := loadCredStore(path)
	if err != nil || s2.get(1) != 5 || s2.get(2) != 0 {
		t.Errorf("reloaded: %v, %v", s2.m, err)
	}
	if err := os.WriteFile(path, []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s3, err := loadCredStore(path); err == nil || s3 == nil || len(s3.m) != 0 {
		t.Error("damaged file must give an empty store and an error")
	}
	ids := orderedCredentials(3, []int64{1, 2, 3, 4}, func(i int64) int64 { return i })
	if ids[0] != 3 || ids[1] != 1 || ids[3] != 4 {
		t.Errorf("order = %v", ids)
	}
}

func TestBuildScript(t *testing.T) {
	full := buildScript(scriptOptions{Packages: true, Docker: true, CommandTimeout: 20 * time.Second})
	for _, want := range []string{"timeout 20", "dpkg-query -W", "rpm -qa --qf", "apk list -I", "docker ps -a --no-trunc", "ss -tulpnH", "ip -j addr"} {
		if !strings.Contains(full, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	lean := buildScript(scriptOptions{CommandTimeout: 1500 * time.Millisecond})
	for _, unwanted := range []string{"dpkg-query -W", "rpm -qa --qf", "apk list", "docker ps", "docker images"} {
		if strings.Contains(lean, unwanted) {
			t.Errorf("lean script contains %q", unwanted)
		}
	}
	if !strings.Contains(lean, "timeout 1\"") || !strings.Contains(lean, "rpm -qa --last") {
		t.Error("lean script must keep last-update sections and a timeout of at least 1s")
	}
}

func TestSchemaDefaults(t *testing.T) {
	p := &Plugin{}
	if err := p.Schema().Check(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Schema().Validate(map[string]any{}, nil, nil); err != nil {
		t.Errorf("empty credentials select automatically: %v", err)
	}
	cfg := loadConfig(plugin.NewSettings(p.Schema().Defaults()), "/data/plugins/ssh")
	if cfg.ports.port != 22 || !cfg.ports.requireOpen || !cfg.ports.detect || len(cfg.ports.overrides) != 0 || cfg.commandTimeout != 20*time.Second || !cfg.packages || !cfg.docker ||
		cfg.maxOutput != 16<<20 || cfg.knownHosts != filepath.Join("/data/plugins/ssh", "known_hosts") {
		t.Errorf("defaults = %+v", cfg)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
