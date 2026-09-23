package snmp

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/plugin/plugintest"
)

// Recordings in testdata/ use the snmpsim ".snmprec" format ("OID|type|value"; a type
// with suffix "x" carries a hex-encoded value):
//
//   - netsnmp-alpine.snmprec: real walk of a net-snmp 5.9.4 agent (alpine:3.22 container
//     with host networking on the Ubuntu LXC), captured with TestCaptureSnmprec.
//   - librenms-*.snmprec: real device recordings from the LibreNMS test suite
//     (github.com/librenms/librenms, tests/snmpsim), reduced to the system, IF-MIB,
//     IP-MIB, BRIDGE-MIB, Q-BRIDGE-MIB and LLDP-MIB subtrees.

// loadSnmprec parses a recording into PDUs typed like gosnmp returns them.
func loadSnmprec(t testing.TB, name string) []gosnmp.SnmpPDU {
	t.Helper()
	pdus, err := parseSnmprec(plugintest.Fixture(t, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return pdus
}

func parseSnmprec(b []byte) ([]gosnmp.SnmpPDU, error) {
	var out []gosnmp.SnmpPDU
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		oid, rest, ok := strings.Cut(text, "|")
		if !ok {
			return nil, fmt.Errorf("line %d: no type", line)
		}
		typ, value, ok := strings.Cut(rest, "|")
		if !ok {
			return nil, fmt.Errorf("line %d: no value", line)
		}
		isHex := strings.HasSuffix(typ, "x")
		code, err := strconv.Atoi(strings.TrimSuffix(typ, "x"))
		if err != nil {
			return nil, fmt.Errorf("line %d: type %q", line, typ)
		}
		raw := []byte(value)
		if isHex {
			if raw, err = hex.DecodeString(strings.ReplaceAll(value, " ", "")); err != nil {
				return nil, fmt.Errorf("line %d: hex: %v", line, err)
			}
		}
		pdu := gosnmp.SnmpPDU{Name: "." + strings.TrimPrefix(oid, "."), Type: gosnmp.Asn1BER(code)}
		num := func() (uint64, error) { return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64) }
		switch pdu.Type {
		case gosnmp.Integer:
			n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				return nil, fmt.Errorf("line %d: integer %q", line, raw)
			}
			pdu.Value = n
		case gosnmp.OctetString, gosnmp.Opaque, gosnmp.BitString:
			pdu.Value = raw
		case gosnmp.ObjectIdentifier:
			pdu.Value = "." + strings.TrimPrefix(string(raw), ".")
		case gosnmp.IPAddress:
			if isHex && len(raw) == 4 {
				pdu.Value = net.IP(raw).String()
			} else {
				pdu.Value = string(raw)
			}
		case gosnmp.Counter32, gosnmp.Gauge32:
			n, err := num()
			if err != nil {
				return nil, fmt.Errorf("line %d: unsigned %q", line, raw)
			}
			pdu.Value = uint(n)
		case gosnmp.TimeTicks, gosnmp.Uinteger32:
			n, err := num()
			if err != nil {
				return nil, fmt.Errorf("line %d: timeticks %q", line, raw)
			}
			pdu.Value = uint32(n)
		case gosnmp.Counter64:
			n, err := num()
			if err != nil {
				return nil, fmt.Errorf("line %d: counter64 %q", line, raw)
			}
			pdu.Value = n
		case gosnmp.Null:
		default:
			return nil, fmt.Errorf("line %d: unsupported type %d", line, code)
		}
		out = append(out, pdu)
	}
	return out, sc.Err()
}

// formatSnmprec writes PDUs in the snmprec format (inverse of parseSnmprec).
func formatSnmprec(pdus []gosnmp.SnmpPDU) string {
	var b strings.Builder
	for _, p := range pdus {
		oid := strings.TrimPrefix(p.Name, ".")
		switch v := p.Value.(type) {
		case []byte:
			if utf8.Valid(v) && !bytes.ContainsAny(v, "|\n\r") && isPrintable(v) {
				fmt.Fprintf(&b, "%s|%d|%s\n", oid, p.Type, v)
			} else {
				fmt.Fprintf(&b, "%s|%dx|%s\n", oid, p.Type, hex.EncodeToString(v))
			}
		case string:
			fmt.Fprintf(&b, "%s|%d|%s\n", oid, p.Type, strings.TrimPrefix(v, "."))
		case nil:
			fmt.Fprintf(&b, "%s|%d|\n", oid, p.Type)
		default:
			fmt.Fprintf(&b, "%s|%d|%v\n", oid, p.Type, v)
		}
	}
	return b.String()
}

func TestSnmprecRoundTrip(t *testing.T) {
	for _, name := range []string{"netsnmp-alpine.snmprec", "librenms-procurve.snmprec"} {
		pdus := loadSnmprec(t, name)
		again, err := parseSnmprec([]byte(formatSnmprec(pdus)))
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(pdus) {
			t.Fatalf("%s: %d != %d", name, len(again), len(pdus))
		}
		for i := range pdus {
			if pdus[i].Name != again[i].Name || pdus[i].Type != again[i].Type || fmt.Sprint(pdus[i].Value) != fmt.Sprint(again[i].Value) {
				t.Fatalf("%s: %+v != %+v", name, pdus[i], again[i])
			}
		}
	}
}

// captureRoots are the subtrees stored in the net-snmp capture.
var captureRoots = []string{".1.3.6.1.2.1.1", ".1.3.6.1.2.1.2.2", ".1.3.6.1.2.1.31.1.1", ".1.3.6.1.2.1.4.22", ".1.3.6.1.2.1.4.35",
	".1.3.6.1.2.1.17", ".1.0.8802.1.1.2"}

// TestCaptureSnmprec records a live agent into a snmprec file:
//
//	NETSCOPE_SNMP_CAPTURE=192.168.8.123:1161 NETSCOPE_SNMP_COMMUNITY=… \
//	NETSCOPE_SNMP_CAPTURE_OUT=testdata/netsnmp-alpine.snmprec go test -run TestCaptureSnmprec
func TestCaptureSnmprec(t *testing.T) {
	target := os.Getenv("NETSCOPE_SNMP_CAPTURE")
	out := os.Getenv("NETSCOPE_SNMP_CAPTURE_OUT")
	if target == "" || out == "" {
		t.Skip("NETSCOPE_SNMP_CAPTURE / NETSCOPE_SNMP_CAPTURE_OUT not set")
	}
	host, portS, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portS)
	g := &gosnmp.GoSNMP{Target: host, Port: uint16(port), Version: gosnmp.Version2c, Community: os.Getenv("NETSCOPE_SNMP_COMMUNITY"),
		Timeout: gosnmpTimeout, Retries: 1, MaxRepetitions: 25}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	var all []gosnmp.SnmpPDU
	for _, root := range captureRoots {
		pdus, err := g.BulkWalkAll(root)
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		all = append(all, pdus...)
	}
	if err := os.WriteFile(out, []byte(formatSnmprec(all)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d PDUs written to %s", len(all), out)
}

const gosnmpTimeout = 3 * time.Second
