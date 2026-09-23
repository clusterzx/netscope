package netutil

import (
	"os"
	"testing"
)

func TestParseRouteTable(t *testing.T) {
	f, err := os.Open("testdata/proc-net-route.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gws := ParseRouteTable(f)
	if len(gws) != 1 || gws["eth0"].String() != "192.168.8.1" {
		t.Fatalf("gateways: %v", gws)
	}
}
