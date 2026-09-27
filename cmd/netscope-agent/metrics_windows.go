package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"netscope/internal/agent/proto"
)

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// winSampler reads the utilisation of a Windows host; rates need the previous reading.
// Windows has no load average, and the page file is not reported as swap.
type winSampler struct {
	prev *winReading
}

type winReading struct {
	at          time.Time
	idle, total uint64
	net         map[string]counters
}

func newSampler() *winSampler { return &winSampler{} }

func filetime(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

// sample takes one reading.
func (s *winSampler) sample(now time.Time) (proto.Sample, error) {
	var idleFT, kernelFT, userFT windows.Filetime
	if r, _, err := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idleFT)), uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT))); r == 0 {
		return proto.Sample{}, fmt.Errorf("GetSystemTimes: %w", err)
	}
	idle := filetime(idleFT)
	total := filetime(kernelFT) + filetime(userFT) // kernel time includes the idle time
	mem := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem))); r == 0 {
		return proto.Sample{}, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	smp := proto.Sample{At: now, MemTotal: mem.TotalPhys}
	if mem.TotalPhys >= mem.AvailPhys {
		smp.MemUsed = mem.TotalPhys - mem.AvailPhys
	}
	net := interfaceCounters()
	if p := s.prev; p != nil {
		if total > p.total && idle >= p.idle && idle-p.idle <= total-p.total {
			cpu := float64((total-p.total)-(idle-p.idle)) / float64(total-p.total) * 100
			smp.CPU = &cpu
		}
		if dt := now.Sub(p.at).Seconds(); dt > 0 {
			for _, name := range slices.Sorted(maps.Keys(net)) {
				c, old := net[name], p.net[name]
				if _, known := p.net[name]; known && c.rx >= old.rx && c.tx >= old.tx {
					smp.Net = append(smp.Net, proto.NetRate{Interface: name, RX: float64(c.rx-old.rx) / dt, TX: float64(c.tx-old.tx) / dt})
				}
			}
		}
	}
	smp.Disks = fixedDisks()
	s.prev = &winReading{at: now, idle: idle, total: total, net: net}
	return smp, nil
}

// fixedDisks returns the usage of the local fixed drives (C:, D: …).
func fixedDisks() []proto.Disk {
	buf := make([]uint16, 512)
	n, err := windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0])
	if err != nil || n == 0 || int(n) > len(buf) {
		return nil
	}
	// the buffer holds NUL-terminated roots: "C:\", "D:\" …
	var out []proto.Disk
	for start := 0; start < int(n); {
		end := start
		for end < int(n) && buf[end] != 0 {
			end++
		}
		root := windows.UTF16ToString(buf[start:end])
		start = end + 1
		if root == "" {
			continue
		}
		p, err := windows.UTF16PtrFromString(root)
		if err != nil || windows.GetDriveType(p) != windows.DRIVE_FIXED {
			continue
		}
		var avail, size, free uint64
		if windows.GetDiskFreeSpaceEx(p, &avail, &size, &free) != nil || size == 0 || free > size {
			continue
		}
		out = append(out, proto.Disk{Mount: strings.TrimSuffix(root, `\`), Total: size, Used: size - free})
	}
	return out
}

// Interface flags of MIB_IF_ROW2.InterfaceAndOperStatusFlags.
const (
	ifHardware = 1 << 0
	ifFilter   = 1 << 1
)

// interfaceCounters returns the byte counters of the connected physical interfaces
// (no virtual switches, tunnels or filter drivers).
func interfaceCounters() map[string]counters {
	var t *windows.MibIfTable2
	if err := windows.GetIfTable2Ex(windows.MibIfTableNormal, &t); err != nil || t == nil {
		return nil
	}
	defer windows.FreeMibTable(unsafe.Pointer(t))
	out := map[string]counters{}
	rows := unsafe.Slice(&t.Table[0], t.NumEntries)
	for i := range rows {
		r := &rows[i]
		if r.InterfaceAndOperStatusFlags&ifHardware == 0 || r.InterfaceAndOperStatusFlags&ifFilter != 0 || r.OperStatus != windows.IfOperStatusUp {
			continue
		}
		name := windows.UTF16ToString(r.Alias[:])
		if name == "" {
			continue
		}
		out[name] = counters{rx: r.InOctets, tx: r.OutOctets}
	}
	return out
}
