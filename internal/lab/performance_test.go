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
