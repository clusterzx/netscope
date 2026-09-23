package mdns

import (
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

var lan = netip.MustParsePrefix("192.168.8.0/24")

// loadFixtures feeds all captured responses into a collector. The source address is the
// file name prefix ("192.168.8.33-services.bin").
func loadFixtures(t *testing.T) *collector {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.bin"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v", err)
	}
	sort.Strings(files)
	col := newCollector()
	for _, f := range files {
		ip, _, _ := strings.Cut(filepath.Base(f), "-")
		src := netip.MustParseAddr(ip)
		col.add(src, plugintest.Fixture(t, filepath.Base(f)))
	}
	return col
}

func hostsByIP(col *collector) map[string]*host {
	out := map[string]*host{}
	for _, h := range col.hosts(lan.Contains) {
		out[h.IP.String()] = h
	}
	return out
}

func TestEnumeration(t *testing.T) {
	col := loadFixtures(t)
	types := col.serviceTypes()
	want := []string{"_dosvc._tcp.local.", "_ftp._tcp.local.", "_home-assistant._tcp.local.", "_http._tcp.local.", "_hue._tcp.local.",
		"_matter._tcp.local.", "_nvstream._tcp.local.", "_shelly._tcp.local.",
		"_spotify-connect._tcp.local.", "_spotify-social-listening._tcp.local.", "_tr064._tcp.local.",
		"_vstreamdeck2._tcp.local.", "_wled._tcp.local."}
	if strings.Join(types, " ") != strings.Join(want, " ") {
		t.Fatalf("types:\n%v\nwant:\n%v", types, want)
	}
}

func TestAggregate(t *testing.T) {
	hosts := hostsByIP(loadFixtures(t))
	cases := []struct {
		ip, hostname, services, model, devType string
		present                                bool
	}{
		{"192.168.8.1", "GL-MT6000.local", "", "", "", true},
		{"192.168.8.11", "DESKTOP-FANI8MD.local", "_dosvc._tcp,_nvstream._tcp,_vstreamdeck2._tcp", "", "", true},
		{"192.168.8.33", "ShellyPlugMG3-70AF09E33808.local", "_http._tcp,_shelly._tcp", "", "smart-home", true},
		{"192.168.8.40", "shellyem3-244CAB436469.local", "_http._tcp", "", "", true},
		{"192.168.8.44", "fritz-box.fritz.box", "_ftp._tcp,_tr064._tcp", "", "", true},
		{"192.168.8.65", "Android-2.local", "_matter._tcp,_spotify-connect._tcp", "", "", true},
		{"192.168.8.71", "", "_matter._tcp,_spotify-connect._tcp", "", "", true},
		{"192.168.8.75", "ecb5fabcc753.local", "_hue._tcp", "BSB002", "smart-home", true},
		{"192.168.8.135", "homeassistant.local", "_home-assistant._tcp,_workstation._tcp", "", "", true},
		// The Fire TV relays the service type list of the whole network: none of those
		// types may be attributed to it.
		{"192.168.8.178", "", "", "", "", true},
	}
	for _, c := range cases {
		h, ok := hosts[c.ip]
		if !ok {
			t.Errorf("%s missing", c.ip)
			continue
		}
		obs := observation(h)
		if obs.Hostname != c.hostname || obs.Attrs["mdns.services"] != c.services || obs.Model != c.model ||
			obs.DeviceType != c.devType || obs.Present != c.present {
			t.Errorf("%s: hostname=%q services=%q model=%q type=%q present=%v", c.ip, obs.Hostname,
				obs.Attrs["mdns.services"], obs.Model, obs.DeviceType, obs.Present)
		}
	}
	if len(hosts) != len(cases) {
		var ips []string
		for ip := range hosts {
			ips = append(ips, ip)
		}
		t.Errorf("unexpected hosts: %v", ips)
	}
}

func TestServiceDetails(t *testing.T) {
	hosts := hostsByIP(loadFixtures(t))
	hue := hosts["192.168.8.75"].Services[0]
	if hue.Type != "_hue._tcp" || hue.Instance != "Hue Bridge - BCC753" || hue.Port != 443 ||
		hue.Target != "ecb5fabcc753.local" || hue.TXT["bridgeid"] != "ecb5fafffebcc753" {
		t.Errorf("hue: %+v", hue)
	}
	var ha *Service
	for i, s := range hosts["192.168.8.135"].Services {
		if s.Type == "_home-assistant._tcp" {
			ha = &hosts["192.168.8.135"].Services[i]
		}
	}
	if ha == nil || ha.Port != 8123 || ha.TXT["version"] != "2026.5.1" || ha.TXT["internal_url"] != "http://192.168.8.135:8123" {
		t.Errorf("home assistant: %+v", ha)
	}
	// Two instance names on the Shelly plug, one with a non-ASCII name.
	var instances []string
	for _, s := range hosts["192.168.8.33"].Services {
		instances = append(instances, s.Type+"/"+s.Instance)
	}
	want := "_http._tcp/Proxmox-Büro _http._tcp/shellyplugmg3-70af09e33808 _shelly._tcp/Proxmox-Büro _shelly._tcp/shellyplugmg3-70af09e33808"
	if strings.Join(instances, " ") != want {
		t.Errorf("shelly instances: %v", instances)
	}
	// Matter instance found via the sub-type PTR.
	m := hosts["192.168.8.65"].Services
	if len(m) != 2 || m[0].Type != "_matter._tcp" || m[0].Port != 5541 || m[1].Instance != "SpotifyConnect (2)" {
		t.Errorf("65: %+v", m)
	}
	obs := observation(hosts["192.168.8.75"])
	inv, _ := obs.Inventory.(map[string]any)
	if names, _ := inv["names"].([]string); len(names) != 1 || names[0] != "ecb5fabcc753.local" {
		t.Errorf("inventory names: %+v", inv)
	}
	if !strings.Contains(obs.Raw, `service _hue._tcp "Hue Bridge - BCC753" port 443 bridgeid=ecb5fafffebcc753 modelid=BSB002`) {
		t.Errorf("raw:\n%s", obs.Raw)
	}
}

func TestGoodbyeIgnored(t *testing.T) {
	col := newCollector()
	col.add(netip.MustParseAddr("192.168.8.11"), plugintest.Fixture(t, "192.168.8.11-dosvc-goodbye.bin"))
	if hs := col.hosts(lan.Contains); len(hs) != 0 {
		t.Fatalf("goodbye records produced hosts: %+v", hs[0])
	}
}

func TestMalformedPackets(t *testing.T) {
	good := plugintest.Fixture(t, "192.168.8.33-services.bin")
	src := netip.MustParseAddr("192.168.8.33")
	for n := 0; n < len(good); n++ {
		col := newCollector()
		col.add(src, good[:n]) // must never panic
		col.hosts(lan.Contains)
	}
	col := newCollector()
	col.add(src, make([]byte, maxPacket+1))
	q := queries([]string{servicesDomain})[0]
	col.add(src, q) // our own query looped back
	if len(col.responders()) != 0 || col.packetCount() != 0 {
		t.Fatal("query or oversized packet accepted")
	}
	// A record with a dot inside a label is rejected by dnsmessage; earlier records stay.
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	_ = b.StartAnswers()
	_ = b.AResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("nas.local."), Class: dnsmessage.ClassINET, TTL: 120},
		dnsmessage.AResource{A: [4]byte{192, 168, 8, 50}})
	pkt, _ := b.Finish()
	pkt = append(pkt[:len(pkt):len(pkt)], []byte{0x04, 'a', '.', 'b', 'c', 0x00, 0x00, 0x01, 0x00, 0x01, 0, 0, 0, 120, 0, 4, 1, 2, 3, 4}...)
	pkt[7] = 2 // ANCOUNT
	col.add(netip.MustParseAddr("192.168.8.50"), pkt)
	hs := col.hosts(lan.Contains)
	if len(hs) != 1 || hs[0].hostname() != "nas.local" {
		t.Fatalf("partial packet: %+v", hs)
	}
}

func TestHostnameChoice(t *testing.T) {
	ip := netip.MustParseAddr("192.168.8.44")
	cases := []struct {
		h    host
		want string
	}{
		{host{IP: ip, Reverse: "linux.local", ANames: []string{"Android-2.local", "DEDDD0E7EB1E.local"}}, "Android-2.local"},
		{host{IP: ip, ANames: []string{"192-168-8-44.local"}, SRVNames: []string{"192-168-8-44.fritz.box", "fritz-box.fritz.box"}}, "fritz-box.fritz.box"},
		{host{IP: ip, ANames: []string{"none.local", "0AEAE6F03659.local"}}, ""},
		{host{IP: ip, Reverse: "ecb5fabcc753.local", ANames: []string{"ecb5fabcc753.local"}}, "ecb5fabcc753.local"},
		{host{IP: ip, ANames: []string{"zeta.local", "Alpha.local"}}, "Alpha.local"},
	}
	for _, c := range cases {
		if got := c.h.hostname(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.h, got, c.want)
		}
	}
}

func TestGuessType(t *testing.T) {
	cases := []struct {
		types []string
		model string
		want  string
	}{
		{[]string{"_googlecast._tcp"}, "Chromecast", "media-player"},
		{[]string{"_airplay._tcp", "_raop._tcp"}, "AppleTV5,3", "media-player"},
		{[]string{"_airplay._tcp", "_raop._tcp"}, "AudioAccessory5,1", "speaker"},
		{[]string{"_airplay._tcp", "_sonos._tcp", "_spotify-connect._tcp"}, "", "speaker"},
		{[]string{"_ipp._tcp", "_ipps._tcp", "_pdl-datastream._tcp", "_http._tcp"}, "", "printer"},
		{[]string{"_hap._tcp"}, "", "smart-home"},
		{[]string{"_esphomelib._tcp"}, "", "iot"},
		{[]string{"_smb._tcp", "_adisk._tcp", "_afpovertcp._tcp"}, "", "nas"},
		{[]string{"_smb._tcp", "_workstation._tcp"}, "", ""},
		{[]string{"_companion-link._tcp"}, "", "phone"},
		{[]string{"_http._tcp", "_ssh._tcp"}, "", ""},
	}
	for _, c := range cases {
		if got := guessType(c.types, c.model); got != c.want {
			t.Errorf("%v %q: got %q, want %q", c.types, c.model, got, c.want)
		}
	}
}

func TestTXTHints(t *testing.T) {
	svcs := []Service{
		{Type: "_http._tcp", TXT: map[string]string{"path": "/"}},
		{Type: "_ipp._tcp", TXT: map[string]string{"product": "(Brother HL-L2350DW series)", "usb_MFG": "Brother", "ty": "Brother HL-L2350DW"}},
	}
	if m := txtValue(svcs, modelKeys); m != "Brother HL-L2350DW" {
		t.Errorf("model %q", m)
	}
	if v := txtValue(svcs, vendorKeys); v != "Brother" {
		t.Errorf("vendor %q", v)
	}
	delete(svcs[1].TXT, "ty")
	if m := txtValue(svcs, modelKeys); m != "Brother HL-L2350DW series" {
		t.Errorf("model from product %q", m)
	}
	if got := parseTXT([]string{"a=1", "a=2", "flag", "", "=x", "b="}); len(got) != 3 || got["a"] != "1" || got["flag"] != "" || got["b"] != "" {
		t.Errorf("parseTXT: %v", got)
	}
}

func TestQueriesAndNames(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, reverseName(netip.AddrFrom4([4]byte{192, 168, 8, byte(i)})))
	}
	msgs := queries(names)
	if len(msgs) != 3 {
		t.Fatalf("%d messages", len(msgs))
	}
	var m dnsmessage.Message
	if err := m.Unpack(msgs[0]); err != nil || len(m.Questions) != questionsPerQuery || m.Header.Response {
		t.Fatalf("query: %v %+v", err, m.Header)
	}
	if m.Questions[3].Name.String() != "3.8.168.192.in-addr.arpa." || m.Questions[3].Type != dnsmessage.TypePTR {
		t.Fatalf("question: %+v", m.Questions[3])
	}
	if a, ok := parseReverse("75.8.168.192.in-addr.arpa."); !ok || a.String() != "192.168.8.75" {
		t.Fatal("parseReverse")
	}
	for name, want := range map[string]string{
		"Hue Bridge - BCC753._hue._tcp.local.":                  "_hue._tcp|Hue Bridge - BCC753",
		"8CACFD3EE7426A62-0547B211AB107E91._matter._tcp.local.": "_matter._tcp|8CACFD3EE7426A62-0547B211AB107E91",
		"Printer._ipp._TCP.local.":                              "_ipp._tcp|Printer",
		"_http._tcp.local.":                                     "",
		"host.local.":                                           "",
	} {
		typ, inst, ok := splitInstance(name)
		got := ""
		if ok {
			got = typ + "|" + inst
		}
		if got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestBuildSessions(t *testing.T) {
	ifaceFor := func(a netip.Addr) string {
		if lan.Contains(a) {
			return "eth0"
		}
		return ""
	}
	known := []netip.Addr{netip.MustParseAddr("192.168.8.1"), netip.MustParseAddr("10.9.9.9")}
	sessions, errs := buildSessions(plugin.Targets{Subnets: []plugin.SubnetTarget{
		{CIDR: lan}, {CIDR: netip.MustParsePrefix("172.16.0.0/24")}, {CIDR: netip.MustParsePrefix("fd00::/64")},
	}}, known, ifaceFor)
	if len(sessions) != 1 || len(errs) != 1 || sessions[0].iface != "eth0" || len(sessions[0].reverse) != 1 {
		t.Fatalf("sessions=%+v errs=%v", sessions, errs)
	}
	if !sessions[0].scope(netip.MustParseAddr("192.168.8.77")) || sessions[0].scope(netip.MustParseAddr("192.168.9.1")) {
		t.Fatal("subnet scope")
	}
	sessions, _ = buildSessions(plugin.Targets{DeviceMode: true, Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.75"}}}, nil, ifaceFor)
	if len(sessions) != 1 || !sessions[0].scope(netip.MustParseAddr("192.168.8.75")) || sessions[0].scope(netip.MustParseAddr("192.168.8.1")) ||
		len(sessions[0].reverse) != 1 {
		t.Fatalf("device sessions: %+v", sessions)
	}
}

func TestSchema(t *testing.T) {
	rc, _, _ := plugintest.RunContext(t, &Plugin{}, nil)
	if rc.Settings.Duration("listen").String() != "5s" {
		t.Fatal(rc.Settings.Map())
	}
	if _, err := os.Stat(filepath.Join("testdata", "192.168.8.1-reverse-ptr.bin")); err != nil {
		t.Fatal(err)
	}
}
