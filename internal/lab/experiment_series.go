package lab

import (
	"sort"
	"time"
)

type experimentPoint struct {
	Count  int
	Errors int
	Sum    float64
	Bytes  int64
}

func (p *experimentPhase) updateSeries() {
	if p.points == nil {
		return
	}
	keys := []int64{}
	for key := range p.points {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	rows := []Object{}
	end := time.Now()
	if !p.Ended.IsZero() {
		end = p.Ended
	}
	for _, key := range keys {
		point := p.points[key]
		left := time.Unix(key, 0)
		right := left.Add(5 * time.Second)
		if left.Before(p.Started) {
			left = p.Started
		}
		if right.After(end) {
			right = end
		}
		seconds := right.Sub(left).Seconds()
		if seconds <= 0 {
			continue
		}
		var mean any
		if point.Count > 0 {
			mean = point.Sum / float64(point.Count)
		}
		rows = append(rows, Object{"at": right.UTC(), "requests": point.Count, "errors": point.Errors, "rps": float64(point.Count) / seconds, "mean_ms": mean, "payload_bytes_per_second": float64(point.Bytes) / seconds})
	}
	p.Series = rows
}
