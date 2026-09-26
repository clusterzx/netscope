package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeProc(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const meminfo = `MemTotal:        8000000 kB
MemFree:          500000 kB
MemAvailable:    6000000 kB
Buffers:          100000 kB
Cached:          2000000 kB
SwapTotal:       1000000 kB
SwapFree:         750000 kB
`

const mounts = `/dev/sda1 /etc/hostname ext4 rw 0 0
/dev/sda1 / ext4 rw,relatime 0 0
proc /proc proc rw 0 0
tmpfs /run tmpfs rw 0 0
/dev/sda1 /var/lib/docker/overlay ext4 rw 0 0
/dev/sdb1 /mnt/data\040disk xfs rw 0 0
overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0
rpool/data/subvol-105-disk-0 /srv zfs rw 0 0
server:/export /mnt/nfs nfs4 rw 0 0
`

func netdev(eth0rx, eth0tx string) string {
	return `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
  eth0: ` + eth0rx + ` 100 0 0 0 0 0 0 ` + eth0tx + ` 100 0 0 0 0 0 0
veth1a2b: 700 5 0 0 0 0 0 0 800 5 0 0 0 0 0 0
`
}

func TestSampler(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"mnt/data disk", "srv"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeProc(t, root, map[string]string{
		"etc/hostname":     "agent\n",
		"proc/stat":        "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\n",
		"proc/meminfo":     meminfo,
		"proc/loadavg":     "0.42 0.30 0.20 1/300 1234\n",
		"proc/net/dev":     netdev("1000", "2000"),
		"proc/self/mounts": mounts,
	})
	var asked []string
	s := &sampler{root: root, statfs: func(p string) (uint64, uint64, bool) {
		asked = append(asked, filepath.ToSlash(p))
		return 100 << 30, 40 << 30, true
	}}
	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	first, err := s.sample(t0)
	if err != nil {
		t.Fatal(err)
	}
	if first.CPU != nil || len(first.Net) != 0 {
		t.Fatalf("first sample has no rates: %+v", first)
	}
	if first.MemTotal != 8000000*1024 || first.MemUsed != 2000000*1024 || first.SwapUsed != 250000*1024 || first.Load1 != 0.42 {
		t.Fatalf("memory/load: %+v", first)
	}
	wantMounts := []string{"/", "/mnt/data disk", "/srv"}
	var got []string
	for _, d := range first.Disks {
		got = append(got, d.Mount)
	}
	if !slices.Equal(got, wantMounts) {
		t.Fatalf("mounts = %q, want %q (statfs asked for %q)", got, wantMounts, asked)
	}

	// 10 s later: 300 of 1000 new jiffies busy, eth0 +5000 / +1000 bytes
	writeProc(t, root, map[string]string{
		"proc/stat":    "cpu  250 0 250 1300 200 0 0 0 0 0\n",
		"proc/net/dev": netdev("6000", "3000"),
	})
	second, err := s.sample(t0.Add(10 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.CPU == nil || *second.CPU < 29.9 || *second.CPU > 30.1 {
		t.Fatalf("cpu = %v, want 30", second.CPU)
	}
	if len(second.Net) != 1 || second.Net[0].Interface != "eth0" || second.Net[0].RX != 500 || second.Net[0].TX != 100 {
		t.Fatalf("net = %+v", second.Net)
	}
}

func TestParseHelpers(t *testing.T) {
	if busy, total, ok := parseCPU("cpu  10 0 10 80 0 0 0 0 0 0\n"); !ok || busy != 20 || total != 100 {
		t.Fatalf("parseCPU = %d %d %v", busy, total, ok)
	}
	if _, _, ok := parseCPU("garbage"); ok {
		t.Fatal("parseCPU accepted garbage")
	}
	// kernels without MemAvailable
	m := parseMeminfo("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 100 kB\nCached: 300 kB\n")
	s := &sampler{root: t.TempDir(), statfs: statfs}
	writeProc(t, s.root, map[string]string{"proc/stat": "cpu 1 1 1 1\n", "proc/meminfo": "MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 100 kB\nCached: 300 kB\n"})
	smp, err := s.sample(time.Now())
	if err != nil || m["MemTotal"] != 1000*1024 || smp.MemUsed != 500*1024 {
		t.Fatalf("fallback: used %d err %v", smp.MemUsed, err)
	}
	for name, want := range map[string]bool{"lo": true, "eth0": false, "vmbr0": false, "veth9": true, "br-1a2b": true, "fwbr101i0": true, "wg0": false} {
		if virtualInterface(name) != want {
			t.Errorf("virtualInterface(%s) != %v", name, want)
		}
	}
	if got := unescapeMount(`/a\040b\011c`); got != "/a b\tc" {
		t.Errorf("unescape = %q", got)
	}
}
