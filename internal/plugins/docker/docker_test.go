package docker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

// engineFixtures maps request URIs to real responses captured from Docker 29.8.1
// (API 1.56, requested as v1.51) on the NetScope LXC.
var engineFixtures = map[string]string{
	"/version":                     "docker-version.json",
	"/v1.51/info":                  "docker-info.json",
	"/v1.51/containers/json?all=1": "docker-containers.json",
	"/v1.51/images/json":           "docker-images.json",
}

type fakeEngine struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string][]byte
}

func newFakeEngine(t *testing.T) *fakeEngine {
	f := &fakeEngine{bodies: map[string][]byte{}}
	for uri, file := range engineFixtures {
		f.bodies[uri] = plugintest.Fixture(t, file)
	}
	return f
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.RequestURI())
	f.mu.Unlock()
	body, ok := f.bodies[r.URL.RequestURI()]
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", "1.56")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"page not found"}` + "\n"))
		return
	}
	_, _ = w.Write(body)
}

func (f *fakeEngine) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want endpoint
	}{
		{"unix:///var/run/docker.sock", endpoint{Scheme: "unix", Path: "/var/run/docker.sock"}},
		{"tcp://docker.lan:2375", endpoint{Scheme: "tcp", Host: "docker.lan", Port: 2375}},
		{"tcp://192.168.8.30", endpoint{Scheme: "tcp", Host: "192.168.8.30", Port: 2375}},
		{"ssh://root@192.168.8.123", endpoint{Scheme: "ssh", Host: "192.168.8.123", Port: 22, User: "root", Path: defaultSocket}},
		{"ssh://nas.lan:2222", endpoint{Scheme: "ssh", Host: "nas.lan", Port: 2222, Path: defaultSocket}},
		{"ssh://admin@[fd00::5]:22/run/user/1000/docker.sock", endpoint{Scheme: "ssh", Host: "fd00::5", Port: 22, User: "admin", Path: "/run/user/1000/docker.sock"}},
	}
	for _, tt := range tests {
		got, err := parseEndpoint(tt.in)
		tt.want.Raw = tt.in
		if err != nil || got != tt.want {
			t.Errorf("parseEndpoint(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"docker.lan:2375", "http://docker.lan", "tcp://", "ssh://root:secret@host", "tcp://host:99999", "unix://", "tcp://host/path"} {
		if _, err := parseEndpoint(bad); err == nil {
			t.Errorf("parseEndpoint(%q): expected error", bad)
		}
	}
}

func TestNegotiateVersion(t *testing.T) {
	tests := []struct{ server, min, want string }{
		{"1.56", "1.40", "1.51"},
		{"1.43", "1.24", "1.43"},
		{"1.41", "", "1.41"},
		{"1.60", "1.55", "1.55"},
		{"", "", fallbackAPIVersion},
		{"garbage", "1.12", fallbackAPIVersion},
	}
	for _, tt := range tests {
		if got := negotiateVersion(tt.server, tt.min); got != tt.want {
			t.Errorf("negotiateVersion(%q, %q) = %q, want %q", tt.server, tt.min, got, tt.want)
		}
	}
}

func loadContainers(t *testing.T) []plugin.Container {
	var list []containerResp
	if err := json.Unmarshal(plugintest.Fixture(t, "docker-containers.json"), &list); err != nil {
		t.Fatal(err)
	}
	return convertContainers(list)
}

func TestConvertContainers(t *testing.T) {
	cs := loadContainers(t)
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"nsfixture-standalone", "nsfixture-web-1", "nsfixture-worker-1"}) {
		t.Fatalf("names: %v", names)
	}
	web := cs[1]
	wantPorts := []plugin.ContainerPort{
		{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 18080, Type: "tcp"},
		{IP: "::", PrivatePort: 80, PublicPort: 18080, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 443, PublicPort: 18443, Type: "tcp"},
	}
	if web.Image != "alpine:3.22" || web.State != "running" || !strings.HasPrefix(web.Status, "Up ") ||
		web.ComposeProject != "nsfixture" || web.ComposeService != "web" || web.Labels["org.netscope.fixture"] != "web" ||
		!reflect.DeepEqual(web.Networks, []string{"nsfixture_backend", "nsfixture_default"}) ||
		!reflect.DeepEqual(web.Ports, wantPorts) || len(web.ID) != 64 ||
		web.ImageID != "sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8" || web.Created.IsZero() {
		t.Errorf("web: %+v", web)
	}
	worker := cs[2]
	if worker.State != "exited" || !strings.HasPrefix(worker.Status, "Exited (0)") || worker.ComposeService != "worker" ||
		len(worker.Ports) != 0 || !reflect.DeepEqual(worker.Networks, []string{"nsfixture_backend"}) {
		t.Errorf("worker: %+v", worker)
	}
	standalone := cs[0]
	if standalone.ComposeProject != "" || len(standalone.Ports) != 2 || standalone.Ports[0].Type != "udp" ||
		standalone.Ports[0].PublicPort != 15353 || !reflect.DeepEqual(standalone.Networks, []string{"bridge"}) {
		t.Errorf("standalone: %+v", standalone)
	}
}

func TestContainerName(t *testing.T) {
	if got := containerName([]string{"/web/db", "/db"}); got != "db" {
		t.Errorf("containerName = %q", got)
	}
	if got := containerName([]string{"/solo"}); got != "solo" {
		t.Errorf("containerName = %q", got)
	}
}

func TestConvertImages(t *testing.T) {
	var list []imageResp
	if err := json.Unmarshal(plugintest.Fixture(t, "docker-images.json"), &list); err != nil {
		t.Fatal(err)
	}
	list = append(list, imageResp{ID: "sha256:0000000000000000000000000000000000000000000000000000000000000001", RepoTags: []string{"<none>:<none>"}, Size: 1})
	imgs := convertImages(list)
	if len(imgs) != 3 {
		t.Fatalf("got %d images", len(imgs))
	}
	var alpine *plugin.ContainerImage
	for i := range imgs {
		if len(imgs[i].Tags) > 0 && imgs[i].Tags[0] == "alpine:3.22" {
			alpine = &imgs[i]
		}
	}
	if alpine == nil || alpine.Size != 12846359 || alpine.Created.IsZero() {
		t.Errorf("alpine: %+v", alpine)
	}
	if imgs[0].Tags != nil {
		t.Errorf("untagged image must have no tags: %+v", imgs[0])
	}
}

func TestDefaultRouteIface(t *testing.T) {
	if got := defaultRouteIface(plugintest.Fixture(t, "proc-net-route.txt")); got != "eth0" {
		t.Errorf("defaultRouteIface = %q", got)
	}
	two := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"wlan0\t00000000\t0100A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
		"eth1\t00000000\t0108A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"eth2\t00000000\t0108A8C0\t0002\t0\t0\t1\t00000000\t0\t0\t0\n"
	if got := defaultRouteIface([]byte(two)); got != "eth1" {
		t.Errorf("lowest metric: %q", got)
	}
	if got := defaultRouteIface([]byte("Iface\tDestination\n")); got != "" {
		t.Errorf("no default route: %q", got)
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	vals, err := p.Schema().Validate(map[string]any{"endpoints": []any{"ssh://root@192.168.8.123"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// no credential selected: picked automatically by scope
	if err := p.ValidateSettings(plugin.NewSettings(vals)); err != nil {
		t.Errorf("valid settings: %v", err)
	}
	var ve *plugin.ValidationError
	if err := p.ValidateSettings(plugin.NewSettings(map[string]any{"endpoints": []any{"ssh://"}})); !errors.As(err, &ve) || ve.Errors[0].Field != "endpoints" {
		t.Errorf("invalid endpoint: %v", err)
	}
	got := p.MigrateSettings(map[string]any{"endpoints": []any{"ssh://a"}, "ssh_credential": float64(3)})
	if !reflect.DeepEqual(got, map[string]any{"endpoints": []any{"ssh://a"}, "ssh_credentials": []any{int64(3)}}) {
		t.Errorf("migrated: %#v", got)
	}
	if _, err := p.Schema().Validate(map[string]any{"endpoints": []any{"docker.lan:2375"}}, nil, nil); err == nil {
		t.Error("endpoint without scheme must fail schema validation")
	}
}

func checkHost(t *testing.T, obs []plugin.Observation, ip string) {
	t.Helper()
	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1", len(obs))
	}
	o := obs[0]
	if o.IP != ip || o.Hostname != "netscope" || o.Create || o.Present {
		t.Errorf("host observation: ip=%q hostname=%q create=%v present=%v", o.IP, o.Hostname, o.Create, o.Present)
	}
	if o.Containers == nil || o.Containers.Engine != "docker" || len(o.Containers.Containers) != 3 || len(o.Containers.Images) != 2 {
		t.Fatalf("containers: %+v", o.Containers)
	}
	inv := o.Inventory.(engineInventory)
	want := engineInventory{Endpoint: inv.Endpoint, EngineVersion: "29.8.1", APIVersion: "1.51", Platform: "Docker Engine - Community",
		OS: "Ubuntu 24.04 LTS", OSType: "linux", Kernel: "7.0.14-8-pve", Architecture: "x86_64", CPUs: 4, Memory: 2147483648,
		StorageDriver: "overlayfs", RootDir: "/var/lib/docker", Swarm: "inactive", Containers: 3, Running: 2, Stopped: 1, Images: 2,
		ComposeProjects: []string{"nsfixture"}}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("inventory:\n got %+v\nwant %+v", inv, want)
	}
	if !strings.Contains(o.Raw, "nsfixture-web-1") {
		t.Error("raw container list missing")
	}
}

func run(t *testing.T, settings map[string]any, creds plugintest.Creds) ([]plugin.Observation, *plugin.RunContext, error) {
	t.Helper()
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	if creds != nil {
		rc.Creds = creds
	}
	err := p.Run(context.Background(), rc)
	return sink.All(), rc, err
}

func TestRunTCP(t *testing.T) {
	eng := newFakeEngine(t)
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	ep := "tcp://" + strings.TrimPrefix(srv.URL, "http://")
	obs, rc, err := run(t, map[string]any{"endpoints": []any{ep}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(t, obs, "127.0.0.1")
	want := []string{"/version", "/v1.51/info", "/v1.51/containers/json?all=1", "/v1.51/images/json"}
	if got := eng.seen(); !reflect.DeepEqual(got, want) {
		t.Errorf("requests: %q", got)
	}
	if st := rc.Stats(); st["containers"] != 3 || st["images"] != 2 {
		t.Errorf("stats: %v", st)
	}
}

func TestRunUnix(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets not available: %v", err)
	}
	srv := httptest.NewUnstartedServer(newFakeEngine(t))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	orig := localIPv4
	localIPv4 = func() (string, error) { return "192.168.8.123", nil }
	t.Cleanup(func() { localIPv4 = orig })

	obs, _, err := run(t, map[string]any{"endpoints": []any{"unix://" + filepath.ToSlash(sock)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(t, obs, "192.168.8.123")
}

// ---------------------------------------------------------------- SSH tunnel

type chanConn struct{ ssh.Channel }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "ssh" }
func (pipeAddr) String() string  { return "ssh-channel" }

func (chanConn) LocalAddr() net.Addr                { return pipeAddr{} }
func (chanConn) RemoteAddr() net.Addr               { return pipeAddr{} }
func (chanConn) SetDeadline(t time.Time) error      { return nil }
func (chanConn) SetReadDeadline(t time.Time) error  { return nil }
func (chanConn) SetWriteDeadline(t time.Time) error { return nil }

// chanListener hands forwarded SSH channels to an http.Server.
type chanListener struct {
	conns chan net.Conn
	once  sync.Once
	done  chan struct{}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *chanListener) Addr() net.Addr { return pipeAddr{} }

// sshDocker is an SSH server that forwards direct-streamlocal channels for
// /var/run/docker.sock to the fake engine.
type sshDocker struct {
	port    int
	mu      sync.Mutex
	sockets []string
}

func startSSHDocker(t *testing.T, handler http.Handler) *sshDocker {
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
		if c.User() == "root" && string(pw) == "goodpass" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lis := &chanListener{conns: make(chan net.Conn), done: make(chan struct{})}
	hs := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hs.Serve(lis) }()
	t.Cleanup(func() {
		ln.Close()
		lis.Close()
		hs.Close()
	})
	s := &sshDocker{port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					if nc.ChannelType() != "direct-streamlocal@openssh.com" {
						_ = nc.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					var p struct {
						SocketPath string
						Reserved0  string
						Reserved1  uint32
					}
					_ = ssh.Unmarshal(nc.ExtraData(), &p)
					s.mu.Lock()
					s.sockets = append(s.sockets, p.SocketPath)
					s.mu.Unlock()
					if p.SocketPath != defaultSocket {
						_ = nc.Reject(ssh.ConnectionFailed, "no such socket")
						continue
					}
					ch, creqs, err := nc.Accept()
					if err != nil {
						return
					}
					go ssh.DiscardRequests(creqs)
					select {
					case lis.conns <- chanConn{ch}:
					case <-lis.done:
						ch.Close()
					}
				}
			}()
		}
	}()
	return s
}

func TestRunSSH(t *testing.T) {
	eng := newFakeEngine(t)
	srv := startSSHDocker(t, eng)
	// the user of the URL overrides the credential's user name
	creds := plugintest.Creds{7: {ID: 7, Name: "docker", Type: plugin.CredPassword,
		Public: map[string]string{"username": "nobody"}, Secret: map[string]string{"password": "goodpass"}}}
	ep := "ssh://root@127.0.0.1:" + strconv.Itoa(srv.port)
	obs, _, err := run(t, map[string]any{"endpoints": []any{ep}, "ssh_credentials": []any{7}}, creds)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(t, obs, "127.0.0.1")
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.sockets) == 0 || srv.sockets[0] != defaultSocket {
		t.Errorf("forwarded sockets: %v", srv.sockets)
	}
}

func TestRunErrors(t *testing.T) {
	// ssh endpoint without credential
	if _, _, err := run(t, map[string]any{"endpoints": []any{"ssh://root@127.0.0.1:1"}}, nil); err == nil ||
		!errors.Is(err, plugin.ErrNoCredential) {
		t.Errorf("missing credential: %v", err)
	}
	// unreachable engine: the whole run fails
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if _, _, err := run(t, map[string]any{"endpoints": []any{"tcp://" + addr}, "timeout": "3s"}, nil); err == nil ||
		!strings.Contains(err.Error(), "kein Docker-Endpunkt erreichbar") {
		t.Errorf("unreachable: %v", err)
	}
	// one of two endpoints fails: the run succeeds with the reachable one
	srv := httptest.NewServer(newFakeEngine(t))
	t.Cleanup(srv.Close)
	obs, rc, err := run(t, map[string]any{"endpoints": []any{"tcp://" + addr, "tcp://" + strings.TrimPrefix(srv.URL, "http://")}, "timeout": "3s"}, nil)
	if err != nil || len(obs) != 1 || rc.Stats()["failed"] != 1 {
		t.Errorf("partial failure: err=%v obs=%d stats=%v", err, len(obs), rc.Stats())
	}
}

// zeroSink simulates a host that is not in the inventory.
type zeroSink struct{ n int }

func (z *zeroSink) Observe(ctx context.Context, o *plugin.Observation) (int64, error) {
	z.n++
	return 0, nil
}

func TestRunUnknownHost(t *testing.T) {
	srv := httptest.NewServer(newFakeEngine(t))
	t.Cleanup(srv.Close)
	p := &Plugin{}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"endpoints": []any{"tcp://" + strings.TrimPrefix(srv.URL, "http://")}})
	sink := &zeroSink{}
	rc.Sink = sink
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if sink.n != 1 || rc.Stats()["unknown_hosts"] != 1 {
		t.Errorf("observations=%d stats=%v", sink.n, rc.Stats())
	}
}
