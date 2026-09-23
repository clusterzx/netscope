package icmp

import (
	"testing"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestObservationReplies(t *testing.T) {
	st := &probing.Statistics{PacketsSent: 3, PacketsRecv: 2, PacketLoss: 100.0 / 3,
		MinRtt: 812 * time.Microsecond, AvgRtt: 1500 * time.Microsecond, MaxRtt: 2188 * time.Microsecond}
	obs := observation(7, "192.168.8.1", "", st)
	if obs == nil || !obs.Present || obs.DeviceID != 7 || obs.IP != "192.168.8.1" || len(obs.Metrics) != 2 {
		t.Fatalf("observation: %+v", obs)
	}
	rtt, loss := obs.Metrics[0], obs.Metrics[1]
	if rtt.Name != MetricRTT || rtt.Unit != "ms" || rtt.Min != 0.812 || rtt.Avg != 1.5 || rtt.Max != 2.188 || rtt.Key != "" {
		t.Errorf("rtt: %+v", rtt)
	}
	if loss.Name != MetricLoss || loss.Unit != "%" || loss.Min != loss.Max || loss.Avg < 33.3 || loss.Avg > 33.4 {
		t.Errorf("loss: %+v", loss)
	}
}

func TestObservationNoReply(t *testing.T) {
	obs := observation(9, "192.168.8.250", "192.168.8.250", &probing.Statistics{PacketsSent: 3, PacketLoss: 100})
	if obs == nil || obs.Present || len(obs.Metrics) != 1 {
		t.Fatalf("observation: %+v", obs)
	}
	m := obs.Metrics[0]
	if m.Name != MetricLoss || m.Min != 100 || m.Avg != 100 || m.Max != 100 || m.Key != "192.168.8.250" {
		t.Errorf("loss: %+v", m)
	}
}

func TestObservationNothingSent(t *testing.T) {
	if obs := observation(1, "192.168.8.1", "", &probing.Statistics{}); obs != nil {
		t.Fatalf("expected nil, got %+v", obs)
	}
	if obs := observation(1, "192.168.8.1", "", nil); obs != nil {
		t.Fatalf("expected nil, got %+v", obs)
	}
}

func TestObservationClampsLoss(t *testing.T) {
	// Duplicate replies can make pro-bing report a negative loss.
	obs := observation(1, "192.168.8.1", "", &probing.Statistics{PacketsSent: 2, PacketsRecv: 2, PacketLoss: -50,
		MinRtt: time.Millisecond, AvgRtt: time.Millisecond, MaxRtt: time.Millisecond})
	if obs.Metrics[1].Avg != 0 {
		t.Fatalf("loss not clamped: %+v", obs.Metrics[1])
	}
}

func TestTargets(t *testing.T) {
	devs := []plugin.DeviceInfo{
		{ID: 1, PrimaryIP: "192.168.8.1", IPs: []string{"192.168.8.1", "10.0.0.1", "fe80::1"}},
		{ID: 2, IPs: []string{"192.168.8.2"}},
		{ID: 3},
		{ID: 4, PrimaryIP: "127.0.0.1"},
	}
	got := targetsFor(devs, false)
	if len(got) != 2 || got[0].ip.String() != "192.168.8.1" || got[0].key != "" || got[1].deviceID != 2 {
		t.Fatalf("primary only: %+v", got)
	}
	got = targetsFor(devs, true)
	if len(got) != 3 {
		t.Fatalf("all ips: %+v", got)
	}
	if got[1].ip.String() != "10.0.0.1" || got[1].key != "10.0.0.1" || got[2].deviceID != 2 {
		t.Fatalf("all ips: %+v", got)
	}
}

func TestSettings(t *testing.T) {
	rc, _, _ := plugintest.RunContext(t, &Plugin{}, map[string]any{"interval": "100ms", "host_timeout": "2s", "count": 5})
	cfg := loadConfig(rc.Settings)
	if cfg.count != 5 || cfg.interval != 100*time.Millisecond || cfg.timeout() != 2400*time.Millisecond || !cfg.privileged || cfg.size != 56 {
		t.Fatalf("config: %+v timeout=%s", cfg, cfg.timeout())
	}
}
