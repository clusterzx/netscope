package netbios

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestRequest(t *testing.T) {
	// The request the fixtures were captured with (transaction id 0x1337).
	want := []byte{0x13, 0x37, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 'C', 'K'}
	want = append(want, bytes.Repeat([]byte{'A'}, 30)...)
	want = append(want, 0x00, 0x00, 0x21, 0x00, 0x01)
	if got := request(0x1337); !bytes.Equal(got, want) {
		t.Fatalf("request:\n% x\nwant:\n% x", got, want)
	}
}

func TestParseWindows(t *testing.T) {
	st, err := parseStatus(plugintest.Fixture(t, "nbstat-windows-192.168.8.11.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != 0x1337 || len(st.Names) != 4 {
		t.Fatalf("status: %+v", st)
	}
	if st.Hostname() != "DESKTOP-FANI8MD" || st.Domain() != "WORKGROUP" || st.MAC != "a8:a1:59:77:91:0e" {
		t.Fatalf("host=%q domain=%q mac=%q", st.Hostname(), st.Domain(), st.MAC)
	}
	e := st.Names[1]
	if e.Name != "WORKGROUP" || !e.Group || e.Suffix != "0x00" || !e.Active {
		t.Fatalf("entry: %+v", e)
	}
	if st.Names[2].Suffix != "0x20" || st.Names[3].Suffix != "0x1E" {
		t.Fatalf("suffixes: %+v", st.Names)
	}
	table := st.Table()
	if !strings.Contains(table, "DESKTOP-FANI8MD <20> -       M <ACTIVE>") || !strings.Contains(table, "MAC Address = A8-A1-59-77-91-0E") {
		t.Fatalf("table:\n%s", table)
	}
}

func TestParseSamba(t *testing.T) {
	st, err := parseStatus(plugintest.Fixture(t, "nbstat-samba-172.17.0.2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Names) != 6 || st.Hostname() != "NASBOX" || st.Domain() != "HOMELAB" || st.MAC != "" {
		t.Fatalf("host=%q domain=%q mac=%q names=%+v", st.Hostname(), st.Domain(), st.MAC, st.Names)
	}
	if st.Names[3].Name != "..__MSBROWSE__." || !st.Names[3].Group || st.Names[3].Suffix != "0x01" {
		t.Fatalf("browse entry: %+v", st.Names[3])
	}
}

func TestParseMalformed(t *testing.T) {
	good := plugintest.Fixture(t, "nbstat-windows-192.168.8.11.bin")
	for n := 0; n < len(good)-107; n++ { // every truncation before the unit id
		if _, err := parseStatus(good[:n]); err == nil {
			t.Fatalf("truncated response (%d bytes) accepted", n)
		}
	}
	q := append([]byte(nil), good...)
	binary.BigEndian.PutUint16(q[2:4], 0x0000) // not a response
	if _, err := parseStatus(q); err == nil {
		t.Fatal("query accepted as response")
	}
	// answer that announces more statistics than it carries is still usable
	st, err := parseStatus(good[:len(good)-20])
	if err != nil || st.Hostname() != "DESKTOP-FANI8MD" {
		t.Fatalf("short statistics: %v %+v", err, st)
	}
}

// fakeNBNS answers node status requests with a fixture (transaction id patched).
func fakeNBNS(t *testing.T, fixture string) uint16 {
	t.Helper()
	resp := plugintest.Fixture(t, fixture)
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		first := true
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			if first { // drop the first request to exercise the retransmission
				first = false
				continue
			}
			out := append([]byte(nil), resp...)
			copy(out[:2], buf[:2])
			_, _ = pc.WriteTo(out, from)
		}
	}()
	t.Cleanup(func() {
		pc.Close()
		<-done
	})
	return uint16(pc.LocalAddr().(*net.UDPAddr).Port)
}

func TestRun(t *testing.T) {
	port := fakeNBNS(t, "nbstat-windows-192.168.8.11.bin")
	p := &Plugin{port: port}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"timeout": "1s"})
	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 3, PrimaryIP: "127.0.0.1"}, {ID: 4}}
	start := time.Now()
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 1 {
		t.Fatalf("observations: %+v", obs)
	}
	o := obs[0]
	if o.DeviceID != 3 || o.IP != "127.0.0.1" || !o.Present || o.Hostname != "DESKTOP-FANI8MD" ||
		o.Attrs["netbios.domain"] != "WORKGROUP" || o.Attrs["netbios.mac"] != "a8:a1:59:77:91:0e" || o.Inventory == nil {
		t.Fatalf("observation: %+v", o)
	}
	if d := time.Since(start); d < 400*time.Millisecond {
		t.Fatalf("answer before retransmission: %s", d)
	}
}

func TestQueryNoAnswer(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	dst := netip.MustParseAddrPort(pc.LocalAddr().String())
	st, _, err := query(context.Background(), dst, 200*time.Millisecond)
	if st != nil || err != nil {
		t.Fatalf("got %+v, %v", st, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := query(ctx, dst, time.Second); err == nil {
		t.Fatal("cancelled query returned no error")
	}
}
