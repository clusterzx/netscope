package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestEvaluateFlapDamping(t *testing.T) {
	ok := Result{OK: true}
	bad := Result{Error: "x"}
	slow := Result{OK: true, Degraded: true}
	steps := []struct {
		res   Result
		state string
	}{
		{ok, StateUp},         // unknown -> up immediately
		{bad, StateUp},        // 1st failure
		{bad, StateUp},        // 2nd failure
		{ok, StateUp},         // flap resets the counter
		{bad, StateUp},        // 1
		{bad, StateUp},        // 2
		{bad, StateDown},      // 3 -> down
		{ok, StateDown},       // 1 ok
		{ok, StateUp},         // 2 ok -> up
		{slow, StateUp},       // degraded 1
		{slow, StateUp},       // 2
		{slow, StateDegraded}, // 3 -> degraded
		{bad, StateDegraded},  // counter continues (4 bad) -> down on the next evaluation
	}
	c := &Check{State: StateUnknown, FailThreshold: 3, RecoverThreshold: 2}
	for i, st := range steps {
		tr := Evaluate(c, st.res)
		c.State = tr.NewState
		want := st.state
		if i == len(steps)-1 {
			want = StateDown // 4 consecutive bad results, now down
		}
		if c.State != want {
			t.Fatalf("step %d: state %s, want %s", i, c.State, want)
		}
	}
}

func TestParseStatus(t *testing.T) {
	m, err := ParseStatus("200-299, 301")
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[int]bool{200: true, 250: true, 301: true, 302: false, 500: false} {
		if m(code) != want {
			t.Errorf("%d: %v", code, !want)
		}
	}
	for _, bad := range []string{"abc", "99", "300-200", "200-700"} {
		if _, err := ParseStatus(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestChecksAgainstLocalServers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte("<html>status: healthy</html>"))
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	r := &runner{userAgent: "test"}
	ctx := context.Background()
	cases := []struct {
		name string
		c    Check
		ok   bool
	}{
		{"tcp open", Check{Type: TypeTCP, Target: host, Port: port, TimeoutS: 2}, true},
		{"tcp closed", Check{Type: TypeTCP, Target: host, Port: 1, TimeoutS: 2}, false},
		{"http ok + body", Check{Type: TypeHTTP, TimeoutS: 2, Config: CheckConfig{URL: srv.URL + "/", Method: "GET", BodyMatch: "healthy"}}, true},
		{"http body mismatch", Check{Type: TypeHTTP, TimeoutS: 2, Config: CheckConfig{URL: srv.URL + "/", Method: "GET", BodyMatch: "^nope$"}}, false},
		{"http 503", Check{Type: TypeHTTP, TimeoutS: 2, Config: CheckConfig{URL: srv.URL + "/down", Method: "GET"}}, false},
		{"http 503 expected", Check{Type: TypeHTTP, TimeoutS: 2, Config: CheckConfig{URL: srv.URL + "/down", Method: "GET", ExpectStatus: "503"}}, true},
	}
	for _, tc := range cases {
		c := tc.c
		c.Name = tc.name
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		res := r.execute(ctx, &c)
		if res.OK != tc.ok {
			t.Errorf("%s: ok=%v err=%s", tc.name, res.OK, res.Error)
		}
	}
}

func TestRecordOutagesAvailabilityEvents(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "hc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	c := &Check{Name: "Router", Type: TypeTCP, Target: "192.0.2.1", Port: 80, Enabled: true, FailThreshold: 1, RecoverThreshold: 1}
	if err := SaveCheck(ctx, d, c); err != nil {
		t.Fatal(err)
	}
	rc, _, evs := plugintest.RunContext(t, &Plugin{}, nil)
	rc.DB = d
	c, _ = GetCheck(ctx, d, c.ID)
	for _, res := range []Result{{OK: true, LatencyMs: 3}, {Error: "Zeitüberschreitung"}, {OK: true, LatencyMs: 4}} {
		if _, err := record(ctx, rc, c, res); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond) // samples are keyed by millisecond
	}
	var types []string
	for _, e := range evs.Events {
		types = append(types, e.Type)
	}
	if len(types) != 2 || types[0] != plugin.EvHealthDown || types[1] != plugin.EvHealthUp {
		t.Fatalf("events %v", types)
	}
	out, err := Outages(ctx, d, c.ID, 10)
	if err != nil || len(out) != 1 || out[0].EndedAt == nil {
		t.Fatalf("outages %+v %v", out, err)
	}
	// availability: a 6h outage within the last 24h of a check created 2 days ago
	now := time.Now()
	if _, err := d.W.Exec("DELETE FROM health_outages"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec("INSERT INTO health_outages(check_id, state, started_at, ended_at) VALUES (?, 'down', ?, ?)",
		c.ID, now.Add(-10*time.Hour).UnixMilli(), now.Add(-4*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	av, err := Availability(ctx, d, c.ID, now.Add(-48*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if av["24h"] < 74.9 || av["24h"] > 75.1 || av["7d"] < 87.4 || av["7d"] > 87.6 {
		t.Fatalf("availability %v", av)
	}
	var points int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM ts_raw").Scan(&points)
	if points != 2 {
		t.Fatalf("latency samples %d", points)
	}
}
