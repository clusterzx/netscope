// Package sshx is the shared SSH client for plugins (ssh scanner, openwrt and docker
// importers). It authenticates with vault credentials and pins host keys on first use
// (trust on first use) in a known_hosts file; a changed host key is rejected.
package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"netscope/internal/plugin"
)

// Options configure a connection.
type Options struct {
	Port    int           // default 22
	Timeout time.Duration // dial + handshake timeout, default 10s
	// KnownHosts is the known_hosts file used for host key pinning. Unknown hosts are
	// added (TOFU). Empty disables host key checking (not recommended).
	KnownHosts string
}

// Client is an SSH connection.
type Client struct {
	c    *ssh.Client
	Host string
}

var khMu sync.Mutex

// ErrHostKeyChanged is returned when a pinned host key does not match.
var ErrHostKeyChanged = errors.New("SSH-Hostschlüssel hat sich geändert (möglicher Man-in-the-Middle) – Eintrag in known_hosts prüfen")

func hostKeyCallback(file string) (ssh.HostKeyCallback, error) {
	if file == "" {
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // explicitly configured
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	khMu.Lock()
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDONLY, 0o600)
	if err == nil {
		f.Close()
	}
	khMu.Unlock()
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		khMu.Lock()
		defer khMu.Unlock()
		cb, err := knownhosts.New(file)
		if err != nil {
			return err
		}
		err = cb(hostname, remote, key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) > 0 {
				return fmt.Errorf("%w: %s", ErrHostKeyChanged, hostname)
			}
			line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
			f, ferr := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			_, ferr = f.WriteString(line + "\n")
			return ferr
		}
		return err
	}, nil
}

// AuthMethods builds SSH auth methods from a vault credential (types ssh or password).
func AuthMethods(cred *plugin.Credential) (string, []ssh.AuthMethod, error) {
	if cred == nil {
		return "", nil, plugin.ErrNoCredential
	}
	if err := cred.RequireType(plugin.CredSSH, plugin.CredPassword); err != nil {
		return "", nil, err
	}
	user := cred.Get("username")
	if user == "" {
		return "", nil, fmt.Errorf("Credential %q: Benutzername fehlt", cred.Name)
	}
	var methods []ssh.AuthMethod
	if key := cred.Get("private_key"); key != "" {
		var (
			signer ssh.Signer
			err    error
		)
		if pass := cred.Get("passphrase"); pass != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(pass))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(key))
		}
		if err != nil {
			return "", nil, fmt.Errorf("Credential %q: privater Schlüssel ungültig: %w", cred.Name, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if pw := cred.Get("password"); pw != "" {
		methods = append(methods, ssh.Password(pw), ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = pw
			}
			return answers, nil
		}))
	}
	if len(methods) == 0 {
		return "", nil, fmt.Errorf("Credential %q: weder Schlüssel noch Passwort", cred.Name)
	}
	return user, methods, nil
}

// Dial connects and authenticates.
func Dial(ctx context.Context, host string, cred *plugin.Credential, opt Options) (*Client, error) {
	user, methods, err := AuthMethods(cred)
	if err != nil {
		return nil, err
	}
	if opt.Port == 0 {
		opt.Port = 22
	}
	if opt.Timeout == 0 {
		opt.Timeout = 10 * time.Second
	}
	hk, err := hostKeyCallback(opt.KnownHosts)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(opt.Port))
	d := net.Dialer{Timeout: opt.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(opt.Timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	cfg := &ssh.ClientConfig{User: user, Auth: methods, HostKeyCallback: hk, Timeout: opt.Timeout,
		ClientVersion: "SSH-2.0-NetScope"}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return &Client{c: ssh.NewClient(cc, chans, reqs), Host: host}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.c.Close() }

// Raw exposes the underlying client.
func (c *Client) Raw() *ssh.Client { return c.c }

type limitedBuffer struct {
	buf bytes.Buffer
	max int
	cut bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		n := l.max - l.buf.Len()
		if n > 0 {
			l.buf.Write(p[:n])
		}
		l.cut = true
		return len(p), nil
	}
	return l.buf.Write(p)
}

// Result of a remote command.
type Result struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Truncated bool
}

// Run executes a command and returns its output (each stream capped at maxOutput bytes).
// A non-zero exit status is not an error; check Result.ExitCode.
func (c *Client) Run(ctx context.Context, cmd string, maxOutput int) (*Result, error) {
	if maxOutput <= 0 {
		maxOutput = 8 << 20
	}
	sess, err := c.c.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	stdout := &limitedBuffer{max: maxOutput}
	stderr := &limitedBuffer{max: 64 << 10}
	sess.Stdout, sess.Stderr = stdout, stderr
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	var runErr error
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return nil, ctx.Err()
	case runErr = <-done:
	}
	res := &Result{Stdout: stdout.buf.Bytes(), Stderr: stderr.buf.Bytes(), Truncated: stdout.cut}
	if runErr != nil {
		var ee *ssh.ExitError
		if errors.As(runErr, &ee) {
			res.ExitCode = ee.ExitStatus()
			return res, nil
		}
		var em *ssh.ExitMissingError
		if errors.As(runErr, &em) {
			res.ExitCode = -1
			return res, nil
		}
		return res, runErr
	}
	return res, nil
}

// DialUnix opens a stream to a unix socket on the remote host (e.g. /var/run/docker.sock).
func (c *Client) DialUnix(path string) (net.Conn, error) { return c.c.Dial("unix", path) }

// ReadAll is a helper to read a remote file with cat (read-only).
func (c *Client) ReadAll(ctx context.Context, path string, max int) ([]byte, error) {
	res, err := c.Run(ctx, "cat "+ShellQuote(path), max)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("cat %s: exit %d: %s", path, res.ExitCode, bytes.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string {
	return "'" + string(bytes.ReplaceAll([]byte(s), []byte("'"), []byte(`'\''`))) + "'"
}

var _ io.Writer = (*limitedBuffer)(nil)
