package lab

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type experimentPhase struct {
	ID           string    `json:"id"`
	Version      string    `json:"version"`
	Image        string    `json:"image"`
	Label        string    `json:"label"`
	State        string    `json:"state"`
	Prepared     time.Time `json:"prepared"`
	Started      time.Time `json:"started"`
	Deadline     time.Time `json:"deadline"`
	EndRequested time.Time `json:"end_requested"`
	Ended        time.Time `json:"ended"`
	Config       Config    `json:"config"`
	Replicas     int       `json:"replicas"`
	Duration     int       `json:"duration_seconds"`
	Series       []Object  `json:"series"`
	points       map[int64]*experimentPoint
	Summary      Object           `json:"summary"`
	Timeline     []Event          `json:"timeline"`
	Traces       []string         `json:"traces"`
	Resources    []ResourceSample `json:"resources"`
	Partial      bool             `json:"partial"`
	Note         string           `json:"note"`
	seen         map[string]bool
	started      int
	roots        durations
	outstanding  map[string]int
	tasks        map[string]bool
	jobs         map[string]bool
	last         time.Time
	lastRoot     time.Time
	payload      int64
	httpBytes    int64
	queueBytes   int64
}
type experiment struct {
	ID           string             `json:"id"`
	PersistError string             `json:"persistence_error,omitempty"`
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	State        string             `json:"state"`
	Created      time.Time          `json:"created"`
	Phases       []*experimentPhase `json:"phases"`
}
type experimentManager struct {
	mu      sync.Mutex
	kv      nats.KeyValue
	records []*experiment
	current string
}

func (m *experimentManager) activeLocked() (*experiment, *experimentPhase) {
	for _, r := range m.records {
		if r.ID == m.current {
			if len(r.Phases) == 0 {
				return r, nil
			}
			return r, r.Phases[len(r.Phases)-1]
		}
	}
	return nil, nil
}
func (m *experimentManager) save(id string) error {
	m.mu.Lock()
	var raw []byte
	for _, r := range m.records {
		if r.ID == id {
			r.PersistError = ""
			for _, p := range r.Phases {
				p.updateSeries()
			}
			raw, _ = json.Marshal(r)
			break
		}
	}
	m.mu.Unlock()
	if raw == nil {
		return fmt.Errorf("nieznany eksperyment")
	}
	_, err := m.kv.Put(id, raw)
	if err != nil {
		m.mu.Lock()
		for _, r := range m.records {
			if r.ID == id {
				r.PersistError = err.Error()
			}
		}
		m.mu.Unlock()
	}
	return err
}
func (m *experimentManager) view() Object {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return detached snapshots: HTTP encoding must not race with event aggregation.
	for _, r := range m.records {
		for _, p := range r.Phases {
			p.updateSeries()
			if p.State == "running" || p.State == "draining" {
				p.Summary = phaseSummary(p)
			}
		}
	}
	raw, _ := json.Marshal(m.records)
	var records []Object
	_ = json.Unmarshal(raw, &records)
	return Object{"experiments": records, "active": m.current}
}
func (m *experimentManager) record(e Event) {
	if e.RunID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, p := m.activeLocked()
	if p == nil || p.ID != e.RunID || (p.State != "running" && p.State != "draining") {
		return
	}
	if e.Kind != "span" {
		if p.seen[e.ID] {
			return
		}
		if len(p.seen) >= 100000 {
			p.Partial = true
			p.Note = "Osiągnięto limit 100000 zdarzeń próby."
			return
		}
		p.seen[e.ID] = true
	}
	p.last = time.Now()
	if (e.Kind == "http" && e.To == "gateway") || e.TransferBytes != nil {
		if p.points == nil {
			p.points = map[int64]*experimentPoint{}
		}
		key := e.Time.Unix() / 5 * 5
		point := p.points[key]
		if point == nil {
			point = &experimentPoint{}
			p.points[key] = point
		}
		if e.Kind == "http" && e.To == "gateway" {
			point.Count++
			point.Sum += e.Duration
			if e.Status >= 400 {
				point.Errors++
			}
		}
		if e.TransferBytes != nil {
			point.Bytes += *e.TransferBytes
		}
	}
	if e.Kind == "request-start" {
		p.started++
		p.outstanding[e.Trace]++
	}
	if e.Kind == "http" && e.To == "gateway" {
		if p.outstanding[e.Trace] > 1 {
			p.outstanding[e.Trace]--
		} else {
			delete(p.outstanding, e.Trace)
		}
		p.roots.Count++
		if e.Status >= 400 {
			p.roots.Errors++
		}
		p.roots.Sum += e.Duration
		p.roots.Max = math.Max(p.roots.Max, e.Duration)
		if len(p.roots.Values) < 30000 {
			p.roots.Values = append(p.roots.Values, e.Duration)
		}
		if e.TransferBytes != nil {
			p.payload += *e.TransferBytes
		}
		if e.Time.After(p.lastRoot) {
			p.lastRoot = e.Time
		}
		for i, trace := range p.Traces {
			if trace == e.Trace {
				p.Traces = append(p.Traces[:i], p.Traces[i+1:]...)
				break
			}
		}
		p.Traces = append(p.Traces, e.Trace)
		if len(p.Traces) > 12 {
			p.Traces = p.Traces[len(p.Traces)-12:]
		}
	}
	if e.TransferBytes != nil {
		if e.TransferProtocol == "http-response" {
			p.httpBytes += *e.TransferBytes
		}
		if e.TransferProtocol == "nats-message" {
			p.queueBytes += *e.TransferBytes
		}
	}
	if e.Kind == "queue" && e.From == "control" && e.To == "nats" {
		p.tasks[e.Task] = true
	}
	if e.Kind == "queue" && e.From == "consumer" && (e.To == "result-store" || e.To == "dead-letter") {
		delete(p.tasks, e.Task)
	}
	if e.Kind == "job-submitted" {
		p.jobs[e.Task] = true
	}
	if e.Kind == "action" || e.Kind == "queue" || e.Kind == "job" || e.Kind == "job-submitted" {
		if len(p.Timeline) < 100 {
			p.Timeline = append(p.Timeline, e)
		}
	}
}
func (a *App) initExperiments() error {
	kv, err := a.bus.js.KeyValue("lab-experiments")
	if err != nil {
		kv, err = a.bus.js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "lab-experiments", Storage: nats.FileStorage, TTL: 24 * time.Hour, MaxBytes: 8 << 20, History: 1})
	}
	if err != nil {
		return err
	}
	m := &experimentManager{kv: kv, records: []*experiment{}}
	if keys, err := kv.Keys(); err == nil {
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		if len(keys) > 10 {
			keys = keys[:10]
		}
		for _, key := range keys {
			if entry, err := kv.Get(key); err == nil {
				var r experiment
				if json.Unmarshal(entry.Value(), &r) == nil {
					m.records = append(m.records, &r)
				}
			}
		}
	}
	// A controller restart is explicit measurement interruption; sealed results
	// survive, but an uncheckpointed live measurement is never silently complete.
	for _, r := range m.records {
		if r.State == "ready" {
			m.current = r.ID
			continue
		}
		if r.State == "running" {
			r.State = "interrupted"
			for _, p := range r.Phases {
				if p.State == "running" || p.State == "preparing" || p.State == "draining" {
					p.State = "interrupted"
					p.Partial = true
					p.Note = "Restart kontrolera przerwał pomiar; powtórz próbę."
					p.Ended = time.Now().UTC()
				}
			}
			_ = m.save(r.ID)
		}
	}
	a.experiments = m
	if a.config.RunID != "" {
		// An interrupted scoped run must stop scheduled producers as well as HTTP.
		// Keep RunID persisted if the patch fails so startup can retry recovery.
		if err := a.kube.cron(true, "* * * * *", ""); err != nil {
			return err
		}
		a.config.RunID = ""
		a.config.RPS = 0
		a.config.RunUntil = 0
		return a.kube.saveConfig(a.config)
	}
	return nil
}
func (a *App) stopExperimentTraffic() error {
	if err := a.kube.cron(true, "* * * * *", a.getConfig().RunID); err != nil {
		return err
	}
	c := a.getConfig()
	c.RPS = 0
	c.RunUntil = 0
	if err := a.kube.saveConfig(c); err != nil {
		return err
	}
	a.mu.Lock()
	a.config = c
	a.mu.Unlock()
	return nil
}
func (a *App) resetLab() error {
	if err := a.changeAutoscaling(false); err != nil {
		return err
	}
	if _, err := a.changeRelease(false); err != nil {
		return err
	}
	c := DefaultConfig()
	if err := a.kube.saveConfig(c); err != nil {
		return err
	}
	a.mu.Lock()
	a.config = c
	a.mu.Unlock()
	for name, replicas := range map[string]int{"echo": 2, "gateway": 1, "validator": 1, "enricher": 1, "consumer": 1, "generator": 1, "result-store": 1} {
		if err := a.kube.scale(name, replicas); err != nil {
			return err
		}
	}
	return a.kube.cron(true, "* * * * *", "")
}
func (a *App) experimentAction(action, id, name string, c Config, duration, replicas int) (Object, error) {
	m := a.experiments
	m.mu.Lock()
	active, phase := m.activeLocked()
	m.mu.Unlock()
	if action == "create" {
		if active != nil {
			return nil, fmt.Errorf("najpierw zakończ albo przerwij bieżący eksperyment")
		}
		name = strings.TrimSpace(name)
		if len(name) > 120 {
			return nil, fmt.Errorf("nazwa do 120 bajtów")
		}
		key := fmt.Sprintf("%013d-%s", time.Now().UnixMilli(), idString())
		if name == "" {
			name = "Eksperyment " + key
		}
		r := &experiment{ID: key, Name: name, Version: env("LAB_VERSION", "dev"), State: "ready", Created: time.Now().UTC(), Phases: []*experimentPhase{}}
		m.mu.Lock()
		previousRecords := m.records
		m.records = append([]*experiment{r}, m.records...)
		if len(m.records) > 10 {
			m.records = m.records[:10]
		}
		m.current = key
		m.mu.Unlock()
		if err := m.save(key); err != nil {
			m.mu.Lock()
			m.records = previousRecords
			m.current = ""
			m.mu.Unlock()
			return nil, err
		}
		return m.view(), nil
	}
	if active == nil || active.ID != id {
		return nil, fmt.Errorf("wybierz aktywny eksperyment")
	}
	if action == "abort" || action == "close" {
		if action == "close" && phase != nil && (phase.State == "running" || phase.State == "preparing" || phase.State == "draining") {
			return nil, fmt.Errorf("najpierw zakończ pomiar")
		}
		if err := a.resetLab(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		if action == "abort" {
			active.State = "aborted"
			if phase != nil && phase.Ended.IsZero() {
				phase.State = "aborted"
				phase.Partial = true
				phase.Note = "Pomiar przerwany przez użytkownika."
				phase.Ended = time.Now().UTC()
				phase.EndRequested = phase.Ended
				if !phase.Started.IsZero() {
					phase.Summary = phaseSummary(phase)
					phase.updateSeries()
				}
				phase.roots.Values = nil
				phase.seen = nil
				phase.tasks = nil
				phase.jobs = nil
				phase.outstanding = nil
			}
		} else {
			active.State = "completed"
		}
		m.current = ""
		m.mu.Unlock()
		if err := m.save(id); err != nil {
			return nil, err
		}
		return m.view(), nil
	}
	if action == "finish" {
		if phase == nil || phase.State != "running" {
			return nil, fmt.Errorf("brak trwającego pomiaru")
		}
		if err := a.stopExperimentTraffic(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		phase.State = "draining"
		phase.EndRequested = time.Now().UTC()
		m.mu.Unlock()
		_ = m.save(id)
		return m.view(), nil
	}
	if action != "baseline" && action != "variant" {
		return nil, fmt.Errorf("nieznana akcja eksperymentu")
	}
	if phase != nil && (phase.State == "running" || phase.State == "preparing" || phase.State == "draining") {
		return nil, fmt.Errorf("poprzednia próba jeszcze trwa")
	}
	if action == "baseline" && len(active.Phases) != 0 || action == "variant" && (len(active.Phases) != 1 || active.Phases[0].State != "completed") {
		return nil, fmt.Errorf("kolejność: pomiar bazowy, potem jeden wariant")
	}
	if duration < 3 || duration > 300 || replicas < 1 || replicas > 5 {
		return nil, fmt.Errorf("czas 3–300 s; repliki workera 1–5")
	}
	c.DurationSeconds = duration
	c.RunUntil = unixSeconds(time.Now().Add(time.Duration(duration) * time.Second))
	c.ReleaseUntil = 0
	c.ReleaseChangedAt = 0
	c.AutoscaleUntil = 0
	c.RunID = active.ID + "-" + action
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.resetLab(); err != nil {
		return nil, err
	}
	if err := a.kube.scale("echo", replicas); err != nil {
		return nil, err
	}
	p := &experimentPhase{ID: c.RunID, Version: env("LAB_VERSION", "dev"), Image: env("LAB_IMAGE_CURRENT", ""), seen: map[string]bool{}, Label: action, State: "preparing", Prepared: time.Now().UTC(), Config: c, Replicas: replicas, Duration: duration, Timeline: []Event{}, Traces: []string{}, Resources: []ResourceSample{}, outstanding: map[string]int{}, tasks: map[string]bool{}, jobs: map[string]bool{}}
	m.mu.Lock()
	active.Phases = append(active.Phases, p)
	active.State = "running"
	m.mu.Unlock()
	if err := m.save(id); err != nil {
		m.mu.Lock()
		active.Phases = active.Phases[:len(active.Phases)-1]
		active.State = "ready"
		m.mu.Unlock()
		_ = a.resetLab()
		return nil, err
	}
	return m.view(), nil
}
func idString() string { return id()[:8] }
func (a *App) experimentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/experiments", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.experiments.view()) })
	mux.HandleFunc("GET /api/experiments/export", func(w http.ResponseWriter, r *http.Request) {
		view := a.experiments.view()
		for _, raw := range view["experiments"].([]Object) {
			if raw["id"] == r.URL.Query().Get("id") {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=systems-lab-%s.json", raw["id"]))
				writeJSON(w, 200, Object{"schema": "systems-lab-experiment-v1", "experiment": raw, "resource_scope": "Kubernetes lab pod/node samples, not per-request CPU or host measurements"})
				return
			}
		}
		writeJSON(w, 404, Object{"error": "nieznany eksperyment"})
	})
	mux.HandleFunc("POST /api/experiments", func(w http.ResponseWriter, r *http.Request) {
		if err := a.authorize(r); err != nil {
			writeJSON(w, 403, Object{"error": err.Error()})
			return
		}
		var v struct {
			Action   string `json:"action"`
			ID       string `json:"id"`
			Name     string `json:"name"`
			Config   Config `json:"config"`
			Duration int    `json:"duration_seconds"`
			Replicas int    `json:"replicas"`
		}
		if err := decode(w, r, &v); err != nil {
			fail(w, err)
			return
		}
		a.actionMu.Lock()
		defer a.actionMu.Unlock()
		result, err := a.experimentAction(v.Action, v.ID, v.Name, v.Config, v.Duration, v.Replicas)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
}

func (m *experimentManager) status() Object {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, p := m.activeLocked()
	if r == nil {
		return Object{"active": ""}
	}
	result := Object{"active": r.ID, "name": r.Name, "state": r.State}
	if p != nil {
		result["phase"] = Object{"id": p.ID, "label": p.Label, "state": p.State, "started": p.Started, "deadline": p.Deadline, "requests": p.roots.Count, "started_requests": p.started}
	}
	return result
}

func (a *App) experimentBusy() bool {
	if a.experiments == nil {
		return false
	}
	a.experiments.mu.Lock()
	defer a.experiments.mu.Unlock()
	_, p := a.experiments.activeLocked()
	return p != nil && (p.State == "preparing" || p.State == "draining")
}
