package lab

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPercentilesUseEmpiricalRank(t *testing.T) {
	for _, c := range []struct {
		values []float64
		want   float64
	}{{nil, 0}, {[]float64{1}, 1}, {[]float64{1, 100}, 100}, {[]float64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}, 10}} {
		if got := percentile(c.values, .95); got != c.want {
			t.Fatalf("p95 %v: got %v want %v", c.values, got, c.want)
		}
	}
}
func TestPerformanceWindowIndependentOfDisplayRing(t *testing.T) {
	o := NewObservatory()
	now := time.Now().Truncate(5 * time.Second)
	for i := 0; i < 6000; i++ {
		at := now.Add(-time.Duration(1+i%30) * time.Second)
		o.record(Event{Time: at, Kind: "http", From: "generator", To: "gateway", Duration: 100, Status: 200})
		for j := 0; j < 5; j++ {
			o.record(Event{Time: at, Kind: "span", From: "echo", Operation: "echo.cpu", Duration: 30, Status: 200})
		}
	}
	view := o.window(now)
	if view["requests"] != 6000 || view["rps"] != float64(200) {
		t.Fatalf("wrong traffic at bounded multi-generator rate: %v", view)
	}
	if view["p95_ms"] != float64(100) {
		t.Fatal(view)
	}
}
func TestKubernetesQuantitiesAndMissingSamples(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want float64
	}{{"100000000n", .1}, {"100m", .1}, {"2048Ki", 2097152}, {"2Mi", 2097152}, {"4096", 4096}} {
		v, e := quantity(c.raw)
		if e != nil || v != c.want {
			t.Fatalf("quantity %s: %v %v", c.raw, v, e)
		}
	}
	o := NewObservatory()
	raw, _ := json.Marshal(Object{"at": time.Now().UTC(), "pods": []Object{{"name": "echo-one", "component": "echo", "phase": "Running"}}})
	var snap Object
	_ = json.Unmarshal(raw, &snap)
	o.resource(snap)
	report := o.report(time.Now(), 1)
	latest := report["latest"].(ResourceSample)
	if latest.CPU != nil || latest.Memory != nil {
		t.Fatal("missing metrics fabricated as zero")
	}
	if latest.Expected != 1 {
		t.Fatal(latest)
	}
}

func TestTopologyWindowSurvivesSpanHeavyDisplayTraffic(t *testing.T) {
	o := NewObservatory()
	now := time.Now().Truncate(time.Second)
	for i := 0; i < 6000; i++ {
		at := now.Add(-time.Duration(1+i%29) * time.Second)
		o.record(Event{Time: at, Kind: "http", From: "generator", To: "gateway", Status: 200})
		for j := 0; j < 20; j++ {
			o.record(Event{Time: at, Kind: "span", From: "echo", To: "echo", Operation: "echo.cpu"})
		}
	}
	edges := o.edges(now)
	if edges["generator→gateway"]["count"] != 6000 {
		t.Fatal("topology truncated by spans", edges)
	}
	if len(edges) != 1 {
		t.Fatal("function spans became network edges", edges)
	}
	if len(o.edges(now.Add(31*time.Second))) != 0 {
		t.Fatal("expired edges retained")
	}
}
func TestResourceReportPreservesPodAndNodeMeasurementTimes(t *testing.T) {
	o := NewObservatory()
	now := time.Now().UTC()
	podAt := now.Add(-10 * time.Second)
	nodeAt := now.Add(-12 * time.Second)
	raw, _ := json.Marshal(Object{"at": now, "pods": []Object{{"name": "worker", "component": "echo", "phase": "Running"}}, "usage": []Object{{"metadata": Object{"name": "worker"}, "timestamp": podAt, "window": "15s", "containers": []Object{{"usage": Object{"cpu": "100m", "memory": "2Mi"}}}}}, "node_usage": []Object{{"timestamp": nodeAt, "window": "15s", "usage": Object{"cpu": "200m", "memory": "4Mi"}}}})
	var snapshot Object
	_ = json.Unmarshal(raw, &snapshot)
	o.resource(snapshot)
	report := o.report(now, 1)
	row := report["series"].([]Object)[0]
	if !row["metric_at"].(time.Time).Equal(podAt) || !row["node_metric_at"].(time.Time).Equal(nodeAt) {
		t.Fatal("poll times substituted for measurement times", row)
	}
	component := row["components"].(map[string]Object)["echo"]
	if !component["metric_at"].(time.Time).Equal(podAt) {
		t.Fatal("component time lost", component)
	}
}

func TestTrafficHistorySurvivesMissingResourceObservations(t *testing.T) {
	o := NewObservatory()
	now := time.Now().Truncate(5 * time.Second)
	for i := 0; i < 40; i++ {
		o.record(Event{Time: now.Add(-time.Second), Kind: "http", From: "generator", To: "gateway", Duration: 10, Status: 200})
	}
	report := o.report(now, 1)
	if len(report["series"].([]Object)) != 0 {
		t.Fatal("invented resource samples")
	}
	rows := report["traffic_series"].([]Object)
	if len(rows) == 0 || rows[len(rows)-1]["requests"] != 40 {
		t.Fatal("missing resource scrape erased independent HTTP history", rows)
	}
}
