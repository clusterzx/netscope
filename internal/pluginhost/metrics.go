package pluginhost

import (
	"sort"
	"sync"
	"time"
)

type pluginMetrics struct {
	runs          map[string]int64 // by status
	runSeconds    float64
	lastDuration  float64
	notifications map[string]int64 // ok / error
	notifySeconds float64
	hookChanges   int64
	hookSeconds   float64
}

type metrics struct {
	mu sync.Mutex
	m  map[string]*pluginMetrics
}

func newMetrics() *metrics { return &metrics{m: map[string]*pluginMetrics{}} }

func (m *metrics) get(id string) *pluginMetrics {
	pm := m.m[id]
	if pm == nil {
		pm = &pluginMetrics{runs: map[string]int64{}, notifications: map[string]int64{}}
		m.m[id] = pm
	}
	return pm
}

func (m *metrics) run(id, status string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pm := m.get(id)
	pm.runs[status]++
	pm.runSeconds += d.Seconds()
	pm.lastDuration = d.Seconds()
}

func (m *metrics) notification(id string, ok bool, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pm := m.get(id)
	if ok {
		pm.notifications["ok"]++
	} else {
		pm.notifications["error"]++
	}
	pm.notifySeconds += d.Seconds()
}

func (m *metrics) hook(id string, n int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pm := m.get(id)
	pm.hookChanges += int64(n)
	pm.hookSeconds += d.Seconds()
}

// MetricSample is one Prometheus sample.
type MetricSample struct {
	Name   string
	Help   string
	Type   string // counter | gauge
	Labels map[string]string
	Value  float64
}

// Metrics returns plugin metrics as samples.
func (h *Host) Metrics() []MetricSample {
	h.metrics.mu.Lock()
	defer h.metrics.mu.Unlock()
	var out []MetricSample
	ids := make([]string, 0, len(h.metrics.m))
	for id := range h.metrics.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pm := h.metrics.m[id]
		statuses := make([]string, 0, len(pm.runs))
		for s := range pm.runs {
			statuses = append(statuses, s)
		}
		sort.Strings(statuses)
		for _, s := range statuses {
			out = append(out, MetricSample{"netscope_plugin_runs_total", "Plugin-Läufe nach Status", "counter",
				map[string]string{"plugin": id, "status": s}, float64(pm.runs[s])})
		}
		if len(pm.runs) > 0 {
			out = append(out, MetricSample{"netscope_plugin_run_seconds_total", "Summierte Laufzeit der Plugin-Läufe", "counter",
				map[string]string{"plugin": id}, pm.runSeconds})
			out = append(out, MetricSample{"netscope_plugin_last_run_seconds", "Dauer des letzten Laufs", "gauge",
				map[string]string{"plugin": id}, pm.lastDuration})
		}
		for _, r := range []string{"ok", "error"} {
			if n, ok := pm.notifications[r]; ok {
				out = append(out, MetricSample{"netscope_notifications_total", "Zugestellte Benachrichtigungen", "counter",
					map[string]string{"publisher": id, "result": r}, float64(n)})
			}
		}
		if pm.hookChanges > 0 {
			out = append(out, MetricSample{"netscope_processor_changes_total", "Von Processorn verarbeitete Änderungen", "counter",
				map[string]string{"plugin": id}, float64(pm.hookChanges)})
		}
	}
	h.mu.RLock()
	active := len(h.active)
	h.mu.RUnlock()
	out = append(out, MetricSample{"netscope_runs_active", "Aktuell laufende Plugin-Läufe", "gauge", nil, float64(active)})
	for id, n := range h.Backlog() {
		out = append(out, MetricSample{"netscope_processor_backlog", "Ausstehende Änderungen je Processor", "gauge",
			map[string]string{"plugin": id}, float64(n)})
	}
	return out
}
