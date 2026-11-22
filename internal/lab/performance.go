package lab

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type durations struct {
	Count  int
	Errors int
	Sum    float64
	Max    float64
	Values []float64
}

func (d *durations) add(value float64, status int) {
	d.Count++
	if status >= 400 {
		d.Errors++
	}
	d.Sum += value
	if value > d.Max {
		d.Max = value
	}
	if len(d.Values) < 3000 {
		d.Values = append(d.Values, value)
	}
}
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64{}, values...)
	sort.Float64s(v)
	rank := int(math.Ceil(p*float64(len(v)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(v) {
		rank = len(v) - 1
	}
	return v[rank]
}

type perfBucket struct {
	Network   map[string]*transferCounter
	Roots     durations
	Functions map[string]*durations
}
type ResourceSample struct {
	At           time.Time `json:"at"`
	MetricAt     time.Time `json:"metric_at"`
	CPU          *float64  `json:"cpu_cores"`
	Memory       *float64  `json:"memory_mib"`
	NodeCPU      *float64  `json:"node_cpu_cores"`
	NodeWindow   string    `json:"node_window"`
	NodeMetricAt time.Time `json:"node_metric_at"`
	NodeMemory   *float64  `json:"node_memory_mib"`
	Pods         []Object  `json:"pods"`
	Covered      int       `json:"covered_pods"`
	Expected     int       `json:"expected_pods"`
}
type Observatory struct {
	started     time.Time
	mu          sync.Mutex
	buckets     map[int64]*perfBucket
	edgeBuckets map[int64]map[string]*edgeCounter
	resources   []ResourceSample
}

func NewObservatory() *Observatory {
	return &Observatory{started: time.Now().UTC(), edgeBuckets: map[int64]map[string]*edgeCounter{}, buckets: map[int64]*perfBucket{}, resources: []ResourceSample{}}
}
func (o *Observatory) record(e Event) {
	if time.Since(e.Time) > 15*time.Minute || e.Time.After(time.Now().Add(time.Second)) {
		return
	}
	root := e.Kind == "http" && e.To == "gateway"
	measured := e.TransferBytes != nil && *e.TransferBytes >= 0 && (e.TransferProtocol == "http-response" || e.TransferProtocol == "nats-message")

	key := e.Time.Unix() / 5 * 5
	o.mu.Lock()
	defer o.mu.Unlock()
	if e.Kind != "span" && e.Kind != "request-start" && e.Time.Unix() >= time.Now().Unix()-30 {
		second := e.Time.Unix()
		rows := o.edgeBuckets[second]
		if rows == nil {
			rows = map[string]*edgeCounter{}
			o.edgeBuckets[second] = rows
		}
		name := e.From + "→" + e.To
		if rows[name] == nil {
			rows[name] = &edgeCounter{}
		}
		rows[name].observe(e)
		for k := range o.edgeBuckets {
			if k < time.Now().Unix()-30 {
				delete(o.edgeBuckets, k)
			}
		}
	}
	if !root && e.Kind != "span" && !measured {
		return
	}
	b := o.buckets[key]
	if b == nil {
		b = &perfBucket{Functions: map[string]*durations{}, Network: map[string]*transferCounter{}}
		o.buckets[key] = b
	}
	if measured {
		name := e.From + "→" + e.To + "/" + e.TransferProtocol
		if b.Network[name] == nil {
			b.Network[name] = &transferCounter{From: e.From, To: e.To, Protocol: e.TransferProtocol}
		}
		b.Network[name].add(e)
	}
	if root {
		b.Roots.add(e.Duration, e.Status)
	}
	if e.Kind == "span" && e.Operation != "" {
		name := e.From + "/" + e.Operation
		d := b.Functions[name]
		if d == nil {
			d = &durations{}
			b.Functions[name] = d
		}
		d.add(e.Duration, e.Status)
	}
	for k := range o.buckets {
		if k < time.Now().Add(-15*time.Minute).Unix() {
			delete(o.buckets, k)
		}
	}
}
func quantity(raw string) (float64, error) {
	for _, unit := range []struct {
		suffix string
		factor float64
	}{{"Ki", 1024}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3}, {"k", 1000}, {"M", 1e6}, {"G", 1e9}} {
		if strings.HasSuffix(raw, unit.suffix) {
			v, e := strconv.ParseFloat(strings.TrimSuffix(raw, unit.suffix), 64)
			return v * unit.factor, e
		}
	}
	return strconv.ParseFloat(raw, 64)
}
func parseTime(v any) time.Time {
	s, _ := v.(string)
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
func metricFresh(timestamp time.Time, window string) bool {
	if timestamp.IsZero() || timestamp.After(time.Now().Add(5*time.Second)) {
		return false
	}
	maxAge := 45 * time.Second
	if d, e := time.ParseDuration(window); e == nil && 2*d+15*time.Second > maxAge {
		maxAge = 2*d + 15*time.Second
	}
	if maxAge > 3*time.Minute {
		maxAge = 3 * time.Minute
	}
	return time.Since(timestamp) <= maxAge
}
func (o *Observatory) resource(snapshot Object) {
	at := parseTime(snapshot["at"])
	if at.IsZero() || time.Since(at) > 20*time.Second {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.resources) > 0 && !at.After(o.resources[len(o.resources)-1].At) {
		return
	}
	sample := ResourceSample{At: at, Pods: []Object{}}
	components := map[string]string{}
	if pods, ok := snapshot["pods"].([]any); ok {
		for _, p := range pods {
			if textField(p, "phase") != "Running" {
				continue
			}
			components[textField(p, "name")] = textField(p, "component")
			if textField(p, "phase") == "Running" {
				sample.Expected++
			}
		}
	}
	cpu, mem := 0.0, 0.0
	for _, p := range itemsField(snapshot, "usage") {
		timestamp := parseTime(field(p, "timestamp"))
		if !metricFresh(timestamp, textField(p, "window")) {
			continue
		}
		pc, pm := 0.0, 0.0
		valid := true
		for _, c := range itemsField(p, "containers") {
			v, e := quantity(textField(c, "usage", "cpu"))
			if e != nil {
				valid = false
			}
			pc += v
			v, e = quantity(textField(c, "usage", "memory"))
			if e != nil {
				valid = false
			}
			pm += v / (1 << 20)
		}
		if !valid {
			continue
		}
		name := textField(p, "metadata", "name")
		component, exists := components[name]
		if !exists {
			continue
		}
		sample.Pods = append(sample.Pods, Object{"name": name, "component": component, "cpu_millicores": pc * 1000, "memory_mib": pm, "timestamp": timestamp, "window": field(p, "window")})
		cpu += pc
		mem += pm
		sample.Covered++
		if sample.MetricAt.IsZero() || timestamp.Before(sample.MetricAt) {
			sample.MetricAt = timestamp
		}
	}
	if sample.Covered > 0 {
		sample.CPU = &cpu
		sample.Memory = &mem
	}
	nodeWindow := time.Duration(0)
	nc, nm, nn := 0.0, 0.0, 0
	for _, n := range itemsField(snapshot, "node_usage") {
		ts := parseTime(field(n, "timestamp"))
		if !metricFresh(ts, textField(n, "window")) {
			continue
		}
		c, e := quantity(textField(n, "usage", "cpu"))
		m, me := quantity(textField(n, "usage", "memory"))
		if e == nil && me == nil {
			nc += c
			nm += m / (1 << 20)
			nn++
			if window, err := time.ParseDuration(textField(n, "window")); err == nil && window > nodeWindow {
				nodeWindow = window
			}
			if sample.NodeMetricAt.IsZero() || ts.Before(sample.NodeMetricAt) {
				sample.NodeMetricAt = ts
			}
		}
	}
	if nn > 0 {
		sample.NodeWindow = nodeWindow.String()
		sample.NodeCPU = &nc
		sample.NodeMemory = &nm
	}
	o.resources = append(o.resources, sample)
	if len(o.resources) > 300 {
		o.resources = o.resources[len(o.resources)-300:]
	}
}
func mergeDurations(to *durations, from durations) {
	to.Count += from.Count
	to.Errors += from.Errors
	to.Sum += from.Sum
	if from.Max > to.Max {
		to.Max = from.Max
	}
	to.Values = append(to.Values, from.Values...)
}
func durationView(d durations) Object {
	mean := 0.0
	if d.Count > 0 {
		mean = d.Sum / float64(d.Count)
	}
	return Object{"count": d.Count, "errors": d.Errors, "mean_ms": mean, "p95_ms": percentile(d.Values, .95), "max_ms": d.Max}
}
func (o *Observatory) window(now time.Time) Object {
	o.mu.Lock()
	defer o.mu.Unlock()
	end := now.Unix() / 5 * 5
	total := durations{}
	for key, b := range o.buckets {
		if key >= end-30 && key < end {
			mergeDurations(&total, b.Roots)
		}
	}
	return Object{"requests": total.Count, "errors": total.Errors, "rps": float64(total.Count) / 30, "p95_ms": percentile(total.Values, .95), "window_seconds": 30, "window_end": time.Unix(end, 0).UTC()}
}
func (o *Observatory) report(now time.Time, minutes int) Object {
	if minutes != 1 && minutes != 5 && minutes != 15 {
		minutes = 5
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)
	series := []Object{}
	functions := map[string]*durations{}
	latest := ResourceSample{}
	for _, r := range o.resources {
		if r.At.Before(cutoff) {
			continue
		}
		point := Object{"at": r.At, "metric_at": r.MetricAt, "cpu_cores": r.CPU, "memory_mib": r.Memory, "node_metric_at": r.NodeMetricAt, "node_cpu_cores": r.NodeCPU, "node_memory_mib": r.NodeMemory, "covered_pods": r.Covered, "expected_pods": r.Expected}
		components := map[string]Object{}
		for _, p := range r.Pods {
			name, _ := p["component"].(string)
			if components[name] == nil {
				components[name] = Object{"cpu_cores": 0.0, "memory_mib": 0.0}
			}
			components[name]["cpu_cores"] = components[name]["cpu_cores"].(float64) + p["cpu_millicores"].(float64)/1000
			components[name]["memory_mib"] = components[name]["memory_mib"].(float64) + p["memory_mib"].(float64)
		}
		for _, p := range r.Pods {
			name, _ := p["component"].(string)
			at, _ := p["timestamp"].(time.Time)
			previous, _ := components[name]["metric_at"].(time.Time)
			if previous.IsZero() || at.Before(previous) {
				components[name]["metric_at"] = at
			}
		}
		point["components"] = components
		end := r.At.Unix() / 5 * 5
		b := o.buckets[end-5]
		if b != nil {
			point["rps"] = float64(b.Roots.Count) / 5
			point["p95_ms"] = percentile(b.Roots.Values, .95)
			point["requests"] = b.Roots.Count
			point["errors"] = b.Roots.Errors
		} else {
			point["rps"] = 0
			point["p95_ms"] = nil
			point["requests"] = 0
			point["errors"] = 0
		}
		series = append(series, point)
		latest = r
	}
	for key, b := range o.buckets {
		if key < cutoff.Unix() {
			continue
		}
		for name, d := range b.Functions {
			if functions[name] == nil {
				functions[name] = &durations{}
			}
			mergeDurations(functions[name], *d)
		}
	}
	traffic := []Object{}
	end := now.Unix() / 5 * 5
	for second := cutoff.Unix() / 5 * 5; second < end; second += 5 {
		b := o.buckets[second]
		row := Object{"at": time.Unix(second+5, 0).UTC(), "rps": 0.0, "requests": 0, "errors": 0, "p95_ms": nil}
		if b != nil {
			row["rps"] = float64(b.Roots.Count) / 5
			row["requests"] = b.Roots.Count
			row["errors"] = b.Roots.Errors
			row["p95_ms"] = percentile(b.Roots.Values, .95)
		}
		traffic = append(traffic, row)
	}
	rows := []Object{}
	for name, d := range functions {
		row := durationView(*d)
		row["name"] = name
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["name"]) < fmt.Sprint(rows[j]["name"]) })
	return Object{"series": series, "traffic_series": traffic, "functions": rows, "latest": latest, "minutes": minutes, "source": "Kubernetes Metrics API + application spans", "retention_minutes": 15, "attribution": "CPU/RAM to próbki poda/węzła, nie koszt pojedynczego żądania. Czasy spanów są zagnieżdżone — nie sumuj ich.", "trace_storage": "Jaeger: pamięć ograniczona do 5000 trace; restart czyści historię trace"}
}
