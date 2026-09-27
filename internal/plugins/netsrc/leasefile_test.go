package netsrc

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const iscLeases = `# The format of this file is documented in the dhcpd.leases(5) manual page.
# This lease file was written by isc-dhcp-4.4.3-P1

lease 192.168.1.100 {
  starts 6 2026/09/27 10:00:00;
  ends 6 2026/09/27 14:00:00;
  cltt 6 2026/09/27 10:00:00;
  binding state active;
  next binding state free;
  hardware ethernet aa:bb:cc:00:00:01;
  uid "\001\252\273\314\000\000\001";
  client-hostname "laptop";
}
lease 192.168.1.101 {
  starts 6 2026/09/27 08:00:00;
  ends 6 2026/09/27 10:00:00;
  binding state active;
  hardware ethernet aa:bb:cc:00:00:02;
  client-hostname "old";
}
lease 192.168.1.102 {
  starts 6 2026/09/27 11:00:00;
  ends never;
  binding state active;
  hardware ethernet aa:bb:cc:00:00:03;
}
lease 192.168.1.100 {
  starts 6 2026/09/27 11:00:00;
  ends 6 2026/09/27 15:00:00;
  binding state active;
  hardware ethernet aa:bb:cc:00:00:01;
  client-hostname "laptop-neu";
}
lease 192.168.1.103 {
  starts 6 2026/09/27 11:00:00;
  ends 6 2026/09/27 15:00:00;
  binding state free;
  hardware ethernet aa:bb:cc:00:00:04;
}
`

func TestParseISCLeases(t *testing.T) {
	got := ParseISCLeases(strings.NewReader(iscLeases), now)
	if len(got) != 2 {
		t.Fatalf("leases %+v", got)
	}
	if got[0].IP != "192.168.1.100" || got[0].Hostname != "laptop-neu" || !got[0].Expires.Equal(time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("last block wins: %+v", got[0])
	}
	if got[1].IP != "192.168.1.102" || !got[1].Expires.IsZero() || got[1].Kind != KindDHCP {
		t.Errorf("never ending lease: %+v", got[1])
	}
}

const keaLeases = `address,hwaddr,client_id,valid_lifetime,expire,subnet_id,fqdn_fwd,fqdn_rev,hostname,state,user_context,pool_id
192.168.2.10,aa:bb:cc:00:01:01,01:aa:bb:cc:00:01:01,7200,1790520000,1,0,0,drucker,0,,0
192.168.2.11,aa:bb:cc:00:01:02,,7200,1790500000,1,0,0,abgelaufen,0,,0
192.168.2.12,aa:bb:cc:00:01:03,,7200,1790520000,1,0,0,Erikas iPhone&#x2c privat,0,,0
192.168.2.10,aa:bb:cc:00:01:01,,0,1790520000,1,0,0,drucker,0,,0
192.168.2.13,aa:bb:cc:00:01:04,,7200,1790520000,1,0,0,declined,1,,0
`

func TestParseKeaLeases(t *testing.T) {
	// 1790520000 = 2026-09-28 13:20 UTC, 1790500000 = 2026-09-28 07:46 UTC
	got := ParseKeaLeases(strings.NewReader(keaLeases), time.Unix(1790510000, 0))
	if len(got) != 1 || got[0].IP != "192.168.2.12" || got[0].Hostname != "Erikas iPhone, privat" || got[0].MAC != "aa:bb:cc:00:01:03" {
		t.Fatalf("kea %+v", got)
	}
	if ParseKeaLeases(strings.NewReader(""), now) != nil {
		t.Error("empty file")
	}
}

const arpOut = `? (192.168.1.1) at 00:0c:29:aa:bb:cc on em1 permanent [ethernet]
? (192.168.1.10) at 00:11:22:33:44:55 on em1 expires in 1178 seconds [ethernet]
? (192.168.1.99) at (incomplete) on em1 expired [ethernet]
nas.lan (10.0.5.2) at 3c:52:82:01:02:03 on igb0.5 expires in 30 seconds [vlan]
`

func TestParseARPBSD(t *testing.T) {
	got := ParseARPBSD(strings.NewReader(arpOut), now)
	if len(got) != 2 || got[0].IP != "192.168.1.10" || got[0].Interface != "em1" || got[1].MAC != "3c:52:82:01:02:03" ||
		got[1].Interface != "igb0.5" || got[1].Kind != KindARP {
		t.Fatalf("arp %+v", got)
	}
}
