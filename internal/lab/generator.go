package lab

import (
	"math"
	"time"
)

// Rate is based on elapsed time rather than assuming ticker delivery is exact.
// New experiments begin with one immediate request, including one-second runs.
// Blocked workers cannot accumulate an unbounded catch-up burst.
type trafficPacer struct {
	deadline float64
	last     time.Time
	tokens   float64
}

func (p *trafficPacer) take(c Config, now time.Time, available int) int {
	if !c.running(now) {
		p.deadline = 0
		p.tokens = 0
		p.last = now
		return 0
	}
	if p.deadline != c.RunUntil {
		p.deadline = c.RunUntil
		p.tokens = 1
	} else if !p.last.IsZero() {
		p.tokens += math.Max(0, now.Sub(p.last).Seconds()) * float64(c.RPS)
	}
	p.last = now
	p.tokens = math.Min(p.tokens, float64(c.Concurrency))
	count := int(p.tokens)
	if available < count {
		count = available
	}
	if count < 0 {
		count = 0
	}
	p.tokens -= float64(count)
	return count
}
