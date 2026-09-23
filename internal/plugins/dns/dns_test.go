package dns

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// dnsServer is a minimal authoritative server for reverse zones used in tests.
type dnsServer struct {
	addr    string
	ptr     map[string]string // reverse name → target
	drop    map[string]bool   // reverse names that never get an answer
	queries atomic.Int64
}

func startDNS(t *testing.T, ptr map[string]string, drop map[string]bool) *dnsServer {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &dnsServer{addr: pc.LocalAddr().String(), ptr: ptr, drop: drop}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var m dnsmessage.Message
			if err := m.Unpack(buf[:n]); err != nil || len(m.Questions) != 1 {
				continue
			}
			s.queries.Add(1)
			q := m.Questions[0]
			name := strings.ToLower(q.Name.String())
			if s.drop[name] {
				continue
			}
			resp := dnsmessage.Message{
				Header: dnsmessage.Header{ID: m.Header.ID, Response: true, Authoritative: true,
					RecursionDesired: m.Header.RecursionDesired, RecursionAvailable: true},
				Questions: m.Questions,
			}
			if target, ok := s.ptr[name]; ok && q.Type == dnsmessage.TypePTR {
				resp.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(target)},
				}}
			} else {
				resp.Header.RCode = dnsmessage.RCodeNameError
			}
			b, err := resp.Pack()
			if err != nil {
				continue
			}
			_, _ = pc.WriteTo(b, from)
		}
	}()
	t.Cleanup(func() {
		pc.Close()
		<-done
	})
	return s
}

func TestResolverAddr(t *testing.T) {
	cases := map[string]string{
		"192.168.8.1":      "192.168.8.1:53",
		"192.168.8.1:5353": "192.168.8.1:5353",
		"dns.lan":          "dns.lan:53",
		"dns.lan:54":       "dns.lan:54",
		"fd00::1":          "[fd00::1]:53",
		"[fd00::1]:5353":   "[fd00::1]:5353",
		" 10.0.0.1 ":       "10.0.0.1:53",
	}
	for in, want := range cases {
		got, err := resolverAddr(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "host:0", "host:99999", ":53", "a b", "http://x"} {
		if _, err := resolverAddr(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	p := &Plugin{}
	if err := p.ValidateSettings(plugin.NewSettings(map[string]any{"resolver": "x y"})); err == nil {
		t.Error("ValidateSettings accepted invalid resolver")
	}
}

func TestRun(t *testing.T) {
	srv := startDNS(t, map[string]string{
		"1.2.0.192.in-addr.arpa.":  "router.lan.",
		"20.2.0.192.in-addr.arpa.": "nas.lan.",
	}, map[string]bool{"3.2.0.192.in-addr.arpa.": true})
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"resolver": srv.addr, "timeout": "1s"})
	rc.Targets.Devices = []plugin.DeviceInfo{
		{ID: 1, PrimaryIP: "192.0.2.1"},
		{ID: 2, PrimaryIP: "192.0.2.2"}, // NXDOMAIN
		{ID: 3, PrimaryIP: "192.0.2.3"}, // no answer
		{ID: 4, PrimaryIP: "192.0.2.4", IPs: []string{"192.0.2.4", "192.0.2.20"}},
		{ID: 5},
	}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	got := map[int64]plugin.Observation{}
	for _, o := range sink.All() {
		got[o.DeviceID] = o
	}
	if len(got) != 3 {
		t.Fatalf("observations: %+v", sink.All())
	}
	if o := got[1]; o.Hostname != "router.lan" || o.IP != "192.0.2.1" || len(o.Clear) != 0 || o.Present {
		t.Errorf("device 1: %+v", o)
	}
	if o := got[2]; o.Hostname != "" || len(o.Clear) != 1 || o.Clear[0] != "hostname" {
		t.Errorf("device 2: %+v", o)
	}
	// only the primary IP is queried by default
	if o := got[4]; len(o.Clear) != 1 {
		t.Errorf("device 4: %+v", o)
	}
	if _, ok := got[3]; ok {
		t.Error("timeout must not produce an observation")
	}
}

func TestRunAllIPs(t *testing.T) {
	srv := startDNS(t, map[string]string{"20.2.0.192.in-addr.arpa.": "nas.lan."}, nil)
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"resolver": srv.addr, "timeout": "1s", "all_ips": true})
	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 4, PrimaryIP: "192.0.2.4", IPs: []string{"192.0.2.4", "192.0.2.20"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 1 || obs[0].Hostname != "nas.lan" || obs[0].IP != "192.0.2.20" || len(obs[0].Clear) != 0 {
		t.Fatalf("observations: %+v", obs)
	}
}

func TestRunResolverDown(t *testing.T) {
	srv := startDNS(t, nil, map[string]bool{"1.2.0.192.in-addr.arpa.": true})
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"resolver": srv.addr, "timeout": "1s"})
	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.0.2.1"}}
	if err := p.Run(context.Background(), rc); err == nil {
		t.Fatal("expected an error when the resolver never answers")
	}
	if len(sink.All()) != 0 {
		t.Fatal("no observation expected")
	}
}
