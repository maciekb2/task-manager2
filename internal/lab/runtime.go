package lab

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func osToken() string { return os.Getenv("NATS_TOKEN") }
func Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	role := env("ROLE", "web")
	a := &App{role: role, http: &http.Client{Timeout: 12 * time.Second}, config: DefaultConfig(), seen: map[string]bool{}, events: []Event{}, actionToken: id() + id()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Object{"status": "ok", "role": role}) })
	if role == "web" {
		target, _ := url.Parse("http://control:8080")
		proxy := httputil.NewSingleHostReverseProxy(target)
		mux.Handle("/api/", proxy)
		mux.Handle("/metrics", proxy)
		mux.Handle("/", staticHandler())
	} else {
		var err error
		a.bus, err = connectBus()
		if err != nil {
			return err
		}
		defer a.bus.nc.Drain()
		if role == "control" {
			a.kube, err = newKube()
			if err != nil {
				return err
			}
			if err = a.bus.initialize(); err != nil {
				return err
			}
			a.config, err = a.kube.loadConfig()
			if err != nil {
				return err
			}
			if err = a.startControl(ctx, mux); err != nil {
				return err
			}
		} else {
			if err = a.bus.readyTasks(); err != nil {
				return err
			}
			switch role {
			case "observer":
				a.kube, err = newKube()
				if err != nil {
					return err
				}
				go a.observe(ctx)
				mux.HandleFunc("GET /snapshot", func(w http.ResponseWriter, r *http.Request) {
					a.mu.RLock()
					v := a.cluster
					a.mu.RUnlock()
					if v == nil {
						writeJSON(w, 503, Object{"error": "discovery initializing"})
						return
					}
					writeJSON(w, 200, v)
				})
			case "generator":
				go a.generate(ctx)
			case "consumer":
				go a.consume(ctx)
			case "result-store":
				if err = a.startResultStore(mux); err != nil {
					return err
				}
			case "gateway", "validator", "enricher":
				mux.HandleFunc("GET /work", a.forward)
			case "echo":
				mux.HandleFunc("GET /work", a.echo)
			case "job":
				return a.runJob(ctx)
			default:
				return fmt.Errorf("unknown role %q", role)
			}
		}
	}
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	fmt.Println("systems-lab started", role)
	select {
	case <-ctx.Done():
	case err := <-errors:
		if err != http.ErrServerClosed {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
func (a *App) observe(ctx context.Context) {
	for {
		snapshot := a.kube.snapshot()
		a.mu.Lock()
		a.cluster = snapshot
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
func traceID(r *http.Request) string {
	v := r.Header.Get("X-Lab-Trace")
	if len(v) != 32 {
		v = id() + id()[:8]
	}
	return v
}
func (a *App) forward(w http.ResponseWriter, r *http.Request) {
	trace := traceID(r)
	start := time.Now()
	if a.role == "validator" {
		units, err := strconv.Atoi(r.URL.Query().Get("units"))
		if err != nil || units < 1 || units > 10 {
			writeJSON(w, 422, Object{"error": "units must be between 1 and 10", "trace": trace})
			a.emit(Event{Trace: trace, From: "validator", To: "validator", Kind: "validation", Status: 422, Message: "Odrzucono niepoprawną liczbę jednostek"})
			return
		}
	}
	next := env("UPSTREAM", "http://echo:8080")
	req, err := http.NewRequestWithContext(r.Context(), "GET", next+"/work?"+r.URL.RawQuery, nil)
	if err != nil {
		fail(w, err)
		return
	}
	req.Header.Set("X-Lab-Trace", trace)
	if a.role == "enricher" {
		req.Header.Set("X-Lab-Class", "demo-http")
		req.Header.Set("X-Lab-Request", id())
	}
	resp, err := a.http.Do(req)
	status := 502
	targetPod := ""
	var body []byte
	if err == nil {
		defer resp.Body.Close()
		status = resp.StatusCode
		targetPod = resp.Header.Get("X-Lab-Pod")
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 16384))
	} else {
		body, _ = json.Marshal(Object{"error": "upstream unavailable", "trace": trace})
	}
	to := strings.Split(strings.TrimPrefix(next, "http://"), ":")[0]
	a.emit(Event{Trace: trace, From: a.role, To: to, Kind: "http", Status: status, Duration: float64(time.Since(start).Microseconds()) / 1000, TargetPod: targetPod})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Lab-Trace", trace)
	w.Header().Set("X-Lab-Pod", os.Getenv("HOSTNAME"))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
func (a *App) echo(w http.ResponseWriter, r *http.Request) {
	trace := traceID(r)
	c := a.getConfig()
	timer := time.NewTimer(time.Duration(c.DelayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return
	case <-timer.C:
	}
	deadline := time.Now().Add(time.Duration(c.CPUMS) * time.Millisecond)
	data := []byte(trace)
	for time.Now().Before(deadline) {
		sum := sha256.Sum256(data)
		data = sum[:]
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
	status := 200
	if rand.IntN(100) < c.ErrorPercent {
		status = 503
	}
	w.Header().Set("X-Lab-Pod", os.Getenv("HOSTNAME"))
	w.Header().Set("X-Lab-Trace", trace)
	writeJSON(w, status, Object{"trace": trace, "pod": os.Getenv("HOSTNAME"), "role": "echo", "class": r.Header.Get("X-Lab-Class"), "delay_ms": c.DelayMS, "cpu_ms": c.CPUMS, "http_status": status})
}
func (a *App) callGateway(ctx context.Context, from, trace string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://gateway:8080/work?units=1", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Lab-Trace", trace)
	start := time.Now()
	resp, err := a.http.Do(req)
	status := 502
	target := ""
	if err == nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16384))
		status = resp.StatusCode
		target = resp.Header.Get("X-Lab-Pod")
	}
	a.emit(Event{Trace: trace, From: from, To: "gateway", Kind: "http", Status: status, Duration: float64(time.Since(start).Microseconds()) / 1000, TargetPod: target})
	return status, err
}
func (a *App) generate(ctx context.Context) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	slots := make(chan struct{}, 20)
	var wg sync.WaitGroup
	defer wg.Wait()
	tokens := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c := a.getConfig()
			if c.RPS == 0 || time.Now().Unix() >= c.RunUntil {
				tokens = 0
				continue
			}
			tokens += float64(c.RPS) / 20
			if tokens > float64(c.Concurrency) {
				tokens = float64(c.Concurrency)
			}
			for tokens >= 1 && len(slots) < c.Concurrency {
				tokens--
				slots <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-slots }()
					_, _ = a.callGateway(ctx, "generator", id()+id()[:8])
				}()
			}
		}
	}
}
func (a *App) runJob(ctx context.Context) error {
	trace := id() + id()[:8]
	status, err := a.callGateway(ctx, "job", trace)
	a.emit(Event{Trace: trace, From: "job", To: "job", Kind: "job", Status: status, Message: "Wykonanie Kubernetes Job"})
	if os.Getenv("JOB_FAIL") == "true" {
		return fmt.Errorf("kontrolowana awaria zadania demonstracyjnego")
	}
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("gateway returned %d", status)
	}
	return nil
}
