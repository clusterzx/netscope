package snmp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/plugin"
)

// clientConfig holds the protocol settings of a run.
type clientConfig struct {
	port           int
	timeout        time.Duration
	retries        int
	maxRepetitions int
}

var authProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	"MD5": gosnmp.MD5, "SHA": gosnmp.SHA, "SHA224": gosnmp.SHA224, "SHA256": gosnmp.SHA256, "SHA384": gosnmp.SHA384, "SHA512": gosnmp.SHA512,
}

var privProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	"DES": gosnmp.DES, "AES": gosnmp.AES, "AES192": gosnmp.AES192, "AES256": gosnmp.AES256, "AES192C": gosnmp.AES192C, "AES256C": gosnmp.AES256C,
}

// newClient builds (but does not connect) a gosnmp client for a vault credential.
func newClient(ctx context.Context, ip string, cred *plugin.Credential, cc clientConfig) (*gosnmp.GoSNMP, error) {
	g := &gosnmp.GoSNMP{
		Target:         ip,
		Port:           uint16(cc.port),
		Transport:      "udp",
		Timeout:        cc.timeout,
		Retries:        cc.retries,
		MaxRepetitions: uint32(cc.maxRepetitions),
		MaxOids:        gosnmp.MaxOids,
		Context:        ctx,
	}
	switch cred.Type {
	case plugin.CredSNMPv2c:
		community := cred.Get("community")
		if community == "" {
			return nil, fmt.Errorf("Credential %q: Community fehlt", cred.Name)
		}
		g.Version, g.Community = gosnmp.Version2c, community
	case plugin.CredSNMPv3:
		user := cred.Get("username")
		if user == "" {
			return nil, fmt.Errorf("Credential %q: Benutzername fehlt", cred.Name)
		}
		usm := &gosnmp.UsmSecurityParameters{UserName: user, AuthenticationProtocol: gosnmp.NoAuth, PrivacyProtocol: gosnmp.NoPriv}
		level := cred.Get("security_level")
		if level == "" {
			level = "authPriv"
		}
		switch level {
		case "noAuthNoPriv":
			g.MsgFlags = gosnmp.NoAuthNoPriv
		case "authNoPriv", "authPriv":
			ap, ok := authProtocols[strings.ToUpper(defaultString(cred.Get("auth_protocol"), "SHA"))]
			if !ok {
				return nil, fmt.Errorf("Credential %q: unbekanntes Auth-Protokoll %q", cred.Name, cred.Get("auth_protocol"))
			}
			if cred.Get("auth_password") == "" {
				return nil, fmt.Errorf("Credential %q: Auth-Passwort fehlt", cred.Name)
			}
			usm.AuthenticationProtocol, usm.AuthenticationPassphrase = ap, cred.Get("auth_password")
			g.MsgFlags = gosnmp.AuthNoPriv
			if level == "authPriv" {
				pp, ok := privProtocols[strings.ToUpper(defaultString(cred.Get("priv_protocol"), "AES"))]
				if !ok {
					return nil, fmt.Errorf("Credential %q: unbekanntes Privacy-Protokoll %q", cred.Name, cred.Get("priv_protocol"))
				}
				if cred.Get("priv_password") == "" {
					return nil, fmt.Errorf("Credential %q: Privacy-Passwort fehlt", cred.Name)
				}
				usm.PrivacyProtocol, usm.PrivacyPassphrase = pp, cred.Get("priv_password")
				g.MsgFlags = gosnmp.AuthPriv
			}
		default:
			return nil, fmt.Errorf("Credential %q: unbekannte Sicherheitsstufe %q", cred.Name, level)
		}
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		g.SecurityParameters = usm
		g.ContextName = cred.Get("context_name")
	default:
		return nil, fmt.Errorf("Credential %q hat Typ %s, erwartet snmp_v2c oder snmp_v3", cred.Name, cred.Type)
	}
	return g, nil
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// versionName returns "2c" or "3".
func versionName(g *gosnmp.GoSNMP) string {
	if g.Version == gosnmp.Version3 {
		return "3"
	}
	return "2c"
}

// querySystem connects and fetches the system group. It fails when the agent does not
// answer (wrong community, unknown user, no agent).
func querySystem(g *gosnmp.GoSNMP) ([]gosnmp.SnmpPDU, error) {
	if err := g.Connect(); err != nil {
		return nil, err
	}
	res, err := g.Get(systemOIDs)
	if err != nil {
		g.Close()
		return nil, err
	}
	if res.Error != gosnmp.NoError {
		g.Close()
		return nil, fmt.Errorf("SNMP-Fehler %s", res.Error)
	}
	return res.Variables, nil
}

// walker collects the PDUs of several subtrees and records per-table errors.
type walker struct {
	g      *gosnmp.GoSNMP
	pdus   []gosnmp.SnmpPDU
	walked []string
	errors map[string]string
}

// table walks the given subtrees for one table. It stops at the first failed walk and
// records the error; the table counts as walked only when all walks succeeded.
func (w *walker) table(name string, oids ...string) bool {
	for _, oid := range oids {
		res, err := w.g.BulkWalkAll(oid)
		if err != nil {
			if w.errors == nil {
				w.errors = map[string]string{}
			}
			w.errors[name] = fmt.Sprintf("%s: %v", oid, err)
			return false
		}
		w.pdus = append(w.pdus, res...)
	}
	return true
}

// walk returns the PDUs of one subtree (appending them) and whether it succeeded.
func (w *walker) walk(name, oid string) (int, bool) {
	before := len(w.pdus)
	ok := w.table(name, oid)
	return len(w.pdus) - before, ok
}

func (w *walker) done(name string) { w.walked = append(w.walked, name) }

// collectOptions select the tables to walk.
type collectOptions struct {
	interfaces, arp, fdb, lldp bool
}

// collect walks the enabled tables and decodes the inventory.
func collect(g *gosnmp.GoSNMP, system []gosnmp.SnmpPDU, o collectOptions) Inventory {
	w := &walker{g: g, pdus: append([]gosnmp.SnmpPDU(nil), system...)}
	inv := Inventory{System: decodeSystem(system)}
	inv.System.Version = versionName(g)
	w.done(TableSystem)

	// interface names are needed to label ARP, FDB and LLDP ports even when the full
	// interface table is not requested
	ifOIDs := []string{oidIfName, oidIfDescr}
	if o.interfaces {
		ifOIDs = append(ifOIDs, oidIfAlias, oidIfType, oidIfPhysAddress, oidIfSpeed, oidIfHighSpeed, oidIfAdminStatus, oidIfOperStatus)
	}
	if o.interfaces || o.arp || o.fdb || o.lldp {
		if w.table(TableInterfaces, ifOIDs...) && o.interfaces {
			w.done(TableInterfaces)
		}
	}
	ifs := decodeInterfaces(w.pdus)
	if o.interfaces {
		inv.Interfaces = ifs
	}
	if o.arp {
		n, ok := w.walk(TableARP, oidIPNetToMediaPhysAddress)
		if ok && n > 0 {
			_, ok = w.walk(TableARP, oidIPNetToMediaType)
		} else if ok {
			if n, ok = w.walk(TableARP, oidIPNetToPhysicalPhysAddress); ok && n > 0 {
				_, ok = w.walk(TableARP, oidIPNetToPhysicalType)
			}
		}
		if ok {
			w.done(TableARP)
			inv.ARP = decodeARP(w.pdus, ifs)
		}
	}
	basePortsWalked := false
	if o.fdb {
		n, ok := w.walk(TableFDB, oidDot1qTpFdbPort)
		if ok && n > 0 {
			ok = w.table(TableFDB, oidDot1qTpFdbStatus, oidDot1qVlanFdbID)
		} else if ok {
			if n, ok = w.walk(TableFDB, oidDot1dTpFdbPort); ok && n > 0 {
				_, ok = w.walk(TableFDB, oidDot1dTpFdbStatus)
			}
		}
		if ok && n > 0 {
			_, ok = w.walk(TableFDB, oidDot1dBasePortIfIndex)
			basePortsWalked = ok
		}
		if ok {
			w.done(TableFDB)
			inv.FDB = decodeFDB(w.pdus, ifs)
		}
	}
	if o.lldp {
		ok := w.table(TableLLDP, oidLldpLocalSystemData)
		if ok {
			var n int
			if n, ok = w.walk(TableLLDP, oidLldpRemTable); ok && n > 0 {
				_, ok = w.walk(TableLLDP, oidLldpRemManAddrTable)
				if ok && !basePortsWalked {
					// lldpLocPortNum is usually the bridge port number
					_, ok = w.walk(TableLLDP, oidDot1dBasePortIfIndex)
				}
				if ok && inv.System.Enterprise == 14988 {
					_, ok = w.walk(TableLLDP, oidMtxrNeighborInterfaceID)
				}
			}
		}
		if ok {
			w.done(TableLLDP)
			inv.LLDPLocal = decodeLLDPLocal(w.pdus)
			inv.LLDP = decodeLLDP(w.pdus, ifs)
		}
	}
	inv.Walked, inv.Errors = w.walked, w.errors
	return inv
}
