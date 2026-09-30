package pfsense

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
	"netscope/internal/sshx"
	"netscope/internal/sshx/sshtest"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const configXML = `<?xml version="1.0"?>
<pfsense>
	<version>23.9</version>
	<system><hostname>fw</hostname><user><name>admin</name><bcrypt-hash>$2y$10$secret</bcrypt-hash></user></system>
	<interfaces>
		<wan><enable></enable><if>igb0</if><descr><![CDATA[WAN]]></descr></wan>
		<lan><enable></enable><if>igb1</if><descr><![CDATA[LAN]]></descr></lan>
		<opt1><enable></enable><if>igb1.10</if><descr><![CDATA[IOT]]></descr></opt1>
	</interfaces>
	<dhcpd>
		<lan>
			<enable></enable>
			<range><from>192.168.1.100</from><to>192.168.1.199</to></range>
			<staticmap><mac>00:11:32:aa:bb:cc</mac><cid></cid><ipaddr>192.168.1.20</ipaddr><hostname>nas01</hostname><descr><![CDATA[Synology]]></descr></staticmap>
		</lan>
		<opt1>
			<staticmap><mac>44:55:66:77:88:99</mac><ipaddr>192.168.10.5</ipaddr><hostname>cam</hostname><descr></descr></staticmap>
		</opt1>
	</dhcpd>
</pfsense>
`

const iscFile = `lease 192.168.1.100 {
  starts 6 2026/09/27 10:00:00;
  ends 6 2026/09/27 14:00:00;
  binding state active;
  hardware ethernet 3c:22:fb:12:34:56;
  client-hostname "MacBook-Pro";
}
`

const arpOut = `? (192.168.1.100) at 3c:22:fb:12:34:56 on igb1 expires in 1187 seconds [ethernet]
? (192.168.10.5) at 44:55:66:77:88:99 on igb1.10 expires in 845 seconds [vlan]
? (192.168.1.1) at 00:08:a2:0c:11:22 on igb1 permanent [ethernet]
? (192.168.1.77) at (incomplete) on igb1 expired [ethernet]
`

type fakeRemote struct {
	out     string
	kea     string // answer of the Kea socket ("" = no socket)
	queried []string
}

func (f *fakeRemote) Run(ctx context.Context, cmd string, max int) (*sshx.Result, error) {
	if cmd != script {
		return nil, errors.New("unexpected command")
	}
	return &sshx.Result{Stdout: []byte(f.out)}, nil
}

func (f *fakeRemote) Query(ctx context.Context, socket string, cmd []byte) ([]byte, error) {
	f.queried = append(f.queried, socket)
	if f.kea == "" || socket != keaSockets[0] {
		return nil, errors.New("no such socket")
	}
	return []byte(f.kea), nil
}

func (f *fakeRemote) Close() error { return nil }

func output(isc, kea string) string {
	return "@@NS:config@@\n" + configXML + "@@NS:isc@@\n" + isc + "@@NS:kea@@\n" + kea + "@@NS:arp@@\n" + arpOut + "@@NS:end@@\n"
}

func runPf(t *testing.T, r *fakeRemote) (*plugintest.Sink, error) {
	p := &Plugin{now: func() time.Time { return now },
		dial: func(ctx context.Context, host string, creds []*plugin.Credential, opt sshx.Options) (remote, error) {
			return r, nil
		}}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{"fw.lan"}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredSSH, Public: map[string]string{"username": "admin"}}}
	rc.DataDir = t.TempDir()
	return sink, p.Run(context.Background(), rc)
}

func byIP(sink *plugintest.Sink) map[string]plugin.Observation {
	out := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		out[o.IP] = o
	}
	return out
}

func TestPfSenseISC(t *testing.T) {
	r := &fakeRemote{out: output(iscFile, "")}
	sink, err := runPf(t, r)
	if err != nil {
		t.Fatal(err)
	}
	got := byIP(sink)
	if len(got) != 3 {
		t.Fatalf("observations %v", got)
	}
	nas := got["192.168.1.20"].Inventory.(netsrc.Inventory)
	if got["192.168.1.20"].Hostname != "Synology" || !nas.Static || nas.Interface != "LAN" {
		t.Errorf("static %+v", nas)
	}
	mb := got["192.168.1.100"]
	if mb.Hostname != "MacBook-Pro" || mb.Inventory.(netsrc.Inventory).Interface != "LAN" || mb.Inventory.(netsrc.Inventory).Expires == nil {
		t.Errorf("isc lease + arp %+v", mb)
	}
	if cam := got["192.168.10.5"]; cam.Hostname != "cam" || cam.Inventory.(netsrc.Inventory).Interface != "IOT" {
		t.Errorf("iot %+v", cam)
	}
	if len(r.queried) != len(keaSockets) {
		t.Errorf("kea sockets tried %v", r.queried)
	}
}

func TestPfSenseKeaSocket(t *testing.T) {
	// 1790520000 = 2026-09-28; the socket answer wins over the (older) lease file
	kea := `{"arguments":{"leases":[{"ip-address":"192.168.1.101","hw-address":"aa:bb:cc:00:00:01","hostname":"drucker.","valid-lft":7200,"cltt":1790513000,"state":0},
		{"ip-address":"192.168.1.102","hw-address":"aa:bb:cc:00:00:02","hostname":"alt","valid-lft":7200,"cltt":1790000000,"state":0}]},"result":0,"text":"2 IPv4 lease(s) found."}`
	r := &fakeRemote{out: output("", "address,hwaddr,client_id,valid_lifetime,expire,subnet_id,fqdn_fwd,fqdn_rev,hostname,state,user_context,pool_id\n192.168.1.103,aa:bb:cc:00:00:03,,7200,1790520000,1,0,0,file,0,,0\n"), kea: kea}
	p := &Plugin{now: func() time.Time { return time.Unix(1790514000, 0) },
		dial: func(ctx context.Context, host string, creds []*plugin.Credential, opt sshx.Options) (remote, error) {
			return r, nil
		}}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{"fw.lan"}, "include_arp": false})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredSSH, Public: map[string]string{"username": "admin"}}}
	rc.DataDir = t.TempDir()
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	got := byIP(sink)
	if _, ok := got["192.168.1.101"]; !ok || got["192.168.1.101"].Hostname != "drucker" {
		t.Errorf("kea socket lease %v", got)
	}
	if _, ok := got["192.168.1.102"]; ok {
		t.Error("expired kea lease")
	}
	if _, ok := got["192.168.1.103"]; ok {
		t.Error("lease file used although the socket answered")
	}
}

func TestPfSenseConsoleMenu(t *testing.T) {
	r := &fakeRemote{out: "\n*** Welcome to pfSense 2.7.2-RELEASE ***\n 0) Logout (SSH only)\n 8) Shell\n\nEnter an option:"}
	if _, err := runPf(t, r); err == nil || !strings.Contains(err.Error(), "Konsolenmenü") {
		t.Errorf("menu: %v", err)
	}
}

// The connection test signs in over SSH and reads like a run, but stores nothing; it
// reports a wrong password and a wrong address.
func TestPfSenseConnectionTest(t *testing.T) {
	srv := sshtest.New(t, "admin", "pw", func(cmd string) (string, int) {
		if cmd != script {
			return "", 127
		}
		return output(iscFile, ""), 0
	})
	creds := func(pw string) plugintest.Creds {
		return plugintest.Creds{1: {ID: 1, Name: "fw", Type: plugin.CredPassword, Public: map[string]string{"username": "admin"},
			Secret: map[string]string{"password": pw}}}
	}
	p := &Plugin{now: func() time.Time { return now }}
	// default host key policy (tofu): the test checks keys but must not write known_hosts
	settings := map[string]any{"hosts": []any{"127.0.0.1"}, "port": srv.Port()}
	res, err := plugintest.ConnectionTest(t, p, settings, creds("pw"), "")
	if err != nil || len(res) != 1 || !res[0].OK || res[0].Target != "127.0.0.1" || !strings.Contains(res[0].Message, "3 Clients") {
		t.Fatalf("valid password: %+v, %v", res, err)
	}
	if cmds := srv.Commands(); len(cmds) != 1 || cmds[0] != script {
		t.Errorf("commands %q", cmds)
	}
	res, err = plugintest.ConnectionTest(t, p, settings, creds("wrong"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt") {
		t.Fatalf("wrong password: %+v, %v", res, err)
	}
	// the SSH port applies to every firewall: a closed port is a wrong address
	u, _ := url.Parse(plugintest.ClosedURL(t, "ssh"))
	closed, _ := strconv.Atoi(u.Port())
	settings["port"] = closed
	res, err = plugintest.ConnectionTest(t, p, settings, creds("pw"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "SSH-Verbindung zu 127.0.0.1 fehlgeschlagen") {
		t.Fatalf("wrong address: %+v, %v", res, err)
	}
}
