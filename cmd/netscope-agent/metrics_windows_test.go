package main

import (
	"testing"
	"time"
)

func TestWindowsSampler(t *testing.T) {
	s := newSampler()
	t0 := time.Now()
	first, err := s.sample(t0)
	if err != nil {
		t.Fatal(err)
	}
	if first.CPU != nil || first.Load1 != nil || first.MemTotal == 0 || first.MemUsed == 0 || first.MemUsed > first.MemTotal {
		t.Fatalf("first sample %+v", first)
	}
	if len(first.Disks) == 0 {
		t.Fatal("no fixed drive")
	}
	for _, d := range first.Disks {
		if len(d.Mount) != 2 || d.Mount[1] != ':' || d.Used > d.Total {
			t.Errorf("disk %+v", d)
		}
	}
	busy := 0
	for time.Since(t0) < 300*time.Millisecond {
		busy++ // some work so the CPU counters move
	}
	second, err := s.sample(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if second.CPU == nil || *second.CPU < 0 || *second.CPU > 100 {
		t.Fatalf("cpu %v", second.CPU)
	}
	for _, n := range second.Net {
		if n.Interface == "" || n.RX < 0 || n.TX < 0 {
			t.Errorf("net %+v", n)
		}
	}
}
