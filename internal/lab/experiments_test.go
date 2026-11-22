package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func testExperiment() (*experimentManager, *experimentPhase) {
	p := &experimentPhase{ID: "scope-baseline", State: "running", Started: time.Now().Add(-10 * time.Second), seen: map[string]bool{}, outstanding: map[string]int{}, tasks: map[string]bool{}, jobs: map[string]bool{}}
	m := &experimentManager{current: "scope", records: []*experiment{{ID: "scope", State: "running", Phases: []*experimentPhase{p}}}}
	return m, p
}
func TestExperimentScopesRootAndPayloadCountsAndDeduplicatesEvents(t *testing.T) {
	m, p := testExperiment()
	now := time.Now()
	events := []Event{{ID: "start", RunID: p.ID, Time: now, Kind: "request-start", From: "generator", To: "gateway", Trace: "trace"}, {ID: "root", RunID: p.ID, Time: now, Kind: "http", From: "generator", To: "gateway", Trace: "trace", Duration: 25, Status: 200, TransferProtocol: "http-response", TransferBytes: byteCount(200)}, {ID: "hop", RunID: p.ID, Time: now, Kind: "http", From: "gateway", To: "validator", Trace: "trace", Duration: 20, Status: 200, TransferProtocol: "http-response", TransferBytes: byteCount(200)}, {ID: "old", RunID: "previous-baseline", Time: now, Kind: "http", To: "gateway", Duration: 9999, Status: 503, TransferBytes: byteCount(9999)}}
	for _, e := range events {
		m.record(e)
		m.record(e)
	}
	if p.started != 1 || p.roots.Count != 1 || len(p.outstanding) != 0 || p.httpBytes != 400 {
		t.Fatalf("mixed or duplicate observations: starts%d roots%d bytes%d", p.started, p.roots.Count, p.httpBytes)
	}
	p.State = "completed"
	p.EndRequested = now
	summary := phaseSummary(p)
	if summary["p95_ms"] != float64(25) || summary["complete"] != true {
		t.Fatal(summary)
	}
	p.updateSeries()
	if len(p.Series) != 1 || p.Series[0]["requests"] != 1 {
		t.Fatal("wrong per-run series", p.Series)
	}
}
func TestExperimentWaitsForOwnedQueuedWork(t *testing.T) {
	m, p := testExperiment()
	now := time.Now()
	m.record(Event{ID: "publish", RunID: p.ID, Time: now, Kind: "queue", From: "control", To: "nats", Task: "task"})
	if len(p.tasks) != 1 {
		t.Fatal("lost pending task")
	}
	m.record(Event{ID: "retry", RunID: p.ID, Time: now, Kind: "queue", From: "consumer", To: "nats", Task: "task"})
	if len(p.tasks) != 1 {
		t.Fatal("retry counted as terminal")
	}
	m.record(Event{ID: "done", RunID: p.ID, Time: now, Kind: "queue", From: "consumer", To: "result-store", Task: "task"})
	if len(p.tasks) != 0 {
		t.Fatal("persisted outcome did not clear owned task")
	}
}
func TestExperimentSnapshotsAreDetachedAndConcurrent(t *testing.T) {
	m, p := testExperiment()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				id := fmt.Sprintf("%d-%d", i, j)
				m.record(Event{ID: "s" + id, RunID: p.ID, Time: time.Now(), Kind: "request-start", Trace: id})
				m.record(Event{ID: "e" + id, RunID: p.ID, Time: time.Now(), Kind: "http", To: "gateway", Trace: id, Status: 200})
				_ = m.view()
			}
		}(i)
	}
	wg.Wait()
	if p.roots.Count != 500 {
		t.Fatal(p.roots.Count)
	}
	view := m.view()
	view["experiments"].([]Object)[0]["id"] = "changed"
	if m.records[0].ID != "scope" {
		t.Fatal("HTTP view aliases mutable state")
	}
}
func TestExperimentEmptyAndPartialMeasurementsAreNotSuccessfulLatencyData(t *testing.T) {
	_, p := testExperiment()
	p.State = "completed"
	p.EndRequested = time.Now()
	summary := phaseSummary(p)
	if summary["p95_ms"] != nil || summary["mean_ms"] != nil {
		t.Fatal("empty latency fabricated as zero", summary)
	}
	p.started = 1
	p.outstanding["missing"] = 1
	p.Partial = true
	if phaseSummary(p)["complete"] != false {
		t.Fatal("missing completion treated as complete")
	}
}
func TestExperimentRejectsLimitsBeforeMutatingCluster(t *testing.T) {
	for _, c := range []Config{{RPS: 51, Concurrency: 1}, {RPS: 5, Concurrency: 21}, {RPS: 5, Concurrency: 1, PayloadBytes: 8193}} {
		m, _ := testExperiment()
		m.records[0].Phases = nil
		m.records[0].State = "ready"
		a := &App{experiments: m}
		if _, err := a.experimentAction("baseline", "scope", "", c, 10, 2); err == nil {
			t.Fatal("unbounded settings reached cluster")
		}
	}
}
func TestRunContextAndJobEnvironmentRemainBounded(t *testing.T) {
	if runID(runContext(context.Background(), "scope-baseline")) != "scope-baseline" {
		t.Fatal("run context lost")
	}
	if runID(runContext(context.Background(), "bad\nheader")) != "" {
		t.Fatal("invalid correlation accepted")
	}
	var c []any
	_ = json.Unmarshal([]byte(`[{"name":"job","env":[{"name":"LAB_RUN_ID","value":"old"},{"name":"NATS_TOKEN","valueFrom":{"secretKeyRef":{"name":"lab-nats","key":"token"}}}]}]`), &c)
	setRunEnv(c, "scope-variant")
	env := itemsField(c[0], "env")
	if len(env) != 2 || textField(env[0], "value") != "scope-variant" || textField(env[1], "valueFrom", "secretKeyRef", "name") != "lab-nats" {
		t.Fatal("job carrier duplicated or changed credential reference")
	}
}
func TestExperimentResourcesRequireWholeMeasurementWindow(t *testing.T) {
	o := NewObservatory()
	now := time.Now().UTC()
	cpu := .1
	memory := 5.0
	sample := ResourceSample{At: now, MetricAt: now.Add(-time.Second), CPU: &cpu, Memory: &memory, Covered: 1, Expected: 1, Pods: []Object{{"name": "echo-one", "timestamp": now.Add(-time.Second), "window": "15s"}}}
	o.resources = []ResourceSample{sample, sample}
	if rows := o.experimentResources(now.Add(-10*time.Second), now); len(rows) != 0 {
		t.Fatal("sample includes pre-experiment CPU", rows)
	}
	if rows := o.experimentResources(now.Add(-20*time.Second), now); len(rows) != 1 {
		t.Fatal("missing or duplicated actual resource sample", rows)
	}
}

func TestPhaseAdmissionRefreshesOldEpochAndRejectsDifferentEpoch(t *testing.T) {
	a := &App{role: "echo", config: Config{RunID: "old"}, configAt: time.Now(), http: &http.Client{Transport: configTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"run_id":"new","delay_ms":750,"concurrency":1}`))}, nil
	})}}
	c, err := a.configForRun(runContext(context.Background(), "new"))
	if err != nil || c.DelayMS != 750 {
		t.Fatal("used old scenario under new correlation", c, err)
	}
	if _, err = a.configForRun(runContext(context.Background(), "other")); err == nil {
		t.Fatal("admitted a retired phase")
	}
	// A matching cached epoch remains usable while control is unavailable.
	a.http = &http.Client{Transport: configTransport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}
	if _, err = a.configForRun(runContext(context.Background(), "new")); err != nil {
		t.Fatal(err)
	}
}
