package wininv

import (
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugins/netsrc"
)

var (
	lan = []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24"), netip.MustParsePrefix("2001:db8:8::/64")}
	now = time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
)

func parse(t *testing.T, name string) (*Result, *Inventory) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Parse(b, false, lan, now)
	if err != nil {
		t.Fatal(err)
	}
	inv, ok := res.Observation.Inventory.(*Inventory)
	if !ok {
		t.Fatalf("inventory %T", res.Observation.Inventory)
	}
	return res, inv
}

func TestServerWithDHCP(t *testing.T) {
	res, inv := parse(t, "server2022-dhcp.json")
	obs := res.Observation
	if res.Sections < 10 || obs.Hostname != "SRV-DHCP01" || obs.DeviceType != "server" || obs.Vendor != "Dell Inc." || obs.Model != "PowerEdge R650xs" {
		t.Fatalf("observation %+v (sections %d)", obs, res.Sections)
	}
	if obs.OS == nil || obs.OS.Name != "Windows Server 2022 Standard" || obs.OS.Accuracy != 100 ||
		!slices.Equal(obs.OS.CPEs, []string{"cpe:2.3:o:microsoft:windows_server_2022:10.0.20348.2849:*:*:*:*:*:x64:*"}) {
		t.Fatalf("os %+v", obs.OS)
	}
	// the iDRAC link-local address and the disconnected port do not identify the host
	if !slices.Equal(obs.IPs, []string{"192.168.8.10"}) || !slices.Equal(obs.MACs, []string{"b0:7b:25:11:22:33"}) {
		t.Fatalf("identity %v %v", obs.IPs, obs.MACs)
	}
	if inv.FQDN != "srv-dhcp01.corp.example" || inv.Domain != "corp.example" || inv.DomainRole != "Server" ||
		inv.Hardware.Chassis != "server" || inv.UptimeSeconds != int64(now.Sub(time.Date(2026, 9, 20, 2, 5, 11, 500e6, time.UTC)).Seconds()) {
		t.Fatalf("inventory %+v %+v", inv, inv.Hardware)
	}
	if inv.Interfaces[0].Addresses[0].Address != "fe80::1c2d:3e4f:5a6b:7c8d" {
		t.Errorf("zone kept: %s", inv.Interfaces[0].Addresses[0].Address)
	}
	// duplicate uninstall entries (32 + 64 bit view) are one package
	if obs.Packages == nil || obs.Packages.Manager != "windows" || len(obs.Packages.Packages) != 3 || obs.Packages.Packages[0].Name != "7-Zip 24.08 (x64)" {
		t.Fatalf("packages %+v", obs.Packages)
	}
	u := inv.Updates
	if !u.RebootRequired || u.Known || len(u.Pending) != 0 || u.LastHotfix == nil || u.LastHotfix.ID != "KB5065432" {
		t.Fatalf("updates %+v", u)
	}
	if inv.Errors["updates.pending"] != "Zeitüberschreitung" {
		t.Errorf("errors %v", inv.Errors)
	}
	if inv.Listening[0].Proto != "tcp" || inv.Listening[0].Port != 135 {
		t.Errorf("listening not sorted: %+v", inv.Listening)
	}

	// DHCP: lease and reservation of the NAS merge, declined addresses are left out
	if inv.DHCP == nil || inv.DHCP.Leases != 2 || inv.DHCP.Reservations != 1 || inv.DHCP.Scopes[0].Leases != 2 {
		t.Fatalf("dhcp summary %+v", inv.DHCP)
	}
	if len(res.DHCP) != 3 {
		t.Fatalf("clients %+v", res.DHCP)
	}
	names := netsrcMerge(res)
	if !slices.Equal(names, []string{"00:11:32:aa:bb:cc Synology NAS static", "3c:22:fb:01:02:03 LT-MUELLER.corp.example dhcp"}) {
		t.Fatalf("merged clients %v", names)
	}
}

func TestLaptop(t *testing.T) {
	res, inv := parse(t, "win11-laptop.json")
	obs := res.Observation
	if obs.DeviceType != "laptop" || obs.OS.Name != "Windows 11 Pro" || obs.OS.Generation != "23H2" ||
		obs.OS.CPEs[0] != "cpe:2.3:o:microsoft:windows_11_23h2:10.0.22631.6199:*:*:*:*:*:x64:*" {
		t.Fatalf("observation %+v %+v", obs, obs.OS)
	}
	// the privacy address and the Hyper-V switch outside the subnets do not count
	if !slices.Equal(obs.IPs, []string{"192.168.8.101", "2001:db8:8:0:3e22:fbff:fe01:203"}) || !slices.Equal(obs.MACs, []string{"3c:22:fb:01:02:03"}) {
		t.Fatalf("identity %v %v", obs.IPs, obs.MACs)
	}
	if inv.Workgroup != "WORKGROUP" || inv.Domain != "" || inv.OS.Timezone != "Mitteleuropäische Zeit" {
		t.Fatalf("inventory %+v", inv)
	}
	if u := inv.Updates; !u.Known || len(u.Pending) != 2 || u.PendingSecurity != 1 {
		t.Fatalf("updates %+v", u)
	}
	if len(inv.Antivirus) != 2 || inv.Antivirus[0].Enabled || !inv.Antivirus[1].Enabled || !inv.Antivirus[1].Current {
		t.Fatalf("antivirus %+v", inv.Antivirus)
	}
	if res.DHCP != nil {
		t.Fatal("DHCP clients without a DHCP server")
	}
}

func TestParseEdgeCases(t *testing.T) {
	if res, err := Parse([]byte("  \r\n"), false, lan, now); err != nil || res.Sections != 0 {
		t.Fatalf("empty output: %+v %v", res, err)
	}
	if _, err := Parse([]byte(`{"format":1,"os":{`), true, lan, now); err == nil || !strings.Contains(err.Error(), "abgeschnitten") {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := Parse([]byte("Get-CimInstance : Zugriff verweigert"), false, lan, now); err == nil {
		t.Fatal("garbage accepted")
	}
	// byte order mark, placeholders of the firmware, no UBR: no CPE
	res, err := Parse(append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"os":{"caption":"Microsoft Windows 10 Pro","version":"10.0.19045","build":"19045","productType":1},
		"computer":{"manufacturer":"To Be Filled By O.E.M.","model":"System Product Name","serial":"Default string","chassis":[3]}}`)...), false, lan, now)
	if err != nil {
		t.Fatal(err)
	}
	obs := res.Observation
	if obs.Vendor != "" || obs.Model != "" || obs.DeviceType != "desktop" || len(obs.OS.CPEs) != 0 || obs.OS.Name != "Windows 10 Pro" {
		t.Fatalf("observation %+v %+v", obs, obs.OS)
	}
	vm, _ := Parse([]byte(`{"computer":{"manufacturer":"QEMU","model":"Standard PC (Q35 + ICH9, 2009)","chassis":[1]},"os":{"productType":3}}`), false, lan, now)
	if vm.Observation.DeviceType != "vm" {
		t.Fatalf("vm %q", vm.Observation.DeviceType)
	}
}

func TestOSCPE(t *testing.T) {
	for _, c := range []struct {
		pt                  int
		build, version, arc string
		want                string
	}{
		{1, "19045", "10.0.19045.6456", "64-Bit", "cpe:2.3:o:microsoft:windows_10_22h2:10.0.19045.6456:*:*:*:*:*:x64:*"},
		{1, "26100", "10.0.26100.6584", "ARM 64-Bit-Prozessor", "cpe:2.3:o:microsoft:windows_11_24h2:10.0.26100.6584:*:*:*:*:*:arm64:*"},
		{2, "17763", "10.0.17763.7792", "64-bit", "cpe:2.3:o:microsoft:windows_server_2019:10.0.17763.7792:*:*:*:*:*:x64:*"},
		{3, "26100", "10.0.26100.6584", "64-bit", "cpe:2.3:o:microsoft:windows_server_2025:10.0.26100.6584:*:*:*:*:*:x64:*"},
		{1, "17763", "10.0.17763.7792", "32-Bit", "cpe:2.3:o:microsoft:windows_10_1809:10.0.17763.7792:*:*:*:*:*:x86:*"},
		{1, "9600", "6.3.9600.21620", "64-Bit", ""}, // Windows 8.1: no patch-level CPEs
		{1, "22631", "10.0.22631", "64-Bit", ""},    // no UBR
	} {
		if got := osCPE(c.pt, c.build, c.version, c.arc); got != c.want {
			t.Errorf("%d %s %s: %q", c.pt, c.build, c.version, got)
		}
	}
}

// netsrcMerge merges the clients like the import does: "mac name kind", sorted.
func netsrcMerge(res *Result) []string {
	var out []string
	for _, c := range netsrc.Merge(res.DHCP) {
		name := c.Name
		if name == "" {
			name = c.Hostname
		}
		out = append(out, c.MAC+" "+name+" "+c.Kind)
	}
	slices.Sort(out)
	return out
}
