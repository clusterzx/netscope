package plugin

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestLocalOnly(t *testing.T) {
	routed := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	in := Targets{
		DeviceMode: true,
		Subnets: []SubnetTarget{
			{CIDR: netip.MustParsePrefix("192.168.8.0/24")},
			{CIDR: netip.MustParsePrefix("192.168.1.0/24"), Routed: true},
			{CIDR: netip.MustParsePrefix("10.20.0.0/24"), Routed: true}, // routed via a router, not in the list
		},
		Devices: []DeviceInfo{
			{ID: 1, PrimaryIP: "192.168.8.10", IPs: []string{"192.168.8.10"}},
			{ID: 2, PrimaryIP: "192.168.1.2", IPs: []string{"192.168.1.2"}},                 // only behind the tunnel
			{ID: 3, PrimaryIP: "192.168.1.3", IPs: []string{"192.168.1.3", "192.168.8.30"}}, // dual-homed
			{ID: 4, Name: "no address"}, // kept
		},
	}
	out, skipped := in.LocalOnly(routed)
	if len(out.Subnets) != 1 || out.Subnets[0].CIDR.String() != "192.168.8.0/24" || !out.DeviceMode {
		t.Errorf("subnets: %+v", out.Subnets)
	}
	var ids []int64
	for _, d := range out.Devices {
		ids = append(ids, d.ID)
	}
	if !reflect.DeepEqual(ids, []int64{1, 3, 4}) {
		t.Fatalf("devices: %v", ids)
	}
	if d := out.Devices[1]; d.PrimaryIP != "192.168.8.30" || !reflect.DeepEqual(d.IPs, []string{"192.168.8.30"}) {
		t.Errorf("dual-homed device keeps its local address only: %+v", d)
	}
	want := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24"), netip.MustParsePrefix("10.20.0.0/24")}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped: %v", skipped)
	}
	if same, s := in.LocalOnly(nil); len(s) != 0 || len(same.Devices) != 4 {
		t.Error("without routed prefixes nothing changes")
	}
}
