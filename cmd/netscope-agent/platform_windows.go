package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"

	"netscope/internal/agent/proto"
	"netscope/internal/hostscript"
)

// serviceName is the Windows service install.ps1 creates.
const serviceName = "NetScopeAgent"

func defaultConfigFile() string {
	dir := os.Getenv("ProgramData")
	if dir == "" {
		dir = `C:\ProgramData`
	}
	return filepath.Join(dir, "NetScope Agent", "agent.json")
}

// logWriter: a service has no console, so it logs to agent.log next to the configuration
// (at most two files of 5 MB).
func logWriter(configPath string) io.Writer {
	if ok, _ := svc.IsWindowsService(); !ok {
		return os.Stderr
	}
	return &rotatingFile{path: filepath.Join(filepath.Dir(configPath), "agent.log")}
}

// serve runs the agent as a Windows service or, started from a console, until Ctrl-C.
func serve(run func(context.Context) error, log *slog.Logger) error {
	if ok, _ := svc.IsWindowsService(); !ok {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		if err := run(ctx); !errors.Is(err, errRestart) {
			return err
		}
		return nil
	}
	h := &service{run: run, log: log}
	if err := svc.Run(serviceName, h); err != nil {
		return err
	}
	return nil
}

// service adapts the agent to the service control manager. Its exit code tells the SCM
// what to do: 0 = stay stopped (stopped by hand or removed in NetScope), otherwise the
// recovery actions set by install.ps1 restart it (after an update or an error).
type service struct {
	run func(context.Context) error
	log *slog.Logger
}

func (s *service) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 20000}
				cancel()
				select {
				case <-done:
				case <-time.After(20 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			switch {
			case err == nil:
				return false, 0
			case errors.Is(err, errRevoked):
				s.log.Error(err.Error())
				return false, 0
			case errors.Is(err, errRestart):
				return true, 1
			default:
				s.log.Error("Agent beendet", "err", err)
				return true, 2
			}
		}
	}
}

// collectCommand runs the PowerShell collection script. It reaches PowerShell through
// stdin, so neither a script file nor the execution policy for files is involved.
func collectCommand(ctx context.Context, cfg proto.Config) *exec.Cmd {
	ps := "powershell.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		ps = filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	}
	cmd := exec.CommandContext(ctx, ps, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", "& ([scriptblock]::Create([Console]::In.ReadToEnd()))")
	cmd.Stdin = strings.NewReader(hostscript.Windows(hostscript.Options{Packages: cfg.Packages, CommandTimeout: cfg.CommandTimeout}))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func collectTimeout(cfg proto.Config) time.Duration {
	return hostscript.WindowsMaxDuration(cfg.CommandTimeout)
}

// replaceBinary swaps in the new binary. A running executable cannot be overwritten on
// Windows, but it can be renamed; the old file is removed at the next start.
func replaceBinary(exe, tmp string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

func removeOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// hostInfo describes this machine (registry values, no WMI: cheap enough for every report).
func hostInfo() proto.Host {
	h := proto.Host{Platform: "windows", Arch: platformArch(), Version: version}
	h.Hostname, _ = os.Hostname()
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY); err == nil {
		h.MachineID, _, _ = k.GetStringValue("MachineGuid")
		_ = k.Close()
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return h
	}
	defer k.Close()
	name, _, _ := k.GetStringValue("ProductName")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuildNumber")
	h.OS = windowsProductName(name, display, build)
	major, _, err1 := k.GetIntegerValue("CurrentMajorVersionNumber")
	minor, _, _ := k.GetIntegerValue("CurrentMinorVersionNumber")
	ubr, _, err2 := k.GetIntegerValue("UBR")
	if err1 == nil && build != "" {
		h.Kernel = fmt.Sprintf("%d.%d.%s", major, minor, build)
		if err2 == nil {
			h.Kernel += "." + strconv.FormatUint(ubr, 10)
		}
	}
	return h
}

// dockerAccess: containers are not collected on Windows.
func dockerAccess() bool { return false }

// rotatingFile is a log file that is renamed to .1 when it reaches maxLog.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

const maxLog = 5 << 20

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.size+int64(len(p)) > maxLog {
		_ = r.f.Close()
		r.f = nil
		_ = os.Rename(r.path, r.path+".1")
	}
	if r.f == nil {
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return len(p), nil // logging must never stop the agent
		}
		r.f, r.size = f, 0
		if fi, err := f.Stat(); err == nil {
			r.size = fi.Size()
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
