package proxmox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/sshx/sshtest"
)

func TestParseLXCOutput(t *testing.T) {
	out, err := parseLXCOutput(string(plugintest.Fixture(t, "lxc-docker.out")))
	if err != nil {
		t.Fatal(err)
	}
	if out.Node != "pve1" || !out.Complete || out.Error != "" || len(out.LXC) != 3 {
		t.Fatalf("output: %+v", out)
	}
	d, ok, why := out.LXC[0].docker()
	if !ok {
		t.Fatalf("lxc 105: %s", why)
	}
	if d.info != (lxcDockerInfo{Version: "27.3.1", Containers: 2, Running: 1, Images: 2}) {
		t.Errorf("info = %+v", d.info)
	}
	c := d.inv.Containers
	if d.inv.Engine != "docker" || len(c) != 2 || c[0].Name != "db-postgres-1" || c[1].Name != "web-nginx-1" {
		t.Fatalf("containers = %+v", c)
	}
	if c[0].State != "exited" || c[0].ComposeProject != "db" ||
		c[0].ImageID != "sha256:5d0da3dc976460b72c77d94c8a1ad043720b0416bfc16c52c45d4847e53fadb6" {
		t.Errorf("postgres = %+v", c[0])
	}
	want := []plugin.ContainerPort{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}}
	if c[1].ImageID != "sha256:39286ab8a5e14aeaf5fdd6e2fac76e0c8d31a0c07224f0ee5e6be502f12e93f3" || !reflect.DeepEqual(c[1].Ports, want) {
		t.Errorf("nginx = %+v", c[1])
	}
	if _, ok, _ := out.LXC[1].docker(); ok || !out.LXC[1].NoDocker || out.LXC[1].VMID != 107 {
		t.Errorf("lxc 107 has no docker: %+v", out.LXC[1])
	}
	if _, ok, why := out.LXC[2].docker(); ok || !strings.Contains(why, "Exit-Code 1") {
		t.Errorf("lxc 108 with failing docker must be skipped, got ok=%v why=%q", ok, why)
	}
}

func TestParseLXCOutputErrors(t *testing.T) {
	if _, err := parseLXCOutput("This account is restricted\n"); err == nil {
		t.Error("output without header must fail")
	}
	out, err := parseLXCOutput(lxcHeader + "\n#node x\n#error pct nicht gefunden – kein Proxmox-VE-Host\n")
	if err != nil || out.Error == "" || out.Complete {
		t.Errorf("out = %+v, err = %v", out, err)
	}
	// docker without containers is a valid, empty list (all stored containers are gone)
	out, _ = parseLXCOutput(lxcHeader + "\n#lxc 5\n#version\n27.0.1\n#ps\n#images\n#end\n")
	d, ok, _ := out.LXC[0].docker()
	if !ok || d.inv.Containers == nil || len(d.inv.Containers) != 0 || d.inv.Images == nil {
		t.Errorf("empty docker: ok=%v %+v", ok, d)
	}
	// a broken JSON line makes the list unreliable
	out, _ = parseLXCOutput(lxcHeader + "\n#lxc 5\n#ps\n{broken\n#end\n")
	if _, ok, _ := out.LXC[0].docker(); ok {
		t.Error("broken docker ps output must be skipped")
	}
}

func TestInventoryScriptInSync(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "proxmox", "netscope-docker-inventory"))
	if err != nil {
		t.Fatal(err)
	}
	if string(shipped) != inventoryScript {
		t.Fatal("scripts/proxmox/netscope-docker-inventory differs from the embedded script – copy internal/plugins/proxmox/netscope-docker-inventory.sh")
	}
	for _, want := range []string{lxcHeader, "pct list", "pct exec", "docker ps -a --no-trunc", "#end"} {
		if !strings.Contains(inventoryScript, want) {
			t.Errorf("script lacks %q", want)
		}
	}
}

func TestRunLXCDocker(t *testing.T) {
	srv := newPVE(t)
	// pve1 is reachable on the loopback address of the test SSH server
	srv.set("/api2/json/cluster/status", `{"data":[{"name":"homelab","type":"cluster"},{"type":"node","name":"pve1","online":1,"ip":"127.0.0.1"},{"type":"node","name":"pve2","online":0,"ip":"192.168.8.21"}]}`)
	fixture := string(plugintest.Fixture(t, "lxc-docker.out"))
	node := sshtest.New(t, "root", "geheim", func(cmd string) (string, int) { return fixture, 0 })

	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"urls": []any{srv.URL}, "lxc_docker": true,
		"lxc_docker_port": node.Port(), "lxc_docker_host_key_policy": "insecure"})
	rc.Creds = plugintest.Creds{
		1: {ID: 1, Name: "pve", Type: plugin.CredAPIToken, Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": testSecret}},
		2: {ID: 2, Name: "pve-ssh", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}, Secret: map[string]string{"password": "geheim"}},
	}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	cmds := node.Commands()
	if len(cmds) != 1 || !strings.Contains(cmds[0], "pct exec") {
		t.Fatalf("commands = %q", cmds)
	}
	obs := sink.All()
	lxc := byRef(obs, refGuest, "pve1/lxc/105")
	if lxc == nil || lxc.Containers == nil || len(lxc.Containers.Containers) != 2 {
		t.Fatalf("lxc 105 containers: %+v", lxc)
	}
	if inv := lxc.Inventory.(guestInventory); inv.Docker == nil || inv.Docker.Containers != 2 || inv.Docker.Version != "27.3.1" {
		t.Errorf("lxc 105 docker summary: %+v", inv.Docker)
	}
	for _, o := range obs {
		if o.Containers != nil && o.Ref.ID != "pve1/lxc/105" {
			t.Errorf("unexpected containers on %s", o.Ref.ID)
		}
	}
	if rc.Stats()["lxc_docker_containers"] != 2 {
		t.Errorf("stats = %v", rc.Stats())
	}
}

func TestRunLXCDockerFailureKeepsImport(t *testing.T) {
	srv := newPVE(t)
	srv.set("/api2/json/cluster/status", `{"data":[{"name":"homelab","type":"cluster"},{"type":"node","name":"pve1","online":1,"ip":"127.0.0.1"}]}`)
	node := sshtest.New(t, "root", "geheim", func(string) (string, int) { return "", 0 })
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"urls": []any{srv.URL}, "lxc_docker": true,
		"lxc_docker_port": node.Port(), "lxc_docker_host_key_policy": "insecure"})
	rc.Creds = plugintest.Creds{
		1: {ID: 1, Name: "pve", Type: plugin.CredAPIToken, Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": testSecret}},
		2: {ID: 2, Name: "falsch", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}, Secret: map[string]string{"password": "nope"}},
	}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if rc.Stats()["lxc_docker_failed"] != 1 || len(sink.All()) != 6 {
		t.Errorf("stats = %v, observations = %d", rc.Stats(), len(sink.All()))
	}
	for _, o := range sink.All() {
		if o.Containers != nil {
			t.Errorf("no containers expected when the node is not readable: %s", o.Ref.ID)
		}
	}
}

func TestMigrateSettings(t *testing.T) {
	p := &Plugin{}
	got := p.MigrateSettings(map[string]any{"url": "https://pve.lan:8006", "credential": float64(3), "verify_tls": true})
	want := map[string]any{"urls": []any{"https://pve.lan:8006"}, "credentials": []any{int64(3)}, "verify_tls": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("migrated = %#v", got)
	}
	// idempotent, new keys win
	again := p.MigrateSettings(map[string]any{"urls": []any{"https://a:8006"}, "url": "https://old:8006"})
	if !reflect.DeepEqual(again, map[string]any{"urls": []any{"https://a:8006"}}) {
		t.Fatalf("second migration = %#v", again)
	}
	s := p.Schema().Normalize(p.MigrateSettings(map[string]any{"url": "https://pve.lan:8006", "credential": float64(3)}))
	st := plugin.NewSettings(s)
	if !reflect.DeepEqual(st.StringList("urls"), []string{"https://pve.lan:8006"}) || !reflect.DeepEqual(st.CredentialIDs("credentials"), []int64{3}) {
		t.Errorf("normalized = %#v", s)
	}
}

func TestRunTokenFallback(t *testing.T) {
	srv := newPVE(t)
	p := &Plugin{}
	// no explicit selection: all applicable tokens are tried, the rejected first one is skipped
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"urls": []any{srv.URL}})
	rc.Creds = plugintest.Creds{
		1: {ID: 1, Name: "alt", Type: plugin.CredAPIToken, Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": "alt"}},
		2: {ID: 2, Name: "neu", Type: plugin.CredAPIToken, Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": testSecret}},
		3: {ID: 3, Name: "ssh", Type: plugin.CredSSH},
	}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if len(sink.All()) != 6 {
		t.Errorf("observations = %d", len(sink.All()))
	}
	// without any applicable token the run fails clearly
	rc.Creds = plugintest.Creds{3: {ID: 3, Name: "ssh", Type: plugin.CredSSH}}
	if err := p.Run(context.Background(), rc); !errors.Is(err, plugin.ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}

func TestRunSeveralURLs(t *testing.T) {
	srv := newPVE(t)
	down := newPVE(t)
	downURL := down.URL
	down.Close()
	// the same cluster twice is imported once; an unreachable endpoint does not fail the run
	obs, rc, err := runPVE(t, srv, map[string]any{"urls": []any{srv.URL, srv.URL + "/", downURL}}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 6 || srv.count("/api2/json/cluster/resources?type=vm") != 1 {
		t.Errorf("observations = %d, guest list requests = %d", len(obs), srv.count("/api2/json/cluster/resources?type=vm"))
	}
	if rc.Stats()["failed_urls"] != 1 {
		t.Errorf("stats = %v", rc.Stats())
	}
	// all endpoints failing fails the run
	if _, _, err := runPVE(t, srv, map[string]any{"urls": []any{downURL, downURL + "/"}}, testSecret); err == nil {
		t.Error("expected an error when no endpoint works")
	}
}
