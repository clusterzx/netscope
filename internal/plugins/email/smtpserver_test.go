package email

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer is a minimal SMTP server for tests: EHLO/HELO, STARTTLS, implicit TLS,
// AUTH PLAIN/LOGIN, MAIL, RCPT, DATA, RSET, NOOP, QUIT.
type smtpServer struct {
	t           *testing.T
	ln          net.Listener
	tlsConfig   *tls.Config
	implicitTLS bool   // TLS from the first byte (SMTPS)
	starttls    bool   // advertise and accept STARTTLS
	authMechs   string // advertised AUTH mechanisms, "" = no AUTH
	// authBeforeTLS advertises AUTH on unencrypted connections too.
	authBeforeTLS bool
	user, pass    string

	mu       sync.Mutex
	sessions []*session
	wg       sync.WaitGroup
}

type session struct {
	Helo     string
	Commands []string // verbs in order
	TLS      bool
	AuthMech string
	AuthUser string // set on successful authentication
	AuthTLS  bool   // TLS was active during AUTH
	From     string
	Rcpts    []string
	Data     []byte
}

func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "NetScope Test SMTP"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}, pool
}

// startSMTP starts the server on 127.0.0.1; configure fields before calling.
func (s *smtpServer) start(t *testing.T, cert tls.Certificate) {
	t.Helper()
	s.t = t
	s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.handle(c)
			}()
		}
	}()
	t.Cleanup(func() { s.finish() })
}

func (s *smtpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// finish stops the server, waits for all sessions and returns them.
func (s *smtpServer) finish() []*session {
	s.ln.Close()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

func (s *smtpServer) handle(c net.Conn) {
	defer func() { c.Close() }()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	sess := &session{}
	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.mu.Unlock()
	if s.implicitTLS {
		tc := tls.Server(c, s.tlsConfig)
		if err := tc.Handshake(); err != nil {
			return
		}
		c, sess.TLS = tc, true
	}
	rd := bufio.NewReader(c)
	write := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	readLine := func() (string, bool) {
		l, err := rd.ReadString('\n')
		return strings.TrimRight(l, "\r\n"), err == nil
	}
	decode := func(s string) string {
		b, _ := base64.StdEncoding.DecodeString(s)
		return string(b)
	}
	write("220 test.local ESMTP NetScopeTest")
	for {
		line, ok := readLine()
		if !ok {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		sess.Commands = append(sess.Commands, verb)
		switch verb {
		case "EHLO", "HELO":
			sess.Helo = arg
			if verb == "HELO" {
				write("250 test.local")
				continue
			}
			lines := []string{"test.local"}
			if s.starttls && !sess.TLS {
				lines = append(lines, "STARTTLS")
			}
			if s.authMechs != "" && (sess.TLS || s.authBeforeTLS) {
				lines = append(lines, "AUTH "+s.authMechs)
			}
			lines = append(lines, "8BITMIME")
			for i, l := range lines {
				sep := "-"
				if i == len(lines)-1 {
					sep = " "
				}
				write("250" + sep + l)
			}
		case "STARTTLS":
			if !s.starttls || sess.TLS {
				write("502 5.5.1 STARTTLS not available")
				continue
			}
			write("220 2.0.0 Ready to start TLS")
			tc := tls.Server(c, s.tlsConfig)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, sess.TLS = tc, true
			rd = bufio.NewReader(c)
		case "AUTH":
			mech, initial, _ := strings.Cut(arg, " ")
			sess.AuthMech, sess.AuthTLS = strings.ToUpper(mech), sess.TLS
			var user, pass string
			switch sess.AuthMech {
			case "PLAIN":
				if initial == "" {
					write("334 ")
					if initial, ok = readLine(); !ok {
						return
					}
				}
				if parts := strings.Split(decode(initial), "\x00"); len(parts) == 3 {
					user, pass = parts[1], parts[2]
				}
			case "LOGIN":
				write("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
				u, ok := readLine()
				if !ok {
					return
				}
				write("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
				p, ok := readLine()
				if !ok {
					return
				}
				user, pass = decode(u), decode(p)
			default:
				write("504 5.5.4 Unrecognized authentication type")
				continue
			}
			if user == s.user && pass == s.pass {
				sess.AuthUser = user
				write("235 2.7.0 Authentication successful")
			} else {
				write("535 5.7.8 Authentication credentials invalid")
			}
		case "MAIL":
			sess.From = arg
			write("250 2.1.0 Ok")
		case "RCPT":
			sess.Rcpts = append(sess.Rcpts, arg)
			write("250 2.1.5 Ok")
		case "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			var buf bytes.Buffer
			for {
				l, err := rd.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				buf.WriteString(strings.TrimPrefix(l, "."))
			}
			sess.Data = buf.Bytes()
			write("250 2.0.0 Ok: queued as TEST")
		case "RSET", "NOOP":
			write("250 2.0.0 Ok")
		case "QUIT":
			write("221 2.0.0 Bye")
			return
		default:
			write("502 5.5.2 Error: command not recognized")
		}
	}
}
