package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"netscope/internal/agent/proto"
	"netscope/internal/hostscript"
)

// Output limits of one inventory run.
const (
	maxStdout = 16 << 20
	maxStderr = 1 << 20
)

// limited is a buffer that stops growing at max bytes.
type limited struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room < len(p) {
		l.truncated = true
		if room > 0 {
			l.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return l.buf.Write(p)
}

// collect runs the collection script with /bin/sh; the instance parses the output.
func collect(ctx context.Context, cfg proto.Config) proto.InventoryReport {
	script := hostscript.Build(hostscript.Options{Packages: cfg.Packages, Docker: cfg.Docker && dockerAccess(),
		CommandTimeout: cfg.CommandTimeout})
	ctx, cancel := context.WithTimeout(ctx, hostscript.MaxDuration(cfg.CommandTimeout))
	defer cancel()
	report := proto.InventoryReport{CollectedAt: time.Now()}
	stdout, stderr := &limited{max: maxStdout}, &limited{max: maxStderr}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C"}
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		report.Error = "Zeitüberschreitung der Erfassung"
	case err != nil && !errors.As(err, &exit):
		report.Error = err.Error()
	}
	report.Stdout, report.Stderr = stdout.buf.String(), stderr.buf.String()
	report.Truncated = stdout.truncated
	return report
}

// maxBinary bounds a downloaded agent binary.
const maxBinary = 64 << 20

// selfUpdate replaces the running binary with the version the instance offers. The
// service manager starts the new binary after the agent exits.
func (a *agent) selfUpdate(ctx context.Context, u *proto.Update) error {
	if !strings.HasPrefix(u.Path, proto.PathBinary) || len(u.SHA256) != 64 {
		return fmt.Errorf("ungültiges Update-Angebot %q", u.Path)
	}
	data, err := a.client.download(ctx, u.Path, maxBinary)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, u.SHA256) {
		return fmt.Errorf("Prüfsumme passt nicht (erwartet %s, erhalten %s)", u.SHA256, got)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil { //nolint:gosec // executable
		return fmt.Errorf("neue Version ablegen: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("neue Version einsetzen: %w", err)
	}
	a.log.Info("Agent aktualisiert", "von", version, "auf", u.Version)
	return nil
}
