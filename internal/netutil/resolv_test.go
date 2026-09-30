package netutil

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseResolvConf(t *testing.T) {
	in := "# generated\nsearch lan\nnameserver 192.168.1.1\nnameserver  fd00::1\nnameserver bogus\noptions edns0\n"
	got := ParseResolvConf(strings.NewReader(in))
	if want := []string{"192.168.1.1", "fd00::1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
