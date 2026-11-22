package lab

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Deliberately per gateway process: scaling/restarting changes the protection
// boundary. This is admission control, not a distributed queue or global quota.
type admission struct {
	status     int
	reason     string
	generation uint64
	probe      bool
}
type protection struct {
	generation                            uint64
	mu                                    sync.Mutex
	active, failures                      int
	openUntil                             time.Time
	probe                                 bool
	epoch                                 string
	admitted, rejected, shorted, timedout uint64
}

func (p *protection) enter(c Config, now time.Time) admission {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !c.Breaker || p.epoch != c.RunID {
		p.generation++
		p.failures = 0
		p.openUntil = time.Time{}
		p.probe = false
		p.epoch = c.RunID
	}
	if c.MaxInFlight > 0 && p.active >= c.MaxInFlight {
		p.rejected++
		return admission{status: 429, reason: "backpressure"}
	}
	if c.Breaker && !p.openUntil.IsZero() {
		if now.Before(p.openUntil) || p.probe {
			p.shorted++
			return admission{status: 503, reason: "circuit-open"}
		}
		p.probe = true
	}
	p.active++
	p.admitted++
	return admission{generation: p.generation, probe: p.probe}
}
func (p *protection) leave(c Config, status int, now time.Time, ticket admission) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active--
	if status == 504 {
		p.timedout++
	}
	if !c.Breaker || p.epoch != c.RunID || ticket.generation != p.generation {
		return
	}
	if status >= 500 {
		p.failures++
		if p.failures >= 3 || ticket.probe {
			p.generation++
			p.openUntil = now.Add(3 * time.Second)
		}
	} else {
		p.failures = 0
		p.openUntil = time.Time{}
	}
	p.probe = false
}
func (p *protection) view() Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := "closed"
	if !p.openUntil.IsZero() {
		state = "open"
		if time.Now().After(p.openUntil) {
			state = "half-open (awaiting probe)"
		}
		if p.probe {
			state = "half-open (probe running)"
		}
	}
	return Object{"pod": os.Getenv("HOSTNAME"), "active": p.active, "state": state, "open_until": p.openUntil, "admitted": p.admitted, "backpressure_rejections": p.rejected, "circuit_rejections": p.shorted, "timeouts": p.timedout, "scope": "one responding gateway pod; counters since process start"}
}
func (a *App) protectionEvent(ctx context.Context, status int, reason string) {
	a.emit(Event{RunID: runID(ctx), Trace: traceHex(ctx, ""), From: a.role, To: a.role, Kind: "protection", Status: status, Message: reason})
}
func (a *App) branch(w http.ResponseWriter, r *http.Request) {
	ctx, finish := a.operation(r.Context(), a.role+".lookup")
	status := 499
	defer func() { finish(status) }()
	c, err := a.configForRun(ctx)
	if err != nil {
		status = 409
		writeJSON(w, status, Object{"error": "phase configuration unavailable"})
		return
	}
	delay := c.BranchDelayMS
	if a.role == "inventory" {
		delay /= 2
	}
	timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	status = 200
	w.Header().Set("X-Lab-Pod", os.Getenv("HOSTNAME"))
	writeJSON(w, status, Object{"service": a.role, "synthetic": true, "available": true})
}
func (a *App) fanOut(ctx context.Context) error {
	ctx, finish := a.operation(ctx, "enricher.fanout")
	status := 502
	defer func() { finish(status) }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	for _, target := range []string{"inventory", "pricing"} {
		go func(target string) {
			code, err := a.callWork(ctx, target)
			if err == nil && code >= 400 {
				err = fmt.Errorf("%s HTTP %d", target, code)
			}
			results <- err
		}(target)
	}
	var first error
	for range 2 {
		if err := <-results; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	if first == nil {
		status = 200
	}
	return first
}

func (a *App) callWork(ctx context.Context, target string) (int, error) {
	if target != "inventory" && target != "pricing" {
		return 0, fmt.Errorf("unsupported fixed branch")
	}
	ctx, finish := a.operation(ctx, "enricher.call."+target)
	status := 502
	defer func() { finish(status) }()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+target+":8080/work", nil)
	req.Header.Set("X-Lab-Trace", traceHex(ctx, ""))
	req.Header.Set("X-Lab-Run", runID(ctx))
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	start := time.Now()
	resp, err := a.http.Do(req)
	var bytes *int64
	pod := ""
	if err == nil {
		defer resp.Body.Close()
		n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 16384))
		bytes = byteCount(int(n))
		status = resp.StatusCode
		pod = resp.Header.Get("X-Lab-Pod")
		err = readErr
	} else if ctx.Err() != nil {
		status = 504
	}
	a.emit(Event{RunID: runID(ctx), Trace: traceHex(ctx, ""), From: "enricher", To: target, Kind: "http", Status: status, Duration: float64(time.Since(start).Microseconds()) / 1000, TransferBytes: bytes, TransferProtocol: "http-response", TargetPod: pod})
	return status, err
}
