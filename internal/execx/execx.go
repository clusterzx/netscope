// Package execx runs system binaries (nmap, arp-scan) with context cancellation and
// streaming stdout, so results can be parsed and written while the process still runs.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// tailBuffer keeps the last n bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	n   int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.n {
		t.buf = t.buf[len(t.buf)-t.n:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// Stream starts name with args and passes its stdout to consume while it runs. When ctx
// is cancelled the process is killed. The returned error includes the tail of stderr.
func Stream(ctx context.Context, consume func(stdout io.Reader) error, name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s nicht gefunden: %w", name, err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	setProcAttrs(cmd)
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = 5 * time.Second
	stderr := &tailBuffer{n: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	consumeErr := consume(stdout)
	// Drain the rest so the process never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			if msg := stderr.String(); msg != "" {
				return fmt.Errorf("%s: %w: %s", name, waitErr, msg)
			}
		}
		return fmt.Errorf("%s: %w", name, waitErr)
	}
	return consumeErr
}

// Output runs a command and returns stdout (bounded to max bytes).
func Output(ctx context.Context, max int, name string, args ...string) ([]byte, error) {
	var out bytes.Buffer
	err := Stream(ctx, func(r io.Reader) error {
		_, err := io.Copy(&out, io.LimitReader(r, int64(max)))
		return err
	}, name, args...)
	return out.Bytes(), err
}
