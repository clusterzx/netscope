package wol

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestMagicPacket(t *testing.T) {
	mac, _ := net.ParseMAC("a8:a1:59:77:91:0e")
	pkt, err := magicPacket(mac, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkt) != 102 || !bytes.Equal(pkt[:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("header: % x", pkt[:6])
	}
	for i := 0; i < 16; i++ {
		if !bytes.Equal(pkt[6+i*6:12+i*6], mac) {
			t.Fatalf("repetition %d: % x", i, pkt[6+i*6:12+i*6])
		}
	}
	pw, err := parseSecureOn("01:23:45:67:89:AB")
	if err != nil {
		t.Fatal(err)
	}
	pkt, _ = magicPacket(mac, pw)
	if len(pkt) != 108 || !bytes.Equal(pkt[102:], []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab}) {
		t.Fatalf("secureon: % x", pkt[96:])
	}
	if _, err := magicPacket(net.HardwareAddr{1, 2, 3}, nil); err == nil {
		t.Fatal("short MAC accepted")
	}
}

func TestParseSecureOn(t *testing.T) {
	for _, ok := range []string{"", "0123456789ab", "01-23-45-67-89-AB", "01:23:45:67:89:ab"} {
		if _, err := parseSecureOn(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"0123", "0123456789abcd", "zz23456789ab"} {
		if _, err := parseSecureOn(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBroadcastTargets(t *testing.T) {
	subnets := []plugin.SubnetTarget{
		{CIDR: netip.MustParsePrefix("192.168.8.0/24")},
		{CIDR: netip.MustParsePrefix("10.20.0.0/22")},
		{CIDR: netip.MustParsePrefix("10.99.0.7/32")},
	}
	got := broadcastTargets([]string{"192.168.8.44", "10.20.1.5", "10.99.0.7", "fd00::5", ""}, subnets,
		[]string{"10.30.0.255", "192.168.8.255", "bad"})
	want := []string{"10.20.3.255", "192.168.8.255", "255.255.255.255", "10.30.0.255"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// Device outside every configured subnet: limited broadcast only.
	if got := broadcastTargets([]string{"172.16.5.5"}, subnets, nil); len(got) != 1 || got[0].String() != "255.255.255.255" {
		t.Fatalf("got %v", got)
	}
}

type sent struct {
	pkt []byte
	dst string
}

func TestWakeAction(t *testing.T) {
	var (
		mu  sync.Mutex
		log []sent
	)
	p := &Plugin{send: func(ctx context.Context, pkt []byte, dst netip.AddrPort) error {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, sent{pkt: append([]byte(nil), pkt...), dst: dst.String()})
		return nil
	}}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"port": 7, "repeat": 2, "extra_broadcasts": []any{"10.30.0.255"}})
	rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix("192.168.8.0/24")}}}
	rc.Targets.Devices = []plugin.DeviceInfo{
		{ID: 1, Name: "Desktop", PrimaryIP: "192.168.8.11", PrimaryMAC: "a8:a1:59:77:91:0e", MACs: []string{"a8:a1:59:77:91:0e", "a8:a1:59:77:91:0f"}},
		{ID: 2, Name: "Drucker", PrimaryIP: "192.168.8.50"},
	}
	res, err := p.RunAction(context.Background(), rc, "wake", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 2 MACs × 3 targets × 2 repetitions
	if len(log) != 12 {
		t.Fatalf("sent %d packets", len(log))
	}
	dsts := map[string]int{}
	for _, s := range log {
		dsts[s.dst]++
		if len(s.pkt) != 102 {
			t.Fatalf("packet length %d", len(s.pkt))
		}
	}
	if dsts["192.168.8.255:7"] != 4 || dsts["255.255.255.255:7"] != 4 || dsts["10.30.0.255:7"] != 4 {
		t.Fatalf("destinations: %v", dsts)
	}
	if !strings.Contains(res.Message, "1 Gerät") || !strings.Contains(res.Message, "Drucker") {
		t.Fatalf("message: %s", res.Message)
	}

	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 2, Name: "Drucker", PrimaryIP: "192.168.8.50"}}
	if _, err := p.RunAction(context.Background(), rc, "wake", nil); err == nil {
		t.Fatal("expected error for a device without MAC")
	}
	if _, err := p.RunAction(context.Background(), rc, "other", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestSendUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer pc.Close()
	dst := netip.MustParseAddrPort(pc.LocalAddr().String())
	mac, _ := net.ParseMAC("02:4e:53:00:00:01")
	pkt, _ := magicPacket(mac, nil)
	if err := sendUDP(context.Background(), pkt, dst); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 200)
	n, _, err := pc.ReadFrom(buf)
	if err != nil || !bytes.Equal(buf[:n], pkt) {
		t.Fatalf("received %d bytes: %v", n, err)
	}
}
