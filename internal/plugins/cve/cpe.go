package cve

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// CPE holds the attributes of a CPE name that matter for matching. Values are
// lowercase and unescaped; "*" means ANY and "-" means NA (not applicable).
type CPE struct {
	Part    string `json:"part"` // a (application) | o (operating system) | h (hardware)
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Version string `json:"version"`
	Update  string `json:"update"`
}

const (
	cpeAny = "*"
	cpeNA  = "-"
)

// specific reports whether v is a concrete value (neither ANY nor NA nor empty).
func specific(v string) bool { return v != "" && v != cpeAny && v != cpeNA }

// ParseCPE parses a CPE 2.3 formatted string ("cpe:2.3:a:…") or a CPE 2.2 URI
// ("cpe:/a:…", as reported by nmap).
func ParseCPE(s string) (CPE, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(strings.ToLower(s), "cpe:2.3:"):
		return ParseCPE23(s)
	case strings.HasPrefix(strings.ToLower(s), "cpe:/"):
		return ParseCPE22(s)
	}
	return CPE{}, fmt.Errorf("kein CPE: %q", s)
}

// ParseCPE23 parses a CPE 2.3 formatted string. Components are separated by unescaped
// colons; a backslash quotes the following character ("\:" is a literal colon inside a
// component, "\\" a literal backslash).
func ParseCPE23(s string) (CPE, error) {
	if !strings.HasPrefix(strings.ToLower(s), "cpe:2.3:") {
		return CPE{}, fmt.Errorf("kein CPE-2.3-Name: %q", s)
	}
	comps, err := splitFS(s[len("cpe:2.3:"):])
	if err != nil {
		return CPE{}, fmt.Errorf("CPE %q: %w", s, err)
	}
	if len(comps) < 3 {
		return CPE{}, fmt.Errorf("CPE %q: zu wenige Bestandteile", s)
	}
	for len(comps) < 5 {
		comps = append(comps, cpeAny)
	}
	c := CPE{Part: comps[0], Vendor: comps[1], Product: comps[2], Version: comps[3], Update: comps[4]}
	return c.normalized()
}

// splitFS splits the formatted-string components and unescapes them. A component that
// consists of an unquoted "*" or "-" keeps its special meaning.
func splitFS(s string) ([]string, error) {
	var (
		out []string
		cur strings.Builder
		raw strings.Builder // component as written, to detect unquoted * and -
	)
	flush := func() {
		r := raw.String()
		if r == cpeAny || r == cpeNA || r == "" {
			if r == "" {
				r = cpeAny
			}
			out = append(out, r)
		} else {
			out = append(out, cur.String())
		}
		cur.Reset()
		raw.Reset()
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\\':
			if i+1 >= len(s) {
				return nil, errors.New("Escape-Zeichen am Ende")
			}
			i++
			cur.WriteByte(s[i])
			raw.WriteByte('\\')
			raw.WriteByte(s[i])
		case ':':
			flush()
		default:
			cur.WriteByte(ch)
			raw.WriteByte(ch)
		}
	}
	flush()
	return out, nil
}

// ParseCPE22 parses a CPE 2.2 URI such as "cpe:/a:openbsd:openssh:9.6p1".
// Percent-encoded characters are decoded; empty components mean ANY.
func ParseCPE22(s string) (CPE, error) {
	if !strings.HasPrefix(strings.ToLower(s), "cpe:/") {
		return CPE{}, fmt.Errorf("kein CPE-2.2-URI: %q", s)
	}
	parts := strings.Split(s[len("cpe:/"):], ":")
	if len(parts) < 3 {
		return CPE{}, fmt.Errorf("CPE %q: zu wenige Bestandteile", s)
	}
	vals := make([]string, 5)
	for i := range vals {
		v := cpeAny
		if i < len(parts) && parts[i] != "" {
			dec, err := url.PathUnescape(parts[i])
			if err != nil {
				return CPE{}, fmt.Errorf("CPE %q: %w", s, err)
			}
			v = dec
		}
		vals[i] = v
	}
	// packed edition ("~edition~sw_edition~…") in the edition component is irrelevant here
	c := CPE{Part: vals[0], Vendor: vals[1], Product: vals[2], Version: vals[3], Update: vals[4]}
	return c.normalized()
}

func (c CPE) normalized() (CPE, error) {
	low := func(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
	c.Part, c.Vendor, c.Product, c.Version, c.Update = low(c.Part), low(c.Vendor), low(c.Product), low(c.Version), low(c.Update)
	switch c.Part {
	case "a", "o", "h":
	default:
		return CPE{}, fmt.Errorf("ungültiger CPE-Typ %q", c.Part)
	}
	if !specific(c.Vendor) || !specific(c.Product) {
		return CPE{}, errors.New("CPE ohne Hersteller oder Produkt")
	}
	if c.Version == "" {
		c.Version = cpeAny
	}
	if c.Update == "" {
		c.Update = cpeAny
	}
	return c, nil
}

// String formats the CPE as a CPE 2.3 formatted string (remaining attributes ANY).
func (c CPE) String() string {
	return "cpe:2.3:" + fsValue(c.Part) + ":" + fsValue(c.Vendor) + ":" + fsValue(c.Product) + ":" +
		fsValue(c.Version) + ":" + fsValue(c.Update) + ":*:*:*:*:*:*"
}

// fsValue quotes a value for the formatted string binding.
func fsValue(v string) string {
	if v == "" {
		return cpeAny
	}
	if v == cpeAny || v == cpeNA {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '.' || ch == '-' {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('\\')
		b.WriteByte(ch)
	}
	return b.String()
}

// VendorProduct returns "vendor:product".
func (c CPE) VendorProduct() string { return c.Vendor + ":" + c.Product }

// ---------------------------------------------------------------- product aliases

type vendorProduct struct{ vendor, product string }

// aliasGroups lists names under which the NVD (and nmap) have recorded the same product
// over time (checked against the complete NVD data). The first entry is the canonical
// name used for stored device CPEs; names used only by nmap are listed so that its CPEs
// resolve (igor_sysoev:nginx, matt_johnston:dropbear_ssh_server, vsftpd:vsftpd, ...).
var aliasGroups = [][]vendorProduct{
	{{"f5", "nginx"}, {"f5", "nginx_open_source"}, {"nginx", "nginx"}, {"igor_sysoev", "nginx"}},
	{{"sudo_project", "sudo"}, {"todd_miller", "sudo"}, {"sudo", "sudo"}, {"gratisoft", "sudo"}},
	{{"haxx", "curl"}, {"haxx", "libcurl"}, {"curl", "curl"}, {"curl", "libcurl"}, {"daniel_stenberg", "curl"}, {"libcurl", "libcurl"}},
	{{"oracle", "mysql"}, {"oracle", "mysql_server"}, {"mysql", "mysql"}},
	{{"microsoft", "internet_information_services"}, {"microsoft", "iis"}},
	{{"beasts", "vsftpd"}, {"vsftpd_project", "vsftpd"}, {"vsftpd", "vsftpd"}},
	{{"dropbear_ssh_project", "dropbear_ssh"}, {"matt_johnston", "dropbear_ssh_server"}},
	{{"eclipse", "jetty"}, {"mortbay", "jetty"}, {"mortbay_jetty", "jetty"}, {"jetty", "jetty"}},
	{{"mobyproject", "moby"}, {"docker", "docker"}, {"docker", "engine"}},
	{{"polkit_project", "polkit"}, {"freedesktop", "polkit"}},
	{{"miniupnp_project", "miniupnpd"}, {"miniupnp.free", "miniupnpd"}},
	{{"squid-cache", "squid"}, {"squid", "squid"}},
	{{"mit", "kerberos_5"}, {"mit", "kerberos"}},
	{{"openprinting", "cups"}, {"apple", "cups"}, {"easy_software_products", "cups"}, {"cups", "cups"}},
	{{"tuxfamily", "chrony"}, {"chrony_project", "chrony"}},
	{{"git-scm", "git"}, {"git", "git"}, {"git_project", "git"}},
	{{"artifex", "ghostscript"}, {"ghostscript", "ghostscript"}, {"aladdin_enterprises", "ghostscript"}},
	{{"samba", "rsync"}, {"rsync", "rsync"}, {"andrew_tridgell", "rsync"}},
	{{"kernel", "util-linux"}, {"linux", "util-linux"}, {"andries_brouwer", "util-linux"}},
	{{"e2fsprogs_project", "e2fsprogs"}, {"ext2_filesystems_utilities", "e2fsprogs"}},
	{{"perl", "perl"}, {"larry_wall", "perl"}},
	{{"tcpdump", "tcpdump"}, {"lbl", "tcpdump"}},
	{{"libpng", "libpng"}, {"greg_roelofs", "libpng"}},
	{{"libtiff", "libtiff"}, {"remotesensing", "libtiff"}},
	{{"dovecot", "dovecot"}, {"timo_sirainen", "dovecot"}, {"open-xchange", "dovecot"}},
	{{"proftpd", "proftpd"}, {"proftpd_project", "proftpd"}},
	{{"nlnetlabs", "unbound"}, {"unbound", "unbound"}},
	{{"vim", "vim"}, {"vim_development_group", "vim"}},
	{{"exim", "exim"}, {"university_of_cambridge", "exim"}},
	{{"postfix", "postfix"}, {"wietse_venema", "postfix"}},
	{{"redis", "redis"}, {"redislabs", "redis"}},
	{{"redhat", "libvirt"}, {"libvirt", "libvirt"}},
	{{"bluez", "bluez"}, {"bluez_project", "bluez"}},
	{{"thekelleys", "dnsmasq"}, {"the_kelleys", "dnsmasq"}, {"dnsmasq", "dnsmasq"}},
	{{"tukaani", "xz"}, {"xz_project", "xz"}},
	{{"frrouting", "frrouting"}},
	{{"tmux_project", "tmux"}, {"nicholas_marriott", "tmux"}},
	{{"nodejs", "node.js"}, {"joyent", "node.js"}},
	{{"libjpeg-turbo", "libjpeg-turbo"}, {"d.r.commander", "libjpeg-turbo"}},
	{{"musl-libc", "musl"}, {"etalabs", "musl"}},
	{{"gnu", "less"}, {"greenwoodsoftware", "less"}},
	{{"canonical", "snapd"}, {"snapcraft", "snapd"}},
	{{"w1.fi", "hostapd"}, {"hostapd", "hostapd"}},
	{{"w1.fi", "wpa_supplicant"}, {"wpa_supplicant", "wpa_supplicant"}},
	// names used by the http scanner's signatures
	{{"proxmox", "virtual_environment"}, {"proxmox", "proxmox_ve"}},
	{{"proxmox", "backup_server"}, {"proxmox", "proxmox_backup_server"}},
	{{"nextcloud", "nextcloud_server"}, {"nextcloud", "nextcloud"}},
	{{"traefik", "traefik"}, {"containous", "traefik"}},
	{{"netgate", "pfsense"}, {"pfsense", "pfsense"}, {"bsdperimeter", "pfsense"}, {"bsd_perimeter", "pfsense"}},
	{{"opnsense", "opnsense"}, {"opnsense_project", "opnsense"}},
	{{"dani-garcia", "vaultwarden"}, {"vaultwarden", "vaultwarden"}},
	{{"futo", "immich"}, {"immich", "immich"}},
	{{"cockpit-project", "cockpit"}, {"redhat", "cockpit"}},
	{{"tasmota_project", "tasmota"}, {"tasmota", "tasmota"}},
	{{"ixsystems", "truenas_firmware"}, {"truenas", "truenas"}},
}

var aliasIndex = func() map[vendorProduct][]vendorProduct {
	m := map[vendorProduct][]vendorProduct{}
	for _, g := range aliasGroups {
		for _, vp := range g {
			if _, dup := m[vp]; !dup {
				m[vp] = g
			}
		}
	}
	return m
}()

// aliases returns all names of a product (canonical first), or just the name itself.
func aliases(vendor, product string) []vendorProduct {
	if g, ok := aliasIndex[vendorProduct{vendor, product}]; ok {
		return g
	}
	return []vendorProduct{{vendor, product}}
}

// canonical returns the canonical vendor/product of a product name.
func canonical(vendor, product string) (string, string) {
	g := aliases(vendor, product)
	return g[0].vendor, g[0].product
}
