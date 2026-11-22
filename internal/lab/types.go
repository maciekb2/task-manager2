package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

const Namespace = "systems-lab"
const ManagedLabel = "app.kubernetes.io/part-of=systems-lab"

type Config struct {
	RPS          int   `json:"rps"`
	Concurrency  int   `json:"concurrency"`
	DelayMS      int   `json:"delay_ms"`
	ErrorPercent int   `json:"error_percent"`
	CPUMS        int   `json:"cpu_ms"`
	RunUntil     int64 `json:"run_until"`
}

func DefaultConfig() Config { return Config{Concurrency: 4, DelayMS: 40} }
func (c Config) Validate() error {
	if c.RPS < 0 || c.RPS > 50 || c.Concurrency < 1 || c.Concurrency > 20 || c.DelayMS < 0 || c.DelayMS > 3000 || c.ErrorPercent < 0 || c.ErrorPercent > 100 || c.CPUMS < 0 || c.CPUMS > 50 {
		return fmt.Errorf("parametry poza zakresem: rps 0–50, concurrency 1–20, delay 0–3000 ms, error 0–100%%, cpu 0–50 ms")
	}
	if c.RPS > 0 && (c.RunUntil <= time.Now().Unix() || c.RunUntil > time.Now().Add(301*time.Second).Unix()) {
		return fmt.Errorf("ruch wymaga czasu działania do 300 sekund")
	}
	return nil
}

type Event struct {
	ID        string    `json:"id"`
	Trace     string    `json:"trace"`
	Time      time.Time `json:"time"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Kind      string    `json:"kind"`
	Status    int       `json:"status"`
	Duration  float64   `json:"duration_ms"`
	Task      string    `json:"task,omitempty"`
	Attempt   uint64    `json:"attempt,omitempty"`
	Message   string    `json:"message,omitempty"`
	Pod       string    `json:"pod"`
	TargetPod string    `json:"target_pod,omitempty"`
	Started   time.Time `json:"started,omitempty"`
	Operation string    `json:"operation,omitempty"`
	SpanID    string    `json:"span_id,omitempty"`
}
type Task struct {
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
	role        string
	bus         *Bus
	kube        *Kube
	http        *http.Client
	mu          sync.RWMutex
	config      Config
	configAt    time.Time
	events      []Event
	seen        map[string]bool
	actionMu    sync.Mutex
	cluster     Object
	actionToken string
	perf        *Observatory
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
func (a *App) getConfig() Config {
	a.mu.RLock()
	c, at := a.config, a.configAt
	a.mu.RUnlock()
	if a.role == "control" || time.Since(at) < time.Second {
		return c
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://control:8080/internal/config", nil)
	resp, err := a.http.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var next Config
			if json.NewDecoder(resp.Body).Decode(&next) == nil {
				a.mu.Lock()
				a.config = next
				a.configAt = time.Now()
				a.mu.Unlock()
				return next
			}
		}
	}
	return c
}
