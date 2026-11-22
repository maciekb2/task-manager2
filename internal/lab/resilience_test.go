package lab

import (
	"testing"
	"time"
)

func TestBackpressureHasNoHiddenWaitingQueue(t *testing.T) {
	p := &protection{}
	c := Config{MaxInFlight: 1}
	now := time.Now()
	ticket := p.enter(c, now)
	if ticket.status != 0 {
		t.Fatal(ticket)
	}
	if rejected := p.enter(c, now); rejected.status != 429 {
		t.Fatal("accepted beyond capacity", rejected)
	}
	p.leave(c, 200, now, ticket)
	if next := p.enter(c, now); next.status != 0 {
		t.Fatal("did not release capacity", next)
	}
}
func TestBreakerOpenSingleProbeAndRecovery(t *testing.T) {
	p := &protection{}
	c := Config{Breaker: true, RunID: "run"}
	now := time.Now()
	late := p.enter(c, now)
	for range 3 {
		ticket := p.enter(c, now)
		p.leave(c, 503, now, ticket)
	}
	if p.enter(c, now).status != 503 {
		t.Fatal("did not trip")
	}
	p.leave(c, 200, now, late)
	if p.enter(c, now).status != 503 {
		t.Fatal("late old success closed circuit")
	}
	probe := p.enter(c, now.Add(4*time.Second))
	if probe.status != 0 || !probe.probe {
		t.Fatal("no recovery probe", probe)
	}
	if p.enter(c, now.Add(4*time.Second)).status != 503 {
		t.Fatal("concurrent recovery probes")
	}
	p.leave(c, 200, now.Add(4*time.Second), probe)
	if p.enter(c, now.Add(4*time.Second)).status != 0 {
		t.Fatal("did not recover")
	}
}
