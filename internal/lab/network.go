package lab

import (
	"sort"
	"time"
)

// Payload observations are counted once at a receiving HTTP client or at the
// application publish/delivery boundary. They are not interface byte counters.
type transferCounter struct {
	From     string
	To       string
	Protocol string
	Count    int
	Bytes    int64
	Errors   int
	First    time.Time
	Last     time.Time
}

func (t *transferCounter) add(e Event) {
	t.Count++
	t.Bytes += *e.TransferBytes
	if e.Status >= 400 {
		t.Errors++
	}
	if t.First.IsZero() || e.Time.Before(t.First) {
		t.First = e.Time
	}
	if e.Time.After(t.Last) {
		t.Last = e.Time
	}
}
func mergeTransfer(to *transferCounter, from *transferCounter) {
	to.From = from.From
	to.To = from.To
	to.Protocol = from.Protocol
	to.Count += from.Count
	to.Bytes += from.Bytes
	to.Errors += from.Errors
	if to.First.IsZero() || from.First.Before(to.First) {
		to.First = from.First
	}
	if from.Last.After(to.Last) {
		to.Last = from.Last
	}
}
func byteCount(n int) *int64 { value := int64(n); return &value }
func (o *Observatory) networkWindow(now time.Time) Object {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.networkWindowLocked(now, 30)
}
func (o *Observatory) networkWindowLocked(now time.Time, seconds int64) Object {
	end := now.Unix() / 5 * 5
	edges := map[string]*transferCounter{}
	for key, b := range o.buckets {
		if key < end-seconds || key >= end {
			continue
		}
		for name, counter := range b.Network {
			if edges[name] == nil {
				edges[name] = &transferCounter{}
			}
			mergeTransfer(edges[name], counter)
		}
	}
	rows := []Object{}
	var total, httpBytes, queueBytes int64
	for _, c := range edges {
		var interval any
		if c.Count > 1 {
			interval = float64(c.Last.Sub(c.First).Microseconds()) / 1000 / float64(c.Count-1)
		}
		source, target := c.From, c.To
		if c.Protocol == "http-response" {
			source, target = c.To, c.From
			httpBytes += c.Bytes
		} else {
			queueBytes += c.Bytes
		}
		rows = append(rows, Object{"from": c.From, "to": c.To, "source": source, "target": target, "protocol": c.Protocol, "count": c.Count, "bytes": c.Bytes, "bytes_per_second": float64(c.Bytes) / float64(seconds), "per_second": float64(c.Count) / float64(seconds), "mean_bytes": float64(c.Bytes) / float64(c.Count), "mean_interval_ms": interval, "errors": c.Errors, "last_event_at": c.Last})
		total += c.Bytes
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["bytes"].(int64) > rows[j]["bytes"].(int64) })
	return Object{"edges": rows, "bytes": total, "bytes_per_second": float64(total) / float64(seconds), "http_bytes_per_second": float64(httpBytes) / float64(seconds), "queue_bytes_per_second": float64(queueBytes) / float64(seconds), "window_seconds": seconds, "window_end": time.Unix(end, 0).UTC()}
}
func (o *Observatory) networkReport(now time.Time) Object {
	o.mu.Lock()
	defer o.mu.Unlock()
	end := now.Unix() / 5 * 5
	series := []Object{}
	for key := end - 300; key < end; key += 5 {
		if key < o.started.Unix()/5*5 {
			continue
		}
		var httpBytes, queueBytes int64
		if b := o.buckets[key]; b != nil {
			for _, c := range b.Network {
				if c.Protocol == "http-response" {
					httpBytes += c.Bytes
				} else {
					queueBytes += c.Bytes
				}
			}
		}
		series = append(series, Object{"at": time.Unix(key+5, 0).UTC(), "http_bytes_per_second": float64(httpBytes) / 5, "queue_bytes_per_second": float64(queueBytes) / 5})
	}
	return Object{"summary": o.networkWindowLocked(now, 30), "recent": o.networkWindowLocked(now, 5), "series": series, "at": now.UTC(), "observed_since": o.started, "scope": "HTTP response bodies + NATS task/DLQ message payloads; excludes headers, ACK/NAK, KV, telemetry, Kubernetes API, TCP/IP, TLS and CNI overhead"}
}
