package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	"netscope/internal/plugin"
)

func TestInfoAndSchema(t *testing.T) {
	p := &Plugin{}
	info := p.Info()
	if info.ID != "tls" || info.Targets != plugin.TargetDevices || info.DefaultConcurrency != 16 {
		t.Fatalf("info = %+v", info)
	}
	if err := p.Schema().Check(); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := p.Schema().Validate(nil, nil, nil); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

// genLeaf builds a self-signed leaf certificate of the given key type.
func genLeaf(t *testing.T, keyType string, notBefore, notAfter time.Time) tls.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test.local"},
		DNSNames:     []string{"test.local", "alt.local"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	return signSelf(t, tmpl, keyType)
}

func signSelf(t *testing.T, tmpl *x509.Certificate, keyType string) tls.Certificate {
	t.Helper()
	switch keyType {
	case "RSA":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	case "ECDSA":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	case "Ed25519":
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}
	t.Fatalf("unknown key type %s", keyType)
	return tls.Certificate{}
}

// serveTLS starts a TLS listener with the given config and returns host, port and a stop
// func. It accepts connections until stopped.
func serveTLS(t *testing.T, conf *tls.Config) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tc := tls.Server(conn, conf)
				_ = tc.Handshake()
				tc.Close()
			}()
		}
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port, func() { close(done); ln.Close() }
}

func cfgFor(certs ...tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: certs}
}

func probeTLS(t *testing.T, host string, port int, cfg config, sni string) *plugin.TLSCert {
	t.Helper()
	p := &Plugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cert, err := p.probe(ctx, endpoint{ip: host, port: port}, cfg, sni)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return cert
}

func TestProbeSelfSignedRSA(t *testing.T) {
	leaf := genLeaf(t, "RSA", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	host, port, stop := serveTLS(t, cfgFor(leaf))
	defer stop()
	cert := probeTLS(t, host, port, config{timeout: 3 * time.Second}, "test.local")
	if cert.SubjectCN != "test.local" || cert.IssuerCN != "test.local" {
		t.Errorf("cn = %q/%q", cert.SubjectCN, cert.IssuerCN)
	}
	if !cert.SelfSigned {
		t.Error("expected self-signed")
	}
	if cert.ChainValid {
		t.Error("self-signed chain should be invalid against system roots")
	}
	if cert.KeyType != "RSA" || cert.KeyBits != 2048 {
		t.Errorf("key = %s/%d", cert.KeyType, cert.KeyBits)
	}
	if len(cert.SANs) != 3 { // test.local, alt.local, 127.0.0.1
		t.Errorf("sans = %v", cert.SANs)
	}
	if cert.Fingerprint == "" || cert.Serial == "" {
		t.Error("missing fingerprint/serial")
	}
}

func TestProbeKeyTypes(t *testing.T) {
	for _, kt := range []string{"ECDSA", "Ed25519"} {
		leaf := genLeaf(t, kt, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
		host, port, stop := serveTLS(t, cfgFor(leaf))
		cert := probeTLS(t, host, port, config{timeout: 3 * time.Second}, "")
		stop()
		if cert.KeyType != kt {
			t.Errorf("key type = %q, want %q", cert.KeyType, kt)
		}
	}
}

func TestProbeWeakProtocolsAndCiphers(t *testing.T) {
	leaf := genLeaf(t, "RSA", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	conf := cfgFor(leaf)
	conf.MinVersion = tls.VersionTLS10
	conf.MaxVersion = tls.VersionTLS12
	conf.CipherSuites = []uint16{
		tls.TLS_RSA_WITH_AES_128_CBC_SHA, // insecure (in InsecureCipherSuites)
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	}
	host, port, stop := serveTLS(t, conf)
	defer stop()
	cert := probeTLS(t, host, port, config{timeout: 4 * time.Second, checkWeak: true}, "test.local")
	if !contains(cert.Versions, "TLS 1.0") || !contains(cert.Versions, "TLS 1.2") {
		t.Errorf("versions = %v", cert.Versions)
	}
	if !contains(cert.WeakProtocols, "TLS 1.0") || !contains(cert.WeakProtocols, "TLS 1.1") {
		t.Errorf("weak protocols = %v", cert.WeakProtocols)
	}
	if len(cert.WeakCiphers) == 0 {
		t.Errorf("expected a weak cipher, got %v", cert.WeakCiphers)
	}
}

func TestProbeModernNoWeak(t *testing.T) {
	leaf := genLeaf(t, "ECDSA", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	conf := cfgFor(leaf)
	conf.MinVersion = tls.VersionTLS13
	host, port, stop := serveTLS(t, conf)
	defer stop()
	cert := probeTLS(t, host, port, config{timeout: 4 * time.Second, checkWeak: true}, "")
	if !contains(cert.Versions, "TLS 1.3") {
		t.Errorf("versions = %v", cert.Versions)
	}
	if len(cert.WeakProtocols) != 0 || len(cert.WeakCiphers) != 0 {
		t.Errorf("unexpected weakness: proto=%v cipher=%v", cert.WeakProtocols, cert.WeakCiphers)
	}
}

func TestStartTLSSMTP(t *testing.T) {
	leaf := genLeaf(t, "RSA", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 256)
		conn.Write([]byte("220 mail.test ESMTP\r\n"))
		conn.Read(buf) // EHLO
		conn.Write([]byte("250-mail.test\r\n250 STARTTLS\r\n"))
		conn.Read(buf) // STARTTLS
		conn.Write([]byte("220 go ahead\r\n"))
		tc := tls.Server(conn, cfgFor(leaf))
		_ = tc.Handshake()
		tc.Close()
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	p := &Plugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cert, err := p.probe(ctx, endpoint{ip: host, port: port, starttls: "smtp"}, config{timeout: 3 * time.Second}, "test.local")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if cert.SubjectCN != "test.local" {
		t.Errorf("cn = %q", cert.SubjectCN)
	}
}

func TestEndpointsByIP(t *testing.T) {
	d := plugin.DeviceInfo{
		ID: 1, PrimaryIP: "10.0.0.5", IPs: []string{"10.0.0.5"},
		Ports: []plugin.PortRef{
			{IP: "10.0.0.5", Proto: "tcp", Port: 443, Service: "http", Tunnel: "ssl"},
			{IP: "10.0.0.5", Proto: "tcp", Port: 993, Service: "imaps"},
			{IP: "10.0.0.5", Proto: "tcp", Port: 25, Service: "smtp"},
			{IP: "10.0.0.5", Proto: "tcp", Port: 22, Service: "ssh"},
		},
	}
	// Without STARTTLS: 443, 993 (+ extra 8443); not 25, not 22.
	eps := endpointsByIP(d, config{useScanned: true, extraPorts: []int{8443}})
	got := ports(eps["10.0.0.5"])
	if !contains(itoa(got), "443") || !contains(itoa(got), "993") || !contains(itoa(got), "8443") {
		t.Errorf("ports = %v", got)
	}
	if contains(itoa(got), "25") || contains(itoa(got), "22") {
		t.Errorf("unexpected plaintext port: %v", got)
	}
	// With STARTTLS: 25 becomes an smtp endpoint.
	eps = endpointsByIP(d, config{useScanned: true, starttls: true})
	var smtp bool
	for _, e := range eps["10.0.0.5"] {
		if e.port == 25 && e.starttls == "smtp" {
			smtp = true
		}
	}
	if !smtp {
		t.Error("expected STARTTLS smtp endpoint on port 25")
	}
}

func ports(eps []endpoint) []int {
	var out []int
	for _, e := range eps {
		out = append(out, e.port)
	}
	return out
}

func itoa(in []int) []string {
	var out []string
	for _, n := range in {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
