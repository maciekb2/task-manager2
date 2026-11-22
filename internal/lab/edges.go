package lab

import "time"

type edgeCounter struct {
	From    string
	To      string
	Kind    string
	Count   int
	Errors  int
	Latency float64
	Last    time.Time
}

func (c *edgeCounter) observe(e Event) {
	c.Count++
	if e.Status >= 400 {
		c.Errors++
	}
	if !e.Time.Before(c.Last) {
		c.From = e.From
		c.To = e.To
		c.Kind = e.Kind
		c.Latency = e.Duration
		c.Last = e.Time
	}
}

// Topology uses independent second buckets; the display ring may legitimately
// hold only a few seconds during span-heavy traffic. Counts use closed seconds,
// while the current second can still report fresh activity for animation.
func (o *Observatory) edges(now time.Time) map[string]Object {
	o.mu.Lock()
	defer o.mu.Unlock()
	end := now.Unix()
	all := map[string]*edgeCounter{}
	for second, rows := range o.edgeBuckets {
		if second < end-30 {
			delete(o.edgeBuckets, second)
			continue
		}
		if second > end {
			continue
		}
		for key, c := range rows {
			total := all[key]
			if total == nil {
				total = &edgeCounter{}
				all[key] = total
			}
			if second < end {
				total.Count += c.Count
				total.Errors += c.Errors
			}
			if !c.Last.Before(total.Last) {
				total.From = c.From
				total.To = c.To
				total.Kind = c.Kind
				total.Last = c.Last
				total.Latency = c.Latency
			}
		}
	}
	result := map[string]Object{}
	for key, c := range all {
		result[key] = Object{"from": c.From, "to": c.To, "kind": c.Kind, "count": c.Count, "errors": c.Errors, "latency_ms": c.Latency, "last_event_at": c.Last}
	}
	return result
}
