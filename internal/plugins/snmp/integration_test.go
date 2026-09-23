package snmp

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationAgent queries a real agent with v2c and (optionally) v3:
//
//	NETSCOPE_INTEGRATION=1 NETSCOPE_SNMP_TARGET=192.168.8.123:1161 NETSCOPE_SNMP_COMMUNITY=… \
//	NETSCOPE_SNMP_V3_USER=… NETSCOPE_SNMP_V3_AUTH=… NETSCOPE_SNMP_V3_PRIV=… \
//	go test ./internal/plugins/snmp -run Integration -v
//
// A throwaway net-snmp agent: docker run --rm --network host alpine:3.22 sh -c
// "apk add net-snmp && snmpd -f -Lo -C -c <conf with agentAddress udp:1161,
// rocommunity, createUser <user> SHA <auth> AES <priv>, rouser <user> priv>".
func TestIntegrationAgent(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" || os.Getenv("NETSCOPE_SNMP_TARGET") == "" {
		t.Skip("NETSCOPE_INTEGRATION=1 and NETSCOPE_SNMP_TARGET not set")
	}
	host, portS, err := net.SplitHostPort(os.Getenv("NETSCOPE_SNMP_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portS)
	creds := plugintest.Creds{1: v2c(1, "v2c", os.Getenv("NETSCOPE_SNMP_COMMUNITY"))}
	if user := os.Getenv("NETSCOPE_SNMP_V3_USER"); user != "" {
		creds[2] = &plugin.Credential{ID: 2, Name: "v3", Type: plugin.CredSNMPv3,
			Public: map[string]string{"username": user, "security_level": "authPriv", "auth_protocol": "SHA", "priv_protocol": "AES"},
			Secret: map[string]string{"auth_password": os.Getenv("NETSCOPE_SNMP_V3_AUTH"), "priv_password": os.Getenv("NETSCOPE_SNMP_V3_PRIV")}}
	}
	for id := range creds {
		t.Run(creds[id].Name, func(t *testing.T) {
			p := &Plugin{}
			rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{id}, "port": port})
			rc.Creds = creds
			rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{ID: 1, CIDR: netip.MustParsePrefix("192.168.8.0/24")}}}
			rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 1, Name: "agent", PrimaryIP: host}}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			start := time.Now()
			if err := p.Run(ctx, rc); err != nil {
				t.Fatal(err)
			}
			obs := sink.All()
			if len(obs) == 0 {
				t.Fatal("no observation")
			}
			inv := obs[0].Inventory.(Inventory)
			t.Logf("took %v: host=%q version=%s sys=%q interfaces=%d arp=%d fdb=%d lldp=%d walked=%v errors=%v neighbours=%d",
				time.Since(start), obs[0].Hostname, inv.System.Version, inv.System.Descr, len(inv.Interfaces), len(inv.ARP), len(inv.FDB),
				len(inv.LLDP), inv.Walked, inv.Errors, len(obs)-1)
			if obs[0].Hostname == "" || inv.System.ObjectID == "" || len(inv.Interfaces) == 0 || len(inv.Errors) > 0 {
				t.Errorf("observation = %+v", obs[0])
			}
			if (id == 2) != (inv.System.Version == "3") {
				t.Errorf("version = %s", inv.System.Version)
			}
		})
	}
}
