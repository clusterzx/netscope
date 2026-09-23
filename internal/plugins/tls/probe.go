package tls

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
)

// versionName maps a TLS version constant to its label.
var versionNames = []struct {
	name string
	ver  uint16
}{
	{"TLS 1.0", tls.VersionTLS10},
	{"TLS 1.1", tls.VersionTLS11},
	{"TLS 1.2", tls.VersionTLS12},
	{"TLS 1.3", tls.VersionTLS13},
}

func versionName(v uint16) string {
	for _, vn := range versionNames {
		if vn.ver == v {
			return vn.name
		}
	}
	return "unbekannt"
}

// probe handshakes with the endpoint, builds the certificate record and, when configured,
// probes for weak protocols and ciphers.
func (p *Plugin) probe(ctx context.Context, ep endpoint, cfg config, serverName string) (*plugin.TLSCert, error) {
	state, err := handshake(ctx, ep, cfg, serverName, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("kein Zertifikat geliefert")
	}
	cert := buildCert(ep.port, serverName, state)
	if cfg.checkWeak {
		supported, weakProto := testVersions(ctx, ep, cfg, serverName)
		cert.Versions = supported
		cert.WeakProtocols = weakProto
		cert.WeakCiphers = testWeakCiphers(ctx, ep, cfg, serverName)
	} else {
		cert.Versions = []string{versionName(state.Version)}
	}
	return cert, nil
}

// buildCert extracts the leaf certificate and TLS parameters from a handshake.
func buildCert(port int, serverName string, state tls.ConnectionState) *plugin.TLSCert {
	leaf := state.PeerCertificates[0]
	fp := sha256.Sum256(leaf.Raw)
	c := &plugin.TLSCert{
		Port:         port,
		ServerName:   serverName,
		Fingerprint:  hex.EncodeToString(fp[:]),
		SubjectCN:    leaf.Subject.CommonName,
		Issuer:       leaf.Issuer.String(),
		IssuerCN:     leaf.Issuer.CommonName,
		Serial:       leaf.SerialNumber.Text(16),
		NotBefore:    leaf.NotBefore,
		NotAfter:     leaf.NotAfter,
		SignatureAlg: leaf.SignatureAlgorithm.String(),
		Cipher:       tls.CipherSuiteName(state.CipherSuite),
	}
	c.SANs = append(c.SANs, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		c.SANs = append(c.SANs, ip.String())
	}
	c.SelfSigned = bytes.Equal(leaf.RawSubject, leaf.RawIssuer) &&
		leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil
	c.KeyType, c.KeyBits = keyInfo(leaf)

	inter := x509.NewCertPool()
	for _, ic := range state.PeerCertificates[1:] {
		inter.AddCert(ic)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Intermediates: inter}); err != nil {
		c.ChainValid = false
		c.ChainError = err.Error()
	} else {
		c.ChainValid = true
	}
	return c
}

func keyInfo(cert *x509.Certificate) (string, int) {
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA", pub.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", pub.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	}
	return "", 0
}

// allCipherIDs is every cipher suite the Go stack knows (secure and insecure), used when
// probing which protocol versions a server supports so that servers offering only legacy
// ciphers are still detected.
func allCipherIDs() []uint16 {
	var out []uint16
	for _, cs := range tls.CipherSuites() {
		out = append(out, cs.ID)
	}
	for _, cs := range tls.InsecureCipherSuites() {
		out = append(out, cs.ID)
	}
	return out
}

// testVersions returns the supported TLS versions and the weak ones (1.0/1.1).
func testVersions(ctx context.Context, ep endpoint, cfg config, serverName string) (supported, weak []string) {
	ciphers := allCipherIDs()
	for _, vn := range versionNames {
		if ctx.Err() != nil {
			return supported, weak
		}
		if _, err := handshake(ctx, ep, cfg, serverName, vn.ver, vn.ver, ciphers); err == nil {
			supported = append(supported, vn.name)
			if vn.ver == tls.VersionTLS10 || vn.ver == tls.VersionTLS11 {
				weak = append(weak, vn.name)
			}
		}
	}
	return supported, weak
}

// testWeakCiphers returns the insecure cipher suites (TLS ≤ 1.2) the server accepts.
func testWeakCiphers(ctx context.Context, ep endpoint, cfg config, serverName string) []string {
	var weak []string
	for _, cs := range tls.InsecureCipherSuites() {
		if ctx.Err() != nil {
			return weak
		}
		state, err := handshake(ctx, ep, cfg, serverName, tls.VersionTLS10, tls.VersionTLS12, []uint16{cs.ID})
		if err == nil && state.CipherSuite == cs.ID {
			weak = append(weak, tls.CipherSuiteName(cs.ID))
		}
	}
	return weak
}

// handshake dials the endpoint (negotiating STARTTLS when required) and performs a TLS
// handshake with the given version bounds and ciphers (0 / nil = library defaults).
func handshake(ctx context.Context, ep endpoint, cfg config, serverName string, minV, maxV uint16, ciphers []uint16) (tls.ConnectionState, error) {
	var zero tls.ConnectionState
	dialer := net.Dialer{Timeout: cfg.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ep.ip, strconv.Itoa(ep.port)))
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(cfg.timeout))

	if ep.starttls != "" {
		if err := negotiateStartTLS(conn, ep.starttls); err != nil {
			return zero, fmt.Errorf("STARTTLS: %w", err)
		}
	}
	conf := &tls.Config{InsecureSkipVerify: true, ServerName: serverName}
	if minV != 0 {
		conf.MinVersion = minV
	}
	if maxV != 0 {
		conf.MaxVersion = maxV
	}
	if ciphers != nil {
		conf.CipherSuites = ciphers
	}
	tconn := tls.Client(conn, conf)
	hctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	if err := tconn.HandshakeContext(hctx); err != nil {
		return zero, err
	}
	return tconn.ConnectionState(), nil
}

// negotiateStartTLS drives the plaintext protocol up to the point TLS begins.
func negotiateStartTLS(conn net.Conn, proto string) error {
	switch proto {
	case "smtp":
		if err := expectCode(conn, "220"); err != nil {
			return err
		}
		if err := writeLine(conn, "EHLO netscope"); err != nil {
			return err
		}
		if err := drainMultiline(conn, "250"); err != nil {
			return err
		}
		if err := writeLine(conn, "STARTTLS"); err != nil {
			return err
		}
		return expectCode(conn, "220")
	case "imap":
		if _, err := readLine(conn); err != nil { // greeting "* OK …"
			return err
		}
		if err := writeLine(conn, "a1 STARTTLS"); err != nil {
			return err
		}
		return expectPrefix(conn, "a1 OK")
	case "pop3":
		if err := expectPrefix(conn, "+OK"); err != nil {
			return err
		}
		if err := writeLine(conn, "STLS"); err != nil {
			return err
		}
		return expectPrefix(conn, "+OK")
	case "ftp":
		if err := drainMultiline(conn, "220"); err != nil {
			return err
		}
		if err := writeLine(conn, "AUTH TLS"); err != nil {
			return err
		}
		return expectCode(conn, "234")
	}
	return fmt.Errorf("unbekanntes STARTTLS-Protokoll %q", proto)
}

func writeLine(conn net.Conn, s string) error {
	_, err := conn.Write([]byte(s + "\r\n"))
	return err
}

// readLine reads one CRLF-terminated line, byte by byte to avoid buffering past the
// STARTTLS boundary.
func readLine(conn net.Conn) (string, error) {
	var b []byte
	buf := make([]byte, 1)
	for len(b) < 4096 {
		n, err := conn.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			if buf[0] != '\r' {
				b = append(b, buf[0])
			}
		}
		if err != nil {
			return string(b), err
		}
	}
	return string(b), nil
}

func expectCode(conn net.Conn, code string) error {
	line, err := readLine(conn)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, code) {
		return fmt.Errorf("unerwartete Antwort %q", line)
	}
	return nil
}

func expectPrefix(conn net.Conn, prefix string) error {
	line, err := readLine(conn)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("unerwartete Antwort %q", line)
	}
	return nil
}

// drainMultiline reads a (possibly multi-line) reply "code-…" until the final "code ".
func drainMultiline(conn net.Conn, code string) error {
	for {
		line, err := readLine(conn)
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, code+" ") {
			return nil
		}
		if !strings.HasPrefix(line, code+"-") {
			// Single-line reply without continuation marker.
			if strings.HasPrefix(line, code) {
				return nil
			}
			return fmt.Errorf("unerwartete Antwort %q", line)
		}
	}
}
