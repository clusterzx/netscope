package netutil

import (
	"net/netip"
	"testing"
)

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"AA:BB:CC:DD:EE:FF": "aa:bb:cc:dd:ee:ff",
		"aa-bb-cc-dd-ee-ff": "aa:bb:cc:dd:ee:ff",
		"aabb.ccdd.eeff":    "aa:bb:cc:dd:ee:ff",
		"aabbccddeeff":      "aa:bb:cc:dd:ee:ff",
		"a:b:c:d:e:f":       "0a:0b:0c:0d:0e:0f",
		"94:83:c4:1:2:3":    "94:83:c4:01:02:03",
	}
	for in, want := range cases {
		got, ok := NormalizeMAC(in)
		if !ok || got != want {
			t.Errorf("NormalizeMAC(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff", "zz:bb:cc:dd:ee:ff", "aa:bb:cc"} {
		if _, ok := NormalizeMAC(bad); ok {
			t.Errorf("NormalizeMAC(%q) should fail", bad)
		}
	}
}

func TestRandomizedMAC(t *testing.T) {
	if !IsRandomizedMAC("da:a1:19:00:00:01") {
		t.Error("expected randomized")
	}
	if IsRandomizedMAC("94:83:c4:00:00:01") {
		t.Error("expected global")
	}
}

func TestHostsAndBroadcast(t *testing.T) {
	p := netip.MustParsePrefix("192.168.8.0/24")
	hosts, err := Hosts(p)
	if err != nil || len(hosts) != 254 || hosts[0].String() != "192.168.8.1" || hosts[253].String() != "192.168.8.254" {
		t.Fatalf("hosts: %v %d", err, len(hosts))
	}
	if b, _ := Broadcast(p); b.String() != "192.168.8.255" {
		t.Fatalf("broadcast %v", b)
	}
	if _, err := Hosts(netip.MustParsePrefix("10.0.0.0/8")); err == nil {
		t.Fatal("expected error for /8")
	}
}

func TestIPKeySort(t *testing.T) {
	ips := []string{"192.168.8.10", "192.168.8.2", "10.0.0.1"}
	SortIPs(ips)
	if ips[0] != "10.0.0.1" || ips[1] != "192.168.8.2" {
		t.Fatalf("sort: %v", ips)
	}
	if string(IPKey("192.168.8.2")) >= string(IPKey("192.168.8.10")) {
		t.Fatal("ip key order")
	}
}
