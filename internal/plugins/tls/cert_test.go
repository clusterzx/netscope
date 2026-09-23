package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// loadPEM parses a PEM chain fixture into a certificate list.
func loadPEM(t *testing.T, name string) []*x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse cert: %v", err)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		t.Fatalf("no certificates in %s", name)
	}
	return certs
}

// TestBuildCertRealFixtures runs the certificate extraction on the certificates captured
// from the lab router (offline, no network).
func TestBuildCertRealFixtures(t *testing.T) {
	cases := []struct {
		file      string
		subjectCN string
		keyType   string
	}{
		{"glinet-443.pem", "console.gl-inet.com", "RSA"},
		{"glinet-8443.pem", "OpenWrt", "ECDSA"},
	}
	for _, c := range cases {
		certs := loadPEM(t, c.file)
		state := tls.ConnectionState{
			Version:          tls.VersionTLS13,
			CipherSuite:      tls.TLS_AES_256_GCM_SHA384,
			PeerCertificates: certs,
		}
		cert := buildCert(443, "", state)
		if cert.SubjectCN != c.subjectCN {
			t.Errorf("%s: subjectCN = %q, want %q", c.file, cert.SubjectCN, c.subjectCN)
		}
		if !cert.SelfSigned {
			t.Errorf("%s: expected self-signed", c.file)
		}
		if cert.ChainValid {
			t.Errorf("%s: self-signed chain should be invalid", c.file)
		}
		if cert.KeyType != c.keyType {
			t.Errorf("%s: keyType = %q, want %q", c.file, cert.KeyType, c.keyType)
		}
		if cert.Fingerprint == "" || len(cert.Fingerprint) != 64 {
			t.Errorf("%s: fingerprint = %q", c.file, cert.Fingerprint)
		}
		if cert.Cipher == "" {
			t.Errorf("%s: cipher empty", c.file)
		}
	}
}
