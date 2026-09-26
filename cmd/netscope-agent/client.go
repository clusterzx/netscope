package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"netscope/internal/agent/proto"
)

// errRevoked: the instance no longer knows this agent (removed or token revoked).
var errRevoked = errors.New("der Agent wurde in NetScope entfernt – bitte neu installieren")

// config is the agent's state file (written by enroll, 0600).
type config struct {
	URL         string `json:"url"`
	AgentID     int64  `json:"agentId"`
	Secret      string `json:"secret"`
	Fingerprint string `json:"fingerprint,omitempty"` // pinned TLS certificate (self-signed)
}

func loadConfig(path string) (*config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration lesen (erst „netscope-agent enroll“ ausführen): %w", err)
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("Konfiguration %s: %w", path, err)
	}
	if c.URL == "" || c.Secret == "" {
		return nil, fmt.Errorf("Konfiguration %s ist unvollständig", path)
	}
	return &c, nil
}

func (c *config) save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeFingerprint(s string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(s)))
}

// hostInfo describes this machine.
func hostInfo() proto.Host {
	h := proto.Host{Arch: platformArch(), Version: version}
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			h.MachineID = strings.TrimSpace(string(b))
			break
		}
	}
	h.Hostname, _ = os.Hostname()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.Kernel = strings.TrimSpace(string(b))
	}
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				h.OS = strings.Trim(v, `"'`)
			}
		}
		break
	}
	return h
}

// platformArch is the platform name of the binaries (amd64, arm64, armv7).
func platformArch() string {
	if runtime.GOARCH != "arm" {
		return runtime.GOARCH
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" {
				return "armv" + strings.TrimSuffix(s.Value, ",softfloat")
			}
		}
	}
	return "armv7"
}

// dockerAccess reports whether the agent may talk to the docker engine.
func dockerAccess() bool {
	c, err := net.DialTimeout("unix", "/var/run/docker.sock", time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// client talks to the instance.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(base, fingerprint, token string) *client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if fingerprint != "" {
		want := fingerprint
		tr.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // replaced by the pinned fingerprint below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return errors.New("kein Zertifikat")
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if got := hex.EncodeToString(sum[:]); got != want {
					return fmt.Errorf("Zertifikat der Instanz passt nicht zum hinterlegten Fingerprint (erhalten %s)", got)
				}
				return nil
			},
		}
	}
	// the long poll waits up to 50 s
	return &client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Transport: tr, Timeout: 90 * time.Second}}
}

// do sends in as gzip-compressed JSON (if not nil) and decodes the answer into out.
func (c *client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if err := json.NewEncoder(zw).Encode(in); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		body = &buf
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "netscope-agent/"+version)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && c.token != "" {
		return errRevoked
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		msg := strings.TrimSpace(string(b))
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		if resp.StatusCode == http.StatusNotFound && msg == "" {
			msg = "keine NetScope-Instanz mit Agent-Unterstützung unter dieser Adresse"
		}
		return fmt.Errorf("NetScope antwortet mit HTTP %d: %s", resp.StatusCode, msg)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// download fetches a file from the instance (e.g. a new agent binary).
func (c *client) download(ctx context.Context, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d für %s", resp.StatusCode, path)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s ist größer als %d MB", path, limit>>20)
	}
	return b, nil
}
