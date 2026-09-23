package cve

import (
	"path"
	"strings"
)

// pkgRule maps distribution package names (exact names or path.Match globs) to the
// NVD product. Aliases of the product (see aliasGroups) are matched as well.
type pkgRule struct {
	names   []string
	part    string
	vendor  string
	product string
	kernel  bool // Linux kernel images: only the newest installed kernel counts
}

// packageRules is the curated package → CPE table. Only packages whose upstream version
// is meaningful for the NVD product are listed; library packages carry the version of
// the upstream project they are built from.
var packageRules = []pkgRule{
	{names: []string{"openssh-server", "openssh-client", "openssh", "openssh-clients", "openssh-sftp-server", "openssh-server-pam"}, part: "a", vendor: "openbsd", product: "openssh"},
	{names: []string{"openssl", "libssl3", "libssl3t64", "libssl1.1", "libssl1.0.0", "libssl1.0.2", "openssl-libs", "libssl[0-9]*", "libcrypto3", "libcrypto1.1", "openssl1.1"}, part: "a", vendor: "openssl", product: "openssl"},
	{names: []string{"nginx", "nginx-core", "nginx-full", "nginx-light", "nginx-extras", "nginx-common", "nginx-mainline"}, part: "a", vendor: "f5", product: "nginx"},
	{names: []string{"apache2", "apache2-bin", "httpd", "httpd-core", "apache2-utils"}, part: "a", vendor: "apache", product: "http_server"},
	{names: []string{"bash"}, part: "a", vendor: "gnu", product: "bash"},
	{names: []string{"sudo", "sudo-ldap"}, part: "a", vendor: "sudo_project", product: "sudo"},
	{names: []string{"curl", "libcurl4", "libcurl4t64", "libcurl3", "libcurl3-gnutls", "libcurl3t64-gnutls", "libcurl4-openssl-dev", "libcurl", "libcurl-minimal", "curl-minimal"}, part: "a", vendor: "haxx", product: "curl"},
	{names: []string{"libc6", "libc-bin", "glibc", "glibc-common", "libc6-dev"}, part: "a", vendor: "gnu", product: "glibc"},
	{names: []string{"musl"}, part: "a", vendor: "musl-libc", product: "musl"},
	{names: []string{"samba", "samba-common-bin", "samba-libs", "libsmbclient", "samba-client", "samba-common"}, part: "a", vendor: "samba", product: "samba"},
	{names: []string{"bind9", "bind", "bind9-libs", "bind9-host", "bind9-dnsutils", "bind-libs", "bind-utils"}, part: "a", vendor: "isc", product: "bind"},
	{names: []string{"dnsmasq", "dnsmasq-base", "dnsmasq-full"}, part: "a", vendor: "thekelleys", product: "dnsmasq"},
	{names: []string{"systemd", "libsystemd0", "systemd-libs", "udev"}, part: "a", vendor: "systemd_project", product: "systemd"},
	{names: []string{"linux-image-*", "linux-modules-*", "kernel", "kernel-core", "kernel-lts", "linux-lts", "linux-virt", "linux-edge", "linux-rpi", "pve-kernel-*", "proxmox-kernel-*", "raspberrypi-kernel"}, part: "o", vendor: "linux", product: "linux_kernel", kernel: true},
	{names: []string{"docker-ce", "docker.io", "moby-engine", "docker-engine"}, part: "a", vendor: "mobyproject", product: "moby"},
	{names: []string{"containerd", "containerd.io"}, part: "a", vendor: "linuxfoundation", product: "containerd"},
	{names: []string{"runc"}, part: "a", vendor: "linuxfoundation", product: "runc"},
	{names: []string{"openvpn"}, part: "a", vendor: "openvpn", product: "openvpn"},
	{names: []string{"postgresql", "postgresql-[0-9]", "postgresql-[0-9][0-9]", "postgresql-[0-9].[0-9]", "postgresql-server", "postgresql[0-9][0-9]-server", "libpq5"}, part: "a", vendor: "postgresql", product: "postgresql"},
	{names: []string{"mariadb-server", "mariadb-server-core", "mariadb-server-[0-9]*", "mariadb-client", "mariadb"}, part: "a", vendor: "mariadb", product: "mariadb"},
	{names: []string{"mysql-server", "mysql-server-[0-9]*", "mysql-server-core-[0-9]*", "mysql-community-server"}, part: "a", vendor: "oracle", product: "mysql"},
	{names: []string{"redis-server", "redis", "redis-tools"}, part: "a", vendor: "redis", product: "redis"},
	{names: []string{"git", "git-core"}, part: "a", vendor: "git-scm", product: "git"},
	{names: []string{"vim", "vim-tiny", "vim-common", "vim-runtime", "vim-minimal", "vim-enhanced", "vim-nox", "vim-gtk3"}, part: "a", vendor: "vim", product: "vim"},
	{names: []string{"xz-utils", "liblzma5", "xz", "xz-libs"}, part: "a", vendor: "tukaani", product: "xz"},
	{names: []string{"zlib1g", "zlib", "zlib-ng-compat"}, part: "a", vendor: "zlib", product: "zlib"},
	{names: []string{"expat", "libexpat1", "libexpat"}, part: "a", vendor: "libexpat_project", product: "libexpat"},
	{names: []string{"sqlite3", "libsqlite3-0", "sqlite", "sqlite-libs"}, part: "a", vendor: "sqlite", product: "sqlite"},
	{names: []string{"python3.[0-9]*", "libpython3.[0-9]*", "python3", "python3-libs", "python"}, part: "a", vendor: "python", product: "python"},
	{names: []string{"perl", "perl-base", "perl-libs", "perl-interpreter"}, part: "a", vendor: "perl", product: "perl"},
	{names: []string{"polkit", "policykit-1", "polkitd", "libpolkit-gobject-1-0", "polkit-libs"}, part: "a", vendor: "polkit_project", product: "polkit"},
	{names: []string{"nodejs"}, part: "a", vendor: "nodejs", product: "node.js"},
	{names: []string{"php[0-9].[0-9]*", "php", "php-cli", "php-fpm"}, part: "a", vendor: "php", product: "php"},
	{names: []string{"haproxy"}, part: "a", vendor: "haproxy", product: "haproxy"},
	{names: []string{"lighttpd"}, part: "a", vendor: "lighttpd", product: "lighttpd"},
	{names: []string{"squid", "squid3"}, part: "a", vendor: "squid-cache", product: "squid"},
	{names: []string{"postfix"}, part: "a", vendor: "postfix", product: "postfix"},
	{names: []string{"exim4", "exim4-daemon-light", "exim4-daemon-heavy", "exim"}, part: "a", vendor: "exim", product: "exim"},
	{names: []string{"dovecot-core", "dovecot"}, part: "a", vendor: "dovecot", product: "dovecot"},
	{names: []string{"slapd", "openldap", "libldap-2.5-0", "libldap2", "openldap-servers"}, part: "a", vendor: "openldap", product: "openldap"},
	{names: []string{"libxml2"}, part: "a", vendor: "xmlsoft", product: "libxml2"},
	{names: []string{"libgnutls30", "libgnutls30t64", "gnutls"}, part: "a", vendor: "gnu", product: "gnutls"},
	{names: []string{"libkrb5-3", "krb5-libs", "krb5-user", "krb5"}, part: "a", vendor: "mit", product: "kerberos_5"},
	{names: []string{"cups", "cups-daemon", "libcups2", "libcups2t64"}, part: "a", vendor: "openprinting", product: "cups"},
	{names: []string{"cups-browsed"}, part: "a", vendor: "openprinting", product: "cups-browsed"},
	{names: []string{"avahi-daemon", "avahi"}, part: "a", vendor: "avahi", product: "avahi"},
	{names: []string{"bluez"}, part: "a", vendor: "bluez", product: "bluez"},
	{names: []string{"ntp"}, part: "a", vendor: "ntp", product: "ntp"},
	{names: []string{"chrony"}, part: "a", vendor: "tuxfamily", product: "chrony"},
	{names: []string{"rsync"}, part: "a", vendor: "samba", product: "rsync"},
	{names: []string{"tar"}, part: "a", vendor: "gnu", product: "tar"},
	{names: []string{"wget"}, part: "a", vendor: "gnu", product: "wget"},
	{names: []string{"less"}, part: "a", vendor: "gnu", product: "less"},
	{names: []string{"grub-common", "grub2-common", "grub-pc", "grub-efi-amd64-bin", "grub2-tools"}, part: "a", vendor: "gnu", product: "grub2"},
	{names: []string{"libarchive13", "libarchive13t64", "libarchive"}, part: "a", vendor: "libarchive", product: "libarchive"},
	{names: []string{"unbound", "libunbound8"}, part: "a", vendor: "nlnetlabs", product: "unbound"},
	{names: []string{"libpam0g", "pam", "libpam-modules"}, part: "a", vendor: "linux-pam", product: "linux-pam"},
	{names: []string{"util-linux"}, part: "a", vendor: "kernel", product: "util-linux"},
	{names: []string{"e2fsprogs"}, part: "a", vendor: "e2fsprogs_project", product: "e2fsprogs"},
	{names: []string{"busybox", "busybox-static"}, part: "a", vendor: "busybox", product: "busybox"},
	{names: []string{"tmux"}, part: "a", vendor: "tmux_project", product: "tmux"},
	{names: []string{"screen"}, part: "a", vendor: "gnu", product: "screen"},
	{names: []string{"proftpd-basic", "proftpd", "proftpd-core"}, part: "a", vendor: "proftpd", product: "proftpd"},
	{names: []string{"vsftpd"}, part: "a", vendor: "beasts", product: "vsftpd"},
	{names: []string{"mosquitto"}, part: "a", vendor: "eclipse", product: "mosquitto"}, //nolint:misspell // Eclipse Mosquitto is the product name
	{names: []string{"libvirt-daemon", "libvirt0", "libvirt"}, part: "a", vendor: "redhat", product: "libvirt"},
	{names: []string{"qemu-system-x86", "qemu-kvm", "qemu-system-common", "pve-qemu-kvm", "qemu"}, part: "a", vendor: "qemu", product: "qemu"},
	{names: []string{"grafana", "grafana-enterprise"}, part: "a", vendor: "grafana", product: "grafana"},
	{names: []string{"tomcat9", "tomcat10", "tomcat"}, part: "a", vendor: "apache", product: "tomcat"},
	{names: []string{"zabbix-server-mysql", "zabbix-server-pgsql", "zabbix-agent", "zabbix-agent2"}, part: "a", vendor: "zabbix", product: "zabbix"},
	{names: []string{"frr"}, part: "a", vendor: "frrouting", product: "frrouting"},
	{names: []string{"ffmpeg", "libavcodec[0-9]*"}, part: "a", vendor: "ffmpeg", product: "ffmpeg"},
	{names: []string{"imagemagick", "imagemagick-6.q16", "ImageMagick", "libmagickcore-6.q16-6"}, part: "a", vendor: "imagemagick", product: "imagemagick"},
	{names: []string{"ghostscript", "libgs10", "libgs9"}, part: "a", vendor: "artifex", product: "ghostscript"},
	{names: []string{"libwebp7", "libwebp", "libwebp6"}, part: "a", vendor: "webmproject", product: "libwebp"},
	{names: []string{"libpng16-16", "libpng16-16t64", "libpng", "libpng16"}, part: "a", vendor: "libpng", product: "libpng"},
	{names: []string{"libtiff6", "libtiff5", "libtiff"}, part: "a", vendor: "libtiff", product: "libtiff"},
	{names: []string{"libssh-4", "libssh", "libssh-gcrypt-4"}, part: "a", vendor: "libssh", product: "libssh"},
	{names: []string{"libssh2-1", "libssh2-1t64", "libssh2"}, part: "a", vendor: "libssh2", product: "libssh2"},
	{names: []string{"libgcrypt20", "libgcrypt"}, part: "a", vendor: "gnupg", product: "libgcrypt"},
	{names: []string{"gnupg", "gpg", "gnupg2"}, part: "a", vendor: "gnupg", product: "gnupg"},
	{names: []string{"libnss3", "nss"}, part: "a", vendor: "mozilla", product: "nss"},
	{names: []string{"tcpdump"}, part: "a", vendor: "tcpdump", product: "tcpdump"},
	{names: []string{"libpcap0.8", "libpcap0.8t64", "libpcap"}, part: "a", vendor: "tcpdump", product: "libpcap"},
	{names: []string{"wpasupplicant", "wpa_supplicant"}, part: "a", vendor: "w1.fi", product: "wpa_supplicant"},
	{names: []string{"hostapd"}, part: "a", vendor: "w1.fi", product: "hostapd"},
	{names: []string{"snapd"}, part: "a", vendor: "canonical", product: "snapd"},
	{names: []string{"dropbear", "dropbear-bin"}, part: "a", vendor: "dropbear_ssh_project", product: "dropbear_ssh"},
	{names: []string{"libjpeg-turbo8", "libjpeg62-turbo", "libjpeg-turbo"}, part: "a", vendor: "libjpeg-turbo", product: "libjpeg-turbo"},
	{names: []string{"libcurl-gnutls"}, part: "a", vendor: "haxx", product: "curl"},
}

var (
	pkgExact = map[string]int{}
	pkgGlobs []struct {
		pattern string
		rule    int
	}
)

func init() {
	for i, r := range packageRules {
		for _, n := range r.names {
			if strings.ContainsAny(n, "*?[") {
				pkgGlobs = append(pkgGlobs, struct {
					pattern string
					rule    int
				}{n, i})
				continue
			}
			if _, dup := pkgExact[n]; !dup {
				pkgExact[n] = i
			}
		}
	}
}

// lookupPackage returns the mapping rule of a package name.
func lookupPackage(name string) (pkgRule, bool) {
	if i, ok := pkgExact[name]; ok {
		return packageRules[i], true
	}
	for _, g := range pkgGlobs {
		if ok, _ := path.Match(g.pattern, name); ok {
			return packageRules[g.rule], true
		}
	}
	return pkgRule{}, false
}

// packageFilter returns an SQL condition (on column "name") selecting all packages the
// table knows, plus its arguments. GLOB uses the same syntax as path.Match here and can
// use the index on packages(name) for prefix patterns.
func packageFilter() (string, []any) {
	names := make([]string, 0, len(pkgExact))
	for n := range pkgExact {
		names = append(names, n)
	}
	var (
		conds []string
		args  []any
	)
	if len(names) > 0 {
		conds = append(conds, "name IN ("+strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")+")")
		for _, n := range names {
			args = append(args, n)
		}
	}
	for _, g := range pkgGlobs {
		conds = append(conds, "name GLOB ?")
		args = append(args, g.pattern)
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// kernelVersionUsable reports whether a kernel package version reveals the upstream
// patch level. Ubuntu and RHEL version their kernels by ABI ("6.8.0-45.45",
// "5.14.0-427.el9"): the ".0" patch level says nothing about the fixes included, so
// such kernels are not matched. Debian ("6.1.106-3") and Proxmox ("6.8.12-4") kernels
// carry the real upstream version.
func kernelVersionUsable(upstream, full string) bool {
	toks := tokenize(upstream)
	nums := 0
	for _, t := range toks {
		if !t.num {
			break
		}
		nums++
	}
	if nums < 3 {
		return false
	}
	if toks[2].s == "0" && len(full) > len(upstream) {
		return false
	}
	return true
}
