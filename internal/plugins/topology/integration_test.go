package topology

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/snmp"
	"netscope/internal/plugins/ssh"
	"netscope/internal/settings"
)

// storeSink writes observations through the real inventory core.
type storeSink struct {
	s      *inventory.Store
	plugin string
}

func (k storeSink) Observe(ctx context.Context, o *plugin.Observation) (int64, error) {
	return k.s.Observe(ctx, k.plugin, 1, o)
}

// TestIntegrationPipeline runs ssh → snmp → topology against real hosts through the
// inventory core:
//
//	NETSCOPE_INTEGRATION=1 NETSCOPE_SNMP_TARGET=192.168.8.123:1161 NETSCOPE_SNMP_COMMUNITY=… \
//	go test ./internal/plugins/topology -run Integration -v
//
// The SSH host is 192.168.8.123 (root, ~/.ssh/netscope_lxc), overridable like in the ssh
// integration test; the SNMP agent must run on the same host.
func TestIntegrationPipeline(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" || os.Getenv("NETSCOPE_SNMP_TARGET") == "" {
		t.Skip("NETSCOPE_INTEGRATION=1 and NETSCOPE_SNMP_TARGET not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st, err := settings.Load(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	store, err := inventory.New(ctx, d, nil, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSubnet(ctx, &inventory.Subnet{CIDR: "192.168.8.0/24", Name: "lan", Gateway: "192.168.8.1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	subnets, err := store.Subnets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sshHost := os.Getenv("NETSCOPE_SSH_HOST")
	if sshHost == "" {
		sshHost = "192.168.8.123"
	}
	// devices as a presence scan would find them: the host, and the router known by IP only
	hostID, err := store.Observe(ctx, "arpscan", 1, &plugin.Observation{IP: sshHost, Present: true})
	if err != nil {
		t.Fatal(err)
	}
	routerID, err := store.Observe(ctx, "arpscan", 1, &plugin.Observation{IP: "192.168.8.1", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	target := func() plugin.Targets {
		dev, err := store.Device(ctx, hostID)
		if err != nil {
			t.Fatal(err)
		}
		return plugin.Targets{Subnets: subnets, Devices: []plugin.DeviceInfo{*dev}}
	}

	// ssh
	home, _ := os.UserHomeDir()
	keyPath := os.Getenv("NETSCOPE_SSH_KEY")
	if keyPath == "" {
		keyPath = filepath.Join(home, ".ssh", "netscope_lxc")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	user := os.Getenv("NETSCOPE_SSH_USER")
	if user == "" {
		user = "root"
	}
	sp := &ssh.Plugin{}
	rc, _, _ := plugintest.RunContext(t, sp, map[string]any{"credentials": []any{1}})
	rc.Sink, rc.Inventory, rc.Targets = storeSink{store, "ssh"}, store, target()
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "key", Type: plugin.CredSSH, Public: map[string]string{"username": user},
		Secret: map[string]string{"private_key": string(key)}}}
	if err := sp.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}

	// snmp
	_, portS, _ := net.SplitHostPort(os.Getenv("NETSCOPE_SNMP_TARGET"))
	port, _ := strconv.Atoi(portS)
	np := &snmp.Plugin{}
	rc, _, _ = plugintest.RunContext(t, np, map[string]any{"credentials": []any{1}, "port": port})
	rc.Sink, rc.Inventory, rc.Targets = storeSink{store, "snmp"}, store, target()
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "v2c", Type: plugin.CredSNMPv2c, Secret: map[string]string{"community": os.Getenv("NETSCOPE_SNMP_COMMUNITY")}}}
	if err := np.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}

	// topology
	tp := &Plugin{}
	rc, _, _ = plugintest.RunContext(t, tp, nil)
	rc.DB = d
	if err := tp.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}

	host, err := store.Device(ctx, hostID)
	if err != nil {
		t.Fatal(err)
	}
	router, err := store.Device(ctx, routerID)
	if err != nil {
		t.Fatal(err)
	}
	var pkgs, containers, inventories int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM packages WHERE device_id = ? AND gone_at IS NULL", hostID).Scan(&pkgs)
	_ = d.R.QueryRow("SELECT COUNT(*) FROM containers WHERE device_id = ? AND gone_at IS NULL", hostID).Scan(&containers)
	_ = d.R.QueryRow("SELECT COUNT(*) FROM device_inventory WHERE device_id = ?", hostID).Scan(&inventories)
	rels, err := store.DeviceRelations(ctx, hostID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("host: name=%q os=%q macs=%v ips=%v packages=%d containers=%d inventories=%d", host.Hostname, host.OS, host.MACs, host.IPs,
		pkgs, containers, inventories)
	t.Logf("router: macs=%v, relations of host: %+v", router.MACs, rels)
	if host.Hostname == "" || host.OS == "" || len(host.MACs) == 0 || pkgs < 100 || inventories != 2 {
		t.Errorf("host = %+v, packages %d, inventories %d", host, pkgs, inventories)
	}
	if len(router.MACs) != 1 {
		t.Errorf("the router must have learned its MAC from the ARP table: %+v", router)
	}
	found := false
	for _, r := range rels {
		if r.Kind == plugin.RelL3 && r.ParentID == routerID && r.ChildID == hostID && r.Source == source {
			found = true
		}
	}
	if !found {
		t.Errorf("expected l3 edge router -> host, got %+v", rels)
	}
}
