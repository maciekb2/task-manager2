package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const Namespace = "systems-lab"
const ManagedLabel = "app.kubernetes.io/part-of=systems-lab"

type Config struct {
	TimeoutMS        int     `json:"timeout_ms"`
	MaxInFlight      int     `json:"max_in_flight"`
	Breaker          bool    `json:"breaker"`
	FanOut           bool    `json:"fan_out"`
	BranchDelayMS    int     `json:"branch_delay_ms"`
	RunID            string  `json:"run_id,omitempty"`
	ReleaseChangedAt float64 `json:"release_changed_at"`
	DurationSeconds  int     `json:"duration_seconds"`
	ReleaseUntil     float64 `json:"release_until"`
	PayloadBytes     int     `json:"payload_bytes"`
	RPS              int     `json:"rps"`
	Concurrency      int     `json:"concurrency"`
	DelayMS          int     `json:"delay_ms"`
	ErrorPercent     int     `json:"error_percent"`
	CPUMS            int     `json:"cpu_ms"`
	RunUntil         float64 `json:"run_until"`
}

func DefaultConfig() Config { return Config{Concurrency: 4, DelayMS: 40} }
func (c Config) Validate() error {
	if c.TimeoutMS < 0 || c.TimeoutMS > 5000 || c.MaxInFlight < 0 || c.MaxInFlight > 20 || c.BranchDelayMS < 0 || c.BranchDelayMS > 2000 {
		return fmt.Errorf("timeout 0–5000 ms; in-flight 0–20; branch delay 0–2000 ms")
	}
	if c.PayloadBytes < 0 || c.PayloadBytes > 8192 || c.RPS < 0 || c.RPS > 50 || c.Concurrency < 1 || c.Concurrency > 20 || c.DelayMS < 0 || c.DelayMS > 3000 || c.ErrorPercent < 0 || c.ErrorPercent > 100 || c.CPUMS < 0 || c.CPUMS > 50 {
		return fmt.Errorf("parametry poza zakresem: rps 0–50, concurrency 1–20, delay 0–3000 ms, error 0–100%%, cpu 0–50 ms, payload 0–8192 B")
	}
	if c.RPS > 0 && (c.RunUntil <= unixSeconds(time.Now()) || c.RunUntil > unixSeconds(time.Now().Add(301*time.Second))) {
		return fmt.Errorf("ruch wymaga czasu działania do 300 sekund")
	}
	return nil
}

type Event struct {
	RunID            string    `json:"run_id,omitempty"`
	TransferBytes    *int64    `json:"transfer_bytes,omitempty"`
	TransferProtocol string    `json:"transfer_protocol,omitempty"`
	ID               string    `json:"id"`
	Trace            string    `json:"trace"`
	Time             time.Time `json:"time"`
	From             string    `json:"from"`
	To               string    `json:"to"`
	Kind             string    `json:"kind"`
	Status           int       `json:"status"`
	Duration         float64   `json:"duration_ms"`
	Task             string    `json:"task,omitempty"`
	Attempt          uint64    `json:"attempt,omitempty"`
	Message          string    `json:"message,omitempty"`
	Pod              string    `json:"pod"`
	TargetPod        string    `json:"target_pod,omitempty"`
	Started          time.Time `json:"started,omitempty"`
	Operation        string    `json:"operation,omitempty"`
	SpanID           string    `json:"span_id,omitempty"`
}
type Task struct {
	RunID        string    `json:"run_id,omitempty"`
	ID           string    `json:"id"`
	Trace        string    `json:"trace"`
	State        string    `json:"state"`
	Failures     int       `json:"failures"`
	Attempt      uint64    `json:"attempt"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
	Status       int       `json:"http_status"`
	TraceParent  string    `json:"traceparent,omitempty"`
	FirstStarted time.Time `json:"first_started,omitempty"`
	Finished     time.Time `json:"finished,omitempty"`
	TotalMS      float64   `json:"total_ms"`
	QueueMS      float64   `json:"queue_ms"`
}
type App struct {
	protection    protection
	role          string
	bus           *Bus
	kube          *Kube
	http          *http.Client
	mu            sync.RWMutex
	config        Config
	configAt      time.Time
	configLoading bool
	configFetchMu sync.Mutex
	events        []Event
	eventSequence uint64
	seen          map[string]bool
	actionMu      sync.Mutex
	cluster       Object
	actionToken   string
	consumerReady bool
	perf          *Observatory
	experiments   *experimentManager
}

func id() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	writeJSON(w, 400, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(v)
}
func (a *App) emit(e Event) {
	e.ID = id()
	e.Time = time.Now().UTC()
	e.Pod = os.Getenv("HOSTNAME")
	if err := a.bus.emit(e); err != nil {
		fmt.Println("event publish:", err)
	}
}
func unixSeconds(at time.Time) float64     { return float64(at.UnixMicro()) / 1e6 }
func (c Config) running(at time.Time) bool { return c.RPS > 0 && unixSeconds(at) < c.RunUntil }

// Data-plane work never waits for the control service. A single bounded refresh
// updates a valid cached snapshot; failures back off while its deadline still applies.
func (a *App) getConfig() Config {
	a.mu.Lock()
	c := a.config
	interval := time.Second
	if a.role == "generator" {
		interval = 250 * time.Millisecond
	}
	if a.role == "control" || a.configLoading || time.Since(a.configAt) < interval {
		a.mu.Unlock()
		return c
	}
	a.configLoading = true
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = a.refreshConfig(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.configAt = time.Now()
		a.configLoading = false
	}()
	return c
}

// New data-plane pods must observe the persisted scenario before accepting work.
// Later refreshes stay asynchronous so a control outage does not stall requests.
func (a *App) fetchConfig(ctx context.Context) (Config, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://control:8080/internal/config", nil)
	if err != nil {
		return Config{}, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return Config{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Config{}, fmt.Errorf("config HTTP %d", resp.StatusCode)
	}
	var c Config
	err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&c)
	return c, err
}
func (a *App) loadInitialConfig(ctx context.Context) error {
	for {
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		c, err := a.fetchConfig(attempt)
		cancel()
		if err == nil {
			a.mu.Lock()
			a.config = c
			a.configAt = time.Now()
			a.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Serialize fetch + cache publication, so a slow background response cannot
// overwrite the configuration used to admit a newly correlated phase.
func (a *App) refreshConfig(ctx context.Context) (Config, error) {
	a.configFetchMu.Lock()
	defer a.configFetchMu.Unlock()
	c, err := a.fetchConfig(ctx)
	if err == nil {
		a.mu.Lock()
		a.config = c
		a.configAt = time.Now()
		a.mu.Unlock()
	}
	return c, err
}
func (a *App) configForRun(ctx context.Context) (Config, error) {
	c := a.getConfig()
	expected := runID(ctx)
	if expected == "" || c.RunID == expected {
		return c, nil
	}
	attempt, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	c, err := a.refreshConfig(attempt)
	if err != nil {
		return c, err
	}
	if c.RunID != expected {
		return c, fmt.Errorf("phase configuration no longer active")
	}
	return c, nil
}
