package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

const (
	testTokenID = "netscope@pve!inventory"
	testSecret  = "5b2e8f7a-1c3d-4e9f-a0b1-c2d3e4f5a6b7"
)

// pveServer serves the fixtures like a two-node PVE 8 cluster whose second node is offline.
type pveServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newPVE(t *testing.T) *pveServer {
	t.Helper()
	routes := map[string]string{
		"/api2/json/cluster/status":                                   "cluster-status.json",
		"/api2/json/nodes":                                            "nodes.json",
		"/api2/json/nodes/pve1/status":                                "node-pve1-status.json",
		"/api2/json/cluster/resources?type=vm":                        "cluster-resources-vm.json",
		"/api2/json/nodes/pve1/qemu/100/config":                       "qemu-100-config.json",
		"/api2/json/nodes/pve1/qemu/100/agent/network-get-interfaces": "qemu-100-agent-network.json",
		"/api2/json/nodes/pve1/qemu/101/config":                       "qemu-101-config.json",
		"/api2/json/nodes/pve1/qemu/9000/config":                      "qemu-9000-config.json",
		"/api2/json/nodes/pve1/lxc/105/config":                        "lxc-105-config.json",
		"/api2/json/nodes/pve1/lxc/105/interfaces":                    "lxc-105-interfaces.json",
	}
	bodies := map[string][]byte{}
	for path, file := range routes {
		bodies[path] = plugintest.Fixture(t, file)
	}
	s := &pveServer{}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		s.mu.Lock()
		s.requests = append(s.requests, key)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		if r.Header.Get("Authorization") != "PVEAPIToken="+testTokenID+"="+testSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"data":null}`))
			return
		}
		if strings.HasPrefix(key, "/api2/json/nodes/pve2/") {
			// the API proxies requests to the offline node and fails
			w.WriteHeader(595)
			_, _ = w.Write([]byte(`{"data":null}`))
			return
		}
		body, ok := bodies[key]
		if !ok {
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"data":null}`))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *pveServer) requested(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.requests {
		if r == key {
			return true
		}
	}
	return false
}

func runPVE(t *testing.T, srv *pveServer, settings map[string]any, secret string) ([]plugin.Observation, *plugin.RunContext, error) {
	t.Helper()
	p := &Plugin{}
	vals := map[string]any{"url": srv.URL, "credential": 1}
	for k, v := range settings {
		vals[k] = v
	}
	rc, sink, _ := plugintest.RunContext(t, p, vals)
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "pve", Type: plugin.CredAPIToken,
		Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": secret}}}
	err := p.Run(context.Background(), rc)
	return sink.All(), rc, err
}

func byRef(obs []plugin.Observation, source, id string) *plugin.Observation {
	for i := range obs {
		if obs[i].Ref != nil && obs[i].Ref.Source == source && obs[i].Ref.ID == id {
			return &obs[i]
		}
	}
	return nil
}

func TestRun(t *testing.T) {
	srv := newPVE(t)
	obs, rc, err := runPVE(t, srv, nil, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 6 {
		t.Fatalf("got %d observations, want 6 (2 nodes, 4 guests)", len(obs))
	}
	// nodes are observed before the guests so the runs_on relations resolve
	for i, want := range []string{"pve1", "pve2"} {
		if obs[i].Ref == nil || obs[i].Ref.Source != refNode || obs[i].Ref.ID != want {
			t.Fatalf("observation %d: ref %+v, want node %s", i, obs[i].Ref, want)
		}
	}

	pve1 := obs[0]
	if pve1.IP != "192.168.8.20" || pve1.Hostname != "pve1" || pve1.DeviceType != "hypervisor" || !pve1.Create {
		t.Errorf("pve1: %+v", pve1)
	}
	if pve1.OS == nil || pve1.OS.Name != "Proxmox VE 8.2.4" {
		t.Errorf("pve1 os: %+v", pve1.OS)
	}
	inv := pve1.Inventory.(nodeInventory)
	if inv.PVEVersion != "8.2.4" || inv.Cluster != "homelab" || inv.CPUs != 6 || inv.Cores != 6 ||
		inv.MaxMem != 33541771264 || inv.Kernel != "6.8.8-2-pve" || inv.Uptime != 1209838 ||
		!reflect.DeepEqual(inv.LoadAvg, []float64{0.15, 0.19, 0.18}) {
		t.Errorf("pve1 inventory: %+v", inv)
	}
	pve2 := obs[1]
	if pve2.IP != "192.168.8.21" || pve2.OS != nil || pve2.Inventory.(nodeInventory).Status != "offline" {
		t.Errorf("pve2: %+v", pve2)
	}
	if srv.requested("/api2/json/nodes/pve2/status") {
		t.Error("status of the offline node must not be requested")
	}

	vm := byRef(obs, refGuest, "pve1/qemu/100")
	if vm == nil {
		t.Fatal("qemu 100 missing")
	}
	if !reflect.DeepEqual(vm.MACs, []string{"bc:24:11:2e:c5:6a", "bc:24:11:d0:0b:4e"}) {
		t.Errorf("qemu 100 macs: %v", vm.MACs)
	}
	if vm.IP != "192.168.8.30" || !reflect.DeepEqual(vm.IPs, []string{"10.20.0.30", "fd00::be24:11ff:fe2e:c56a"}) {
		t.Errorf("qemu 100 addresses: %s %v", vm.IP, vm.IPs)
	}
	if vm.Hostname != "docker-host" || vm.DeviceType != "vm" || !vm.Create {
		t.Errorf("qemu 100: %+v", vm)
	}
	wantAttrs := map[string]string{"proxmox.vmid": "100", "proxmox.node": "pve1", "proxmox.type": "qemu"}
	if !reflect.DeepEqual(vm.Attrs, wantAttrs) {
		t.Errorf("qemu 100 attrs: %v", vm.Attrs)
	}
	wantRel := []plugin.Relation{{Kind: plugin.RelRunsOn, Other: plugin.DeviceRef{Ref: &plugin.ExternalRef{Source: refNode, ID: "pve1"}}}}
	if !reflect.DeepEqual(vm.Relations, wantRel) {
		t.Errorf("qemu 100 relations: %+v", vm.Relations)
	}
	gi := vm.Inventory.(guestInventory)
	if gi.VMID != 100 || gi.Status != "running" || gi.CPUs != 4 || gi.MaxMem != 8589934592 || gi.MaxDisk != 68719476736 ||
		gi.Uptime != 1208711 || !reflect.DeepEqual(gi.Tags, []string{"docker", "prod"}) || gi.HAState != "started" ||
		!gi.Agent || gi.OSType != "l26" || !gi.OnBoot || gi.Template || len(gi.Networks) != 2 ||
		gi.Networks[1].VLAN != "20" || gi.Networks[1].Bridge != "vmbr1" || gi.Networks[0].Model != "virtio" || !gi.Networks[0].Firewall {
		t.Errorf("qemu 100 inventory: %+v", gi)
	}

	win := byRef(obs, refGuest, "pve1/qemu/101")
	if win == nil || !reflect.DeepEqual(win.MACs, []string{"bc:24:11:8f:01:2d"}) || win.IP != "" || win.Hostname != "win11" {
		t.Fatalf("qemu 101: %+v", win)
	}
	if srv.requested("/api2/json/nodes/pve1/qemu/101/agent/network-get-interfaces") {
		t.Error("guest agent of a stopped VM must not be queried")
	}

	ct := byRef(obs, refGuest, "pve1/lxc/105")
	if ct == nil {
		t.Fatal("lxc 105 missing")
	}
	if !reflect.DeepEqual(ct.MACs, []string{"bc:24:11:27:22:b5"}) || ct.IP != "192.168.8.123" || len(ct.IPs) != 0 ||
		ct.Hostname != "netscope" || ct.DeviceType != "container" {
		t.Errorf("lxc 105: %+v", ct)
	}
	if ci := ct.Inventory.(guestInventory); !ci.Unprivileged || ci.OSType != "ubuntu" || ci.Networks[0].Name != "eth0" || ci.Networks[0].IP != "dhcp" {
		t.Errorf("lxc 105 inventory: %+v", ci)
	}

	// the guest on the offline node keeps its reference and relation although its
	// configuration is unreachable; without MACs it only updates an already linked device
	pihole := byRef(obs, refGuest, "pve2/lxc/106")
	if pihole == nil || len(pihole.MACs) != 0 || pihole.Create || pihole.Hostname != "pihole" || pihole.Relations[0].Other.Ref.ID != "pve2" {
		t.Errorf("lxc 106: %+v", pihole)
	}

	if byRef(obs, refGuest, "pve1/qemu/9000") != nil || srv.requested("/api2/json/nodes/pve1/qemu/9000/config") {
		t.Error("templates must be skipped by default")
	}
	stats := rc.Stats()
	if stats["nodes"] != 2 || stats["vms"] != 2 || stats["containers"] != 2 || stats["skipped_templates"] != 1 {
		t.Errorf("stats: %v", stats)
	}
}

func TestRunFilters(t *testing.T) {
	srv := newPVE(t)
	obs, _, err := runPVE(t, srv, map[string]any{"include_templates": true, "include_stopped": false,
		"guest_agent_ips": false, "create_missing": false}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range obs {
		if o.Create {
			t.Errorf("%s: create_missing=false must not create", o.Target)
		}
		if o.Ref.Source == refGuest {
			ids = append(ids, o.Ref.ID)
		}
	}
	// running guests only; the stopped template is excluded by include_stopped=false
	if len(ids) != 2 || byRef(obs, refGuest, "pve1/qemu/100") == nil || byRef(obs, refGuest, "pve1/lxc/105") == nil {
		t.Fatalf("guests: %v", ids)
	}
	if srv.requested("/api2/json/nodes/pve1/qemu/100/agent/network-get-interfaces") || srv.requested("/api2/json/nodes/pve1/lxc/105/interfaces") {
		t.Error("guest addresses must not be queried with guest_agent_ips=false")
	}
	if vm := byRef(obs, refGuest, "pve1/qemu/100"); vm.IP != "" {
		t.Errorf("qemu 100 without agent: ip %q", vm.IP)
	}

	obs, _, err = runPVE(t, newPVE(t), map[string]any{"include_templates": true}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	tpl := byRef(obs, refGuest, "pve1/qemu/9000")
	if tpl == nil || !tpl.Inventory.(guestInventory).Template || !reflect.DeepEqual(tpl.MACs, []string{"bc:24:11:6a:90:00"}) {
		t.Fatalf("template: %+v", tpl)
	}
}

func TestRunAuthFailure(t *testing.T) {
	srv := newPVE(t)
	obs, _, err := runPVE(t, srv, nil, "wrong-secret")
	if err == nil || !strings.Contains(err.Error(), "Anmeldung") {
		t.Fatalf("err = %v, want authentication error", err)
	}
	if len(obs) != 0 {
		t.Fatalf("no observations expected, got %d", len(obs))
	}
}

func TestRunUnreachable(t *testing.T) {
	srv := newPVE(t)
	url := srv.URL
	srv.Close()
	p := &Plugin{}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"url": url, "credential": 1})
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "pve", Type: plugin.CredAPIToken,
		Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": testSecret}}}
	if err := p.Run(context.Background(), rc); err == nil || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWithoutPermissions(t *testing.T) {
	// a privilege-separated token without ACLs sees empty lists instead of HTTP 403
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/cluster/status" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"data":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"url": srv.URL, "credential": 1})
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "pve", Type: plugin.CredAPIToken,
		Public: map[string]string{"token_id": testTokenID}, Secret: map[string]string{"token": testSecret}}}
	if err := p.Run(context.Background(), rc); err == nil || !strings.Contains(err.Error(), "Leserechte") {
		t.Fatalf("err = %v", err)
	}
	if len(sink.All()) != 0 {
		t.Error("no observations expected")
	}
}

func TestRunWrongCredentialType(t *testing.T) {
	p := &Plugin{}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"url": "https://pve.lan:8006", "credential": 1})
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "ssh", Type: plugin.CredSSH}}
	if err := p.Run(context.Background(), rc); err == nil {
		t.Fatal("expected error for wrong credential type")
	}
}

func TestParseNet(t *testing.T) {
	tests := []struct {
		spec string
		want netIface
	}{
		{"virtio=BC:24:11:2E:C5:6A,bridge=vmbr0,firewall=1",
			netIface{ID: "net0", MAC: "bc:24:11:2e:c5:6a", Model: "virtio", Bridge: "vmbr0", Firewall: true}},
		{"model=e1000e,macaddr=bc:24:11:00:00:01,bridge=vmbr1,tag=30,link_down=1",
			netIface{ID: "net0", MAC: "bc:24:11:00:00:01", Model: "e1000e", Bridge: "vmbr1", VLAN: "30", LinkDown: true}},
		{"name=eth0,bridge=vmbr0,gw=192.168.8.1,hwaddr=BC:24:11:27:22:B5,ip=192.168.8.50/24,ip6=auto,type=veth",
			netIface{ID: "net0", MAC: "bc:24:11:27:22:b5", Name: "eth0", Bridge: "vmbr0", IP: "192.168.8.50/24", IP6: "auto", Gateway: "192.168.8.1"}},
		{"virtio,bridge=vmbr0", netIface{ID: "net0", Model: "virtio", Bridge: "vmbr0"}},
		{"vmxnet3=not-a-mac,bridge=vmbr0", netIface{ID: "net0", Model: "vmxnet3", Bridge: "vmbr0"}},
	}
	for _, tt := range tests {
		if got := parseNet("net0", tt.spec); got != tt.want {
			t.Errorf("parseNet(%q) = %+v, want %+v", tt.spec, got, tt.want)
		}
	}
	if got := staticLXCAddresses([]netIface{{IP: "192.168.8.50/24", IP6: "auto"}, {IP: "dhcp", IP6: "fd00::5/64"}}); !reflect.DeepEqual(got, []string{"192.168.8.50", "fd00::5"}) {
		t.Errorf("static addresses: %v", got)
	}
}

func TestAgentEnabled(t *testing.T) {
	for spec, want := range map[string]bool{
		"1": true, "0": false, "": false, "enabled=1,fstrim_cloned_disks=1": true, "1,type=virtio": true,
		"enabled=0": false, "0,fstrim_cloned_disks=1": false,
	} {
		if got := agentEnabled(spec); got != want {
			t.Errorf("agentEnabled(%q) = %v, want %v", spec, got, want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://pve.lan:8006":             "https://pve.lan:8006/api2/json",
		"https://pve.lan:8006/":            "https://pve.lan:8006/api2/json",
		"https://pve.lan:8006/api2/json/":  "https://pve.lan:8006/api2/json",
		"https://proxy.lan/pve/api2/json":  "https://proxy.lan/pve/api2/json",
		" https://192.168.8.20:8006/#/v1 ": "https://192.168.8.20:8006/api2/json",
	} {
		got, err := baseURL(in)
		if err != nil || got != want {
			t.Errorf("baseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := baseURL("pve.lan:8006"); err == nil {
		t.Error("URL without scheme must be rejected")
	}
}

func TestGuestAddresses(t *testing.T) {
	ifaces := []guestInterface{
		{Name: "br-3f9a1c2b4d5e", HWAddr: "02:42:aa:bb:cc:dd", Inet: "172.18.0.1/16"},
		{Name: "br-lan", HWAddr: "bc:24:11:00:00:09", Inet: "192.168.1.1/24"},
		{Name: "eth1", HWAddr: "bc:24:11:00:00:0a", Inet: "10.0.0.2/24"},
	}
	got := guestAddresses(ifaces, []string{"bc:24:11:00:00:0a"})
	if !reflect.DeepEqual(got, []string{"10.0.0.2", "192.168.1.1"}) {
		t.Errorf("guestAddresses = %v", got)
	}
}
