//go:build !windows

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"netscope/internal/agent/proto"
	"netscope/internal/hostscript"
)

func defaultConfigFile() string { return "/var/lib/netscope-agent/agent.json" }

// logWriter: systemd and OpenRC collect stderr.
func logWriter(string) io.Writer { return os.Stderr }

// serve runs the agent until SIGTERM or Ctrl-C. After an update it exits normally; systemd
// (Restart=always) and OpenRC (supervise-daemon) start the new binary.
func serve(run func(context.Context) error, _ *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	if err := run(ctx); !errors.Is(err, errRestart) {
		return err
	}
	return nil
}

// collectCommand runs the POSIX sh collection script.
func collectCommand(ctx context.Context, cfg proto.Config) *exec.Cmd {
	script := hostscript.Build(hostscript.Options{Packages: cfg.Packages, Docker: cfg.Docker && dockerAccess(),
		CommandTimeout: cfg.CommandTimeout})
	cmd := exec.CommandContext(ctx, "/bin/sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C"}
	return cmd
}

func collectTimeout(cfg proto.Config) time.Duration {
	return hostscript.MaxDuration(cfg.CommandTimeout)
}

// replaceBinary moves the new binary over the running one (fine on Unix).
func replaceBinary(exe, tmp string) error { return os.Rename(tmp, exe) }

func removeOldBinary() {}

func newSampler() *sampler { return &sampler{root: "/", statfs: statfs} }

// hostInfo describes this machine.
func hostInfo() proto.Host {
	h := proto.Host{Platform: "linux", Arch: platformArch(), Version: version}
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

// dockerAccess reports whether the agent may talk to the docker engine.
func dockerAccess() bool {
	c, err := net.DialTimeout("unix", "/var/run/docker.sock", time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
