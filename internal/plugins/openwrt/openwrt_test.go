package openwrt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestParseLeases(t *testing.T) {
	leases := parseLeases(plugintest.Fixture(t, "dhcp.leases"))
	if len(leases) != 7 {
		t.Fatalf("got %d leases, want 7", len(leases))
	}
	first := leases[0]
	if first.MAC != "7c:2e:0d:4a:91:3f" || first.IP != "192.168.8.143" || first.Hostname != "iPhone-von-Anna" ||
		first.ClientID != "01:7c:2e:0d:4a:91:3f" || !first.Expires.Equal(time.Unix(1790155421, 0)) || first.Infinite {
		t.Errorf("lease 0: %+v", first)
	}
	if l := leases[2]; l.Hostname != "" || l.MAC != "da:a1:19:6e:33:07" {
		t.Errorf("hostname * must be empty: %+v", l)
	}
	if l := leases[3]; !l.Infinite || !l.Expires.IsZero() || l.Hostname != "nas" {
		t.Errorf("expiry 0 must be infinite: %+v", l)
	}
	if l := leases[5]; l.ClientID != "" || l.Hostname != "ESP-8C021E" {
		t.Errorf("client id * must be empty: %+v", l)
	}
	// DHCPv6 entries of dnsmasq (duid line, IAID instead of MAC) are skipped
	v6 := "duid 00:01:00:01:2c:5f:3a:10:a8:a1:59:3c:7e:10\n1790155421 1234567 fd00::1:5 host6 00:01:00:01:2c:5f\n"
	if got := parseLeases([]byte(v6)); len(got) != 0 {
		t.Errorf("v6 leases: %+v", got)
	}
}

func TestParseUCIShow(t *testing.T) {
	sections := parseUCIShow(plugintest.Fixture(t, "uci-show-dhcp.txt"))
	if got := leaseFiles(sections); !reflect.DeepEqual(got, []string{"/tmp/dhcp.leases"}) {
		t.Errorf("lease files: %v", got)
	}
	hosts := staticHosts(sections)
	want := []staticHost{
		{Section: "@host[0]", Name: "nas", MACs: []string{"00:11:32:ab:cd:ef"}, IP: "192.168.8.5", DNS: true, LeaseTime: "infinite"},
		{Section: "@host[1]", Name: "pve1", MACs: []string{"a8:a1:59:3c:7e:10", "a8:a1:59:3c:7e:11"}, IP: "192.168.8.20"},
		{Section: "@host[2]", Name: "laptop-max", MACs: []string{"f4:5c:89:b1:22:0d", "d4:81:d7:6a:0c:44"}, IP: "192.168.8.117"},
		{Section: "octopi", Name: "octopi", MACs: []string{"b8:27:eb:5d:12:a0"}, IP: "192.168.8.101", DNS: true},
		{Section: "@host[4]", Name: "drucker", MACs: []string{"30:05:5c:8a:19:b2"}, IP: "192.168.8.40"},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("static hosts:\n got %+v\nwant %+v", hosts, want)
	}
}

func TestUCIValues(t *testing.T) {
	tests := map[string][]string{
		`'1'`:                   {"1"},
		`host`:                  {"host"},
		`'A8:A1' 'A8:A2'`:       {"A8:A1", "A8:A2"},
		`'Bob'\''s iPad'`:       {"Bob's iPad"},
		`'/lan/'`:               {"/lan/"},
		`''`:                    {""},
		`'aa bb' 'cc'`:          {"aa bb", "cc"},
		`'x'\''y' 'z'\'''`:      {"x'y", "z'"},
		`'with=equals,comma'`:   {"with=equals,comma"},
		`'tab	inside' 'second'`: {"tab\tinside", "second"},
	}
	for in, want := range tests {
		if got := uciValues(in); !reflect.DeepEqual(got, want) {
			t.Errorf("uciValues(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeEntries(t *testing.T) {
	leases := parseLeases(plugintest.Fixture(t, "dhcp.leases"))
	hosts := staticHosts(parseUCIShow(plugintest.Fixture(t, "uci-show-dhcp.txt")))

	entries := mergeEntries(leases, hosts, true)
	var got []string
	for _, e := range entries {
		got = append(got, e.ip()+" "+e.hostname()+" "+strconv.FormatBool(e.Static != nil)+" "+strconv.FormatBool(e.Lease != nil))
	}
	want := []string{
		"192.168.8.5 nas true true",
		"192.168.8.20 pve1 true false",
		"192.168.8.40 drucker true false",
		"192.168.8.101 octopi true true",
		"192.168.8.117 laptop-max true true",
		"192.168.8.123 netscope false true",
		"192.168.8.143 iPhone-von-Anna false true",
		"192.168.8.176 ESP-8C021E false true",
		"192.168.8.188  false true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries:\n got %q\nwant %q", got, want)
	}

	// without static import only clients with an active lease are reported
	entries = mergeEntries(leases, hosts, false)
	if len(entries) != 7 {
		t.Fatalf("got %d entries without static hosts, want 7", len(entries))
	}
	for _, e := range entries {
		if e.Lease == nil {
			t.Errorf("entry without lease: %+v", e)
		}
	}
}

// ---------------------------------------------------------------- SSH

type sshReply struct {
	stdout string
	stderr string
	code   uint32
}

// sshServer is a minimal in-process SSH server answering exec requests with canned output.
type sshServer struct {
	port     int
	mu       sync.Mutex
	commands []string
}

func startSSH(t *testing.T, password string, replies map[string]sshReply) *sshServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "root" && string(pw) == password {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &sshServer{port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, cfg, replies)
		}
	}()
	return s
}

func (s *sshServer) serve(conn net.Conn, cfg *ssh.ServerConfig, replies map[string]sshReply) {
	defer conn.Close()
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for req := range creqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &p)
				_ = req.Reply(true, nil)
				s.mu.Lock()
				s.commands = append(s.commands, p.Command)
				s.mu.Unlock()
				r, ok := replies[p.Command]
				if !ok {
					r = sshReply{stderr: "sh: command not found\n", code: 127}
				}
				_, _ = io.WriteString(ch, r.stdout)
				_, _ = io.WriteString(ch.Stderr(), r.stderr)
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{r.code}))
				return
			}
		}()
	}
}

func (s *sshServer) executed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func routerReplies(t *testing.T) map[string]sshReply {
	return map[string]sshReply{
		"uci show dhcp":          {stdout: string(plugintest.Fixture(t, "uci-show-dhcp.txt"))},
		"cat '/tmp/dhcp.leases'": {stdout: string(plugintest.Fixture(t, "dhcp.leases"))},
	}
}

func byIP(obs []plugin.Observation) map[string]plugin.Observation {
	out := map[string]plugin.Observation{}
	for _, o := range obs {
		out[o.IP] = o
	}
	return out
}

func runWith(t *testing.T, settings map[string]any, cred *plugin.Credential) ([]plugin.Observation, *plugin.RunContext, error) {
	t.Helper()
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	rc.Creds = plugintest.Creds{1: cred}
	err := p.Run(context.Background(), rc)
	return sink.All(), rc, err
}

var rootPassword = &plugin.Credential{ID: 1, Name: "router", Type: plugin.CredPassword,
	Public: map[string]string{"username": "root"}, Secret: map[string]string{"password": "goodpass"}}

func checkRouterObservations(t *testing.T, obs []plugin.Observation, method string) {
	t.Helper()
	if len(obs) != 9 {
		t.Fatalf("got %d observations, want 9", len(obs))
	}
	m := byIP(obs)
	for _, o := range obs {
		if o.Present || o.Create {
			t.Errorf("%s: DHCP data must neither mark presence nor create by default", o.IP)
		}
	}
	nas := m["192.168.8.5"]
	inv := nas.Inventory.(dhcpInventory)
	if !reflect.DeepEqual(nas.MACs, []string{"00:11:32:ab:cd:ef"}) || nas.Hostname != "nas" || nas.Attrs["dhcp.static"] != "true" ||
		inv.Lease == nil || !inv.Lease.Infinite || inv.Lease.Expires != nil || !inv.Static || inv.Method != method || !inv.DNS {
		t.Errorf("nas: %+v %+v", nas, inv)
	}
	pve := m["192.168.8.20"]
	if !reflect.DeepEqual(pve.MACs, []string{"a8:a1:59:3c:7e:10", "a8:a1:59:3c:7e:11"}) || pve.Hostname != "pve1" ||
		pve.Inventory.(dhcpInventory).Lease != nil {
		t.Errorf("pve1 (static without lease): %+v", pve)
	}
	phone := m["192.168.8.143"]
	pinv := phone.Inventory.(dhcpInventory)
	if phone.Hostname != "iPhone-von-Anna" || phone.Attrs["dhcp.static"] != "false" || pinv.Static || pinv.Lease == nil ||
		pinv.Lease.Expires == nil || pinv.Lease.Infinite {
		t.Errorf("iPhone: %+v %+v", phone, pinv)
	}
	anon := m["192.168.8.188"]
	if anon.Hostname != "" || !reflect.DeepEqual(anon.MACs, []string{"da:a1:19:6e:33:07"}) {
		t.Errorf("client without hostname: %+v", anon)
	}
	laptop := m["192.168.8.117"]
	if !reflect.DeepEqual(laptop.MACs, []string{"f4:5c:89:b1:22:0d", "d4:81:d7:6a:0c:44"}) || laptop.Hostname != "laptop-max" {
		t.Errorf("laptop: %+v", laptop)
	}
}

func TestRunSSH(t *testing.T) {
	srv := startSSH(t, "goodpass", routerReplies(t))
	obs, rc, err := runWith(t, map[string]any{"host": "127.0.0.1", "port": srv.port, "credential": 1}, rootPassword)
	if err != nil {
		t.Fatal(err)
	}
	checkRouterObservations(t, obs, "ssh")
	if got := srv.executed(); !reflect.DeepEqual(got, []string{"uci show dhcp", "cat '/tmp/dhcp.leases'"}) {
		t.Errorf("commands: %q", got)
	}
	if lease := byIP(obs)["192.168.8.123"].Inventory.(dhcpInventory).Lease; lease.ClientID != "ff:0e:c5:7a:1b:00:02:00:00:ab:11:6d:9b:5c:0e:3f:a2:41:77" ||
		!lease.Expires.Equal(time.Unix(1790149530, 0)) {
		t.Errorf("netscope lease: %+v", lease)
	}
	kh, err := os.ReadFile(filepath.Join(rc.DataDir, "known_hosts"))
	if err != nil || !strings.Contains(string(kh), "ssh-ed25519") {
		t.Errorf("host key not pinned: %v %q", err, kh)
	}
	if st := rc.Stats(); st["leases"] != 7 || st["static"] != 5 || st["observed"] != 9 {
		t.Errorf("stats: %v", st)
	}
}

func TestRunSSHOptions(t *testing.T) {
	srv := startSSH(t, "goodpass", routerReplies(t))
	obs, rc, err := runWith(t, map[string]any{"host": "127.0.0.1", "port": srv.port, "credential": 1,
		"import_static": false, "create_missing": true, "host_key_policy": "insecure"}, rootPassword)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 7 {
		t.Fatalf("got %d observations without static hosts, want 7", len(obs))
	}
	for _, o := range obs {
		if !o.Create {
			t.Errorf("%s: create_missing must set Create", o.IP)
		}
	}
	if _, err := os.Stat(filepath.Join(rc.DataDir, "known_hosts")); !os.IsNotExist(err) {
		t.Errorf("insecure policy must not write known_hosts (err=%v)", err)
	}
}

func TestRunSSHErrors(t *testing.T) {
	srv := startSSH(t, "goodpass", routerReplies(t))
	bad := *rootPassword
	bad.Secret = map[string]string{"password": "wrong"}
	if _, _, err := runWith(t, map[string]any{"host": "127.0.0.1", "port": srv.port, "credential": 1}, &bad); err == nil {
		t.Error("wrong password must fail")
	}

	// a host without uci and lease file is not an OpenWrt router
	other := startSSH(t, "goodpass", map[string]sshReply{})
	if _, _, err := runWith(t, map[string]any{"host": "127.0.0.1", "port": other.port, "credential": 1}, rootPassword); err == nil ||
		!strings.Contains(err.Error(), "OpenWrt") {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------- LuCI

type ubusServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string
}

func startUbus(t *testing.T, loginFixture string) *ubusServer {
	t.Helper()
	login := plugintest.Fixture(t, loginFixture)
	leases := plugintest.Fixture(t, "ubus-dhcp-leases.json")
	uci := plugintest.Fixture(t, "ubus-uci-get-dhcp.json")
	s := &ubusServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ubus" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Method string `json:"method"`
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Method != "call" || len(req.Params) != 4 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var sid, obj, method string
		_ = json.Unmarshal(req.Params[0], &sid)
		_ = json.Unmarshal(req.Params[1], &obj)
		_ = json.Unmarshal(req.Params[2], &method)
		s.mu.Lock()
		s.calls = append(s.calls, obj+"."+method)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case obj == "session" && method == "login":
			var args map[string]string
			_ = json.Unmarshal(req.Params[3], &args)
			if args["username"] != "root" || args["password"] != "goodpass" {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[6]}`))
				return
			}
			_, _ = w.Write(login)
		case sid != "8b4f3a1d9c2e6f7a0b5d4c3e2f1a9b8c":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"Access denied"}}`))
		case obj == "luci-rpc" && method == "getDHCPLeases":
			_, _ = w.Write(leases)
		case obj == "uci" && method == "get":
			_, _ = w.Write(uci)
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-32000,"message":"Object not found"}}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *ubusServer) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestRunLuCI(t *testing.T) {
	srv := startUbus(t, "ubus-login.json")
	before := time.Now()
	obs, _, err := runWith(t, map[string]any{"method": "luci", "luci_url": srv.URL + "/cgi-bin/luci/", "credential": 1}, rootPassword)
	if err != nil {
		t.Fatal(err)
	}
	checkRouterObservations(t, obs, "luci")
	if calls := srv.called(); !reflect.DeepEqual(calls, []string{"session.login", "luci-rpc.getDHCPLeases", "uci.get"}) {
		t.Errorf("calls: %q", calls)
	}
	lease := byIP(obs)["192.168.8.143"].Inventory.(dhcpInventory).Lease
	want := before.Add(42876 * time.Second)
	if lease.Expires.Before(want.Add(-2*time.Second)) || lease.Expires.After(want.Add(5*time.Second)) {
		t.Errorf("expiry %v, want about %v", lease.Expires, want)
	}
	if l := byIP(obs)["192.168.8.123"].Inventory.(dhcpInventory).Lease; l.DUID != "00020000ab116d9b5c0e3fa24177" {
		t.Errorf("duid: %+v", l)
	}
}

func TestRunLuCIErrors(t *testing.T) {
	srv := startUbus(t, "ubus-login.json")
	bad := *rootPassword
	bad.Secret = map[string]string{"password": "wrong"}
	_, _, err := runWith(t, map[string]any{"method": "luci", "luci_url": srv.URL, "credential": 1}, &bad)
	if err == nil || !strings.Contains(err.Error(), "Anmeldung") {
		t.Errorf("wrong password: err = %v", err)
	}

	denied := startUbus(t, "ubus-login-denied.json")
	_, _, err = runWith(t, map[string]any{"method": "luci", "luci_url": denied.URL, "credential": 1}, rootPassword)
	if err == nil || !strings.Contains(err.Error(), "Anmeldung") {
		t.Errorf("denied login: err = %v", err)
	}

	sshCred := &plugin.Credential{ID: 1, Name: "key", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}}
	if _, _, err := runWith(t, map[string]any{"method": "luci", "luci_url": srv.URL, "credential": 1}, sshCred); err == nil {
		t.Error("LuCI with an SSH credential must fail")
	}
}

func TestUbusEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"http://192.168.8.1":                    "http://192.168.8.1/ubus",
		"http://192.168.8.1:8080/":              "http://192.168.8.1:8080/ubus",
		"https://router.lan/cgi-bin/luci/":      "https://router.lan/ubus",
		"http://192.168.8.1/cgi-bin/luci/admin": "http://192.168.8.1/ubus",
		"http://192.168.8.1/ubus":               "http://192.168.8.1/ubus",
	} {
		got, err := ubusEndpoint(in)
		if err != nil || got != want {
			t.Errorf("ubusEndpoint(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ubusEndpoint("192.168.8.1"); err == nil {
		t.Error("URL without scheme must be rejected")
	}
}

func TestUCIGetSections(t *testing.T) {
	var res struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(plugintest.Fixture(t, "ubus-uci-get-dhcp.json"), &res); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Values map[string]map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(res.Result[1], &cfg); err != nil {
		t.Fatal(err)
	}
	sections := uciGetSections(cfg.Values)
	var names []string
	for _, h := range staticHosts(sections) {
		names = append(names, h.Name)
	}
	if !reflect.DeepEqual(names, []string{"nas", "pve1", "laptop-max", "octopi", "drucker"}) {
		t.Errorf("hosts: %v", names)
	}
	if got := leaseFiles(sections); !reflect.DeepEqual(got, []string{"/tmp/dhcp.leases"}) {
		t.Errorf("lease files: %v", got)
	}
}
