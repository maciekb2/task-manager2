package lab

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOneSecondExperimentStartsImmediatelyAndKeepsFractionalDeadline(t *testing.T) {
	at := time.Unix(1000, 950000000)
	c := Config{RPS: 1, Concurrency: 1, RunUntil: unixSeconds(at.Add(time.Second))}
	if c.RunUntil != 1001.95 {
		t.Fatal("lost fractional deadline", c.RunUntil)
	}
	p := trafficPacer{}
	if got := p.take(c, at.Add(200*time.Millisecond), 1); got != 1 {
		t.Fatal("short run failed to start", got)
	}
	if got := p.take(c, at.Add(time.Second), 1); got != 0 {
		t.Fatal("expired run started more work", got)
	}
}
func TestGeneratorUsesElapsedTimeWithoutUnboundedCatchup(t *testing.T) {
	at := time.Now()
	c := Config{RPS: 20, Concurrency: 4, RunUntil: unixSeconds(at.Add(time.Minute))}
	p := trafficPacer{}
	if p.take(c, at, 4) != 1 {
		t.Fatal("no immediate request")
	}
	if n := p.take(c, at.Add(100*time.Millisecond), 4); n != 2 {
		t.Fatal("elapsed rate", n)
	}
	if n := p.take(c, at.Add(time.Second), 1); n != 1 {
		t.Fatal("ignored available slots", n)
	}
	if n := p.take(c, at.Add(1050*time.Millisecond), 4); n > 4 {
		t.Fatal("unbounded catchup", n)
	}
	c.RPS = 0
	if p.take(c, at.Add(1100*time.Millisecond), 4) != 0 {
		t.Fatal("stop ignored")
	}
}
func TestControlOutageNeverBlocksCachedDataPlaneAndRefreshIsSingleFlight(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	a := &App{role: "generator", config: Config{RPS: 10, Concurrency: 4, RunUntil: unixSeconds(time.Now().Add(time.Minute))}, http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			close(finished)
			return nil, context.DeadlineExceeded
		case <-r.Context().Done():
			close(finished)
			return nil, r.Context().Err()
		}
	})}}
	defer func() { close(release) }()
	if a.getConfig().RPS != 10 {
		t.Fatal("lost cached config")
	}
	<-entered
	before := time.Now()
	for i := 0; i < 1000; i++ {
		if a.getConfig().RPS != 10 {
			t.Fatal("lost cached config during outage")
		}
	}
	if time.Since(before) > 100*time.Millisecond {
		t.Fatal("data plane blocked on control")
	}
	if calls.Load() != 1 {
		t.Fatal("parallel refresh storm", calls.Load())
	}
	// Let the request fail and check retry backoff, while retaining its deadline.
	close(release)
	<-finished
	release = make(chan struct{})
	for i := 0; i < 100; i++ {
		a.mu.RLock()
		loading := a.configLoading
		a.mu.RUnlock()
		if !loading {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if a.getConfig().RPS != 10 || calls.Load() != 1 {
		t.Fatal("failure lost cache or bypassed retry backoff")
	}
}
func TestBackgroundConfigurationRefreshPublishesNewSnapshot(t *testing.T) {
	a := &App{role: "echo", config: DefaultConfig(), http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rps":0,"concurrency":4,"delay_ms":777}`))}, nil
	})}}
	a.getConfig()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a.getConfig().DelayMS == 777 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("valid refresh never replaced cached config")
}
