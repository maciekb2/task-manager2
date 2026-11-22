package lab

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (a *App) experimentReady(replicas int, prepared time.Time, fanout bool) bool {
	info, err := a.bus.js.ConsumerInfo("TASKS", "workers")
	if err != nil || info.NumPending > 0 || info.NumAckPending > 0 {
		return false
	}
	a.mu.RLock()
	cluster := a.cluster
	a.mu.RUnlock()
	if parseTime(cluster["at"]).Before(prepared) || time.Since(parseTime(cluster["at"])) > 10*time.Second {
		return false
	}
	for _, job := range itemsField(cluster, "jobs") {
		finished := numField(job, "succeeded") > 0
		for _, condition := range itemsField(job, "conditions") {
			if textField(condition, "type") == "Failed" && textField(condition, "status") == "True" {
				finished = true
			}
		}
		if !finished {
			return false
		}
	}

	required := map[string]int{"generator": 1, "gateway": 1, "validator": 1, "enricher": 1, "echo": replicas, "consumer": 1, "result-store": 1}
	if fanout {
		required["inventory"] = 1
		required["pricing"] = 1
	}
	for name, count := range required {
		d, err := a.kube.object(apps("deployments/" + name))
		if err != nil {
			return false
		}
		if int(numField(d, "status", "observedGeneration")) != int(numField(d, "metadata", "generation")) || int(numField(d, "status", "readyReplicas")) != count || int(numField(d, "status", "updatedReplicas")) != count {
			return false
		}
	}
	return true
}
func phaseSummary(p *experimentPhase) Object {
	end := p.EndRequested
	if p.lastRoot.After(end) {
		end = p.lastRoot
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	seconds := end.Sub(p.Started).Seconds()
	if seconds < .001 {
		seconds = .001
	}
	result := durationView(p.roots)
	result["error_percent"] = nil
	if p.roots.Count > 0 {
		result["error_percent"] = 100 * float64(p.roots.Errors) / float64(p.roots.Count)
	}
	result["started"] = p.started
	result["outstanding"] = len(p.outstanding)
	result["pending_tasks"] = len(p.tasks)
	result["pending_jobs"] = len(p.jobs)
	result["duration_seconds"] = seconds
	result["rps"] = float64(p.roots.Count) / seconds
	result["http_payload_bytes"] = p.httpBytes
	result["queue_payload_bytes"] = p.queueBytes
	result["payload_bytes_per_second"] = float64(p.httpBytes+p.queueBytes) / seconds
	result["mean_response_bytes"] = float64(p.payload) / float64(max(1, p.roots.Count))
	result["p95_complete"] = p.roots.Count > 0 && len(p.roots.Values) == p.roots.Count
	if p.roots.Count == 0 {
		result["p95_ms"] = nil
		result["mean_ms"] = nil
		result["max_ms"] = nil
		result["mean_response_bytes"] = nil
	}
	result["p95_samples"] = len(p.roots.Values)
	result["complete"] = p.State == "completed" && !p.Partial && p.started == p.roots.Count && len(p.outstanding) == 0 && len(p.tasks) == 0 && len(p.jobs) == 0
	return result
}
func (o *Observatory) experimentResources(start, end time.Time) []ResourceSample {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := []ResourceSample{}
	seen := map[string]bool{}
	for _, sample := range o.resources {
		// Only fully contained and covered API windows are eligible for comparison.
		// No interpolation and no attribution to individual requests is implied.
		valid := sample.CPU != nil && sample.Covered > 0 && sample.Covered == sample.Expected
		keys := []string{}
		for _, pod := range sample.Pods {
			at, _ := pod["timestamp"].(time.Time)
			window, err := time.ParseDuration(fmt.Sprint(pod["window"]))
			if err != nil || window <= 0 || at.After(end) || at.Add(-window).Before(start) {
				valid = false
				break
			}
			keys = append(keys, fmt.Sprint(pod["name"])+"/"+at.Format(time.RFC3339Nano))
		}
		sort.Strings(keys)
		key := strings.Join(keys, "|")
		if valid && !seen[key] {
			seen[key] = true
			window, err := time.ParseDuration(sample.NodeWindow)
			if err != nil || sample.NodeMetricAt.Add(-window).Before(start) || sample.NodeMetricAt.After(end) {
				sample.NodeCPU = nil
				sample.NodeMemory = nil
			}
			result = append(result, sample)
		}
	}
	return result
}
func (a *App) experimentTick() {
	m := a.experiments
	m.mu.Lock()
	r, p := m.activeLocked()
	if r == nil || p == nil {
		m.mu.Unlock()
		return
	}
	state := p.State
	id := r.ID
	replicas := p.Replicas
	prepared := p.Prepared
	fanout := p.Config.FanOut
	deadline := p.Deadline
	m.mu.Unlock()
	now := time.Now().UTC()
	if state == "preparing" {
		if now.Sub(prepared) > 60*time.Second {
			m.mu.Lock()
			p.State = "failed"
			p.Partial = true
			p.Note = "Nie uzyskano bazowego stanu podów/kolejki w 60 s."
			r.State = "failed"
			m.current = ""
			m.mu.Unlock()
			_ = m.save(id)
			_ = a.resetLab()
			return
		}
		if !a.experimentReady(replicas, prepared, fanout) {
			return
		}
		c := p.Config
		c.RunUntil = unixSeconds(now.Add(time.Duration(p.Duration) * time.Second))
		if c.RPS == 0 {
			c.RunUntil = 0
		}
		if err := a.kube.saveConfig(c); err != nil {
			return
		}
		m.mu.Lock()
		p.Config = c
		p.Started = now
		p.Deadline = now.Add(time.Duration(p.Duration) * time.Second)
		p.State = "running"
		p.last = time.Now()
		m.mu.Unlock()
		a.mu.Lock()
		a.config = c
		a.mu.Unlock()

		_ = m.save(id)
		a.emit(Event{RunID: p.ID, From: "control", To: "generator", Kind: "action", Message: "Start próby " + p.Label})
		return
	}
	if state == "running" && !now.Before(deadline) {
		if err := a.stopExperimentTraffic(); err != nil {
			return
		}
		m.mu.Lock()
		p.State = "draining"
		p.EndRequested = deadline
		m.mu.Unlock()
		_ = m.save(id)
		return
	}
	if state != "draining" {
		return
	}
	a.mu.RLock()
	cluster := a.cluster
	sequence := a.eventSequence
	a.mu.RUnlock()
	stream, streamErr := a.bus.js.StreamInfo("EVENTS")
	queue, queueErr := a.bus.js.ConsumerInfo("TASKS", "workers")
	caughtUp := streamErr == nil && sequence >= stream.State.LastSeq && queueErr == nil && queue.NumPending == 0 && queue.NumAckPending == 0
	m.mu.Lock()
	for _, raw := range itemsField(cluster, "jobs") {
		if textField(raw, "run_id") == p.ID {
			p.jobs[textField(raw, "name")] = true
		}
		name := textField(raw, "name")
		finished := numField(raw, "succeeded") > 0
		for _, condition := range itemsField(raw, "conditions") {
			if textField(condition, "type") == "Failed" && textField(condition, "status") == "True" {
				finished = true
			}
		}
		if finished {
			delete(p.jobs, name)
		}
	}
	quiet := time.Since(p.last) > 1200*time.Millisecond
	finished := caughtUp && parseTime(cluster["at"]).After(p.EndRequested) && len(p.outstanding) == 0 && len(p.tasks) == 0 && len(p.jobs) == 0 && quiet
	expired := now.Sub(p.EndRequested) > 20*time.Second
	if !finished && !expired {
		m.mu.Unlock()
		return
	}
	p.Partial = p.Partial || !finished || p.started != p.roots.Count
	if p.Partial {
		p.Note = "Niepełny pomiar: pozostała praca albo brakujące zdarzenia. Nie traktuj porównania jako pełnego."
	}
	p.Ended = p.EndRequested
	if p.lastRoot.After(p.Ended) {
		p.Ended = p.lastRoot
	}
	p.State = "completed"
	p.Summary = phaseSummary(p)
	p.updateSeries()
	p.Resources = a.perf.experimentResources(p.Started, p.Ended)
	p.roots.Values = nil
	p.seen = nil
	p.outstanding = nil
	p.tasks = nil
	p.jobs = nil
	r.State = "ready"
	if p.Label == "variant" {
		r.State = "completed"
		m.current = ""
	}
	m.mu.Unlock()
	// Sealed measured facts precede resetting resources for a subsequent run.
	if err := m.save(id); err != nil {
		fmt.Println("experiment save:", err)
	}
	if err := a.resetLab(); err != nil {
		fmt.Println("experiment baseline recovery:", err)
	}
}
func (a *App) runExperiments(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.actionMu.Lock()
			a.experimentTick()
			a.actionMu.Unlock()
		}
	}
}

// Safe, short custom correlation value; only server-generated phase IDs are
// accepted by the experiment collector, even when peers supply a header.
type experimentContextKey struct{}

func runContext(ctx context.Context, id string) context.Context {
	if len(id) > 64 {
		id = ""
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			id = ""
			break
		}
	}
	return context.WithValue(ctx, experimentContextKey{}, id)
}
func runID(ctx context.Context) string {
	id, _ := ctx.Value(experimentContextKey{}).(string)
	return id
}
