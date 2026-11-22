package lab

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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
	a := &App{role: role, http: &http.Client{Timeout: 12 * time.Second}, config: DefaultConfig(), seen: map[string]bool{}, events: []Event{}, actionToken: id() + id(), perf: NewObservatory()}
	shutdownTrace, err := initTracing(ctx, role)
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(flush)
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Object{"status": "ok", "role": role}) })
	if role == "web" {
		target, _ := url.Parse("http://control:8080")
		proxy := httputil.NewSingleHostReverseProxy(target)
		mux.Handle("/api/", proxy)
		traceTarget, _ := url.Parse("http://jaeger:16686")
		mux.Handle("/tracing/", httputil.NewSingleHostReverseProxy(traceTarget))
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
	server := &http.Server{Addr: ":8080", Handler: a.tracedWork(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
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
	trace := traceHex(r.Context(), traceID(r))
	ctx, finish := a.operation(r.Context(), a.role+".forward")
	status := 502
	defer func() { finish(status) }()
	r = r.WithContext(ctx)
	start := time.Now()
	if a.role == "validator" {
		_, validated := a.operation(ctx, "validator.validate")
		units, err := strconv.Atoi(r.URL.Query().Get("units"))
		if err != nil || units < 1 || units > 10 {
			status = 422
			validated(422)
			writeJSON(w, 422, Object{"error": "units must be between 1 and 10", "trace": trace})
			a.emit(Event{Trace: trace, From: "validator", To: "validator", Kind: "validation", Status: 422, Message: "Odrzucono niepoprawną liczbę jednostek"})
			return
		}
		validated(200)
	}
	next := env("UPSTREAM", "http://echo:8080")
	req, err := http.NewRequestWithContext(r.Context(), "GET", next+"/work?"+r.URL.RawQuery, nil)
	if err != nil {
		fail(w, err)
		return
	}
	req.Header.Set("X-Lab-Trace", trace)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	if a.role == "enricher" {
		_, enriched := a.operation(ctx, "enricher.decorate")
		req.Header.Set("X-Lab-Class", "demo-http")
		req.Header.Set("X-Lab-Request", id())
		enriched(200)
	}
	resp, err := a.http.Do(req)
	targetPod := ""
	var body []byte
	var transferred *int64
	if err == nil {
		defer resp.Body.Close()
		status = resp.StatusCode
		targetPod = resp.Header.Get("X-Lab-Pod")
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 16384))
		transferred = byteCount(len(body))
	} else {
		body, _ = json.Marshal(Object{"error": "upstream unavailable", "trace": trace})
	}
	to := strings.Split(strings.TrimPrefix(next, "http://"), ":")[0]
	a.emit(Event{Trace: trace, From: a.role, To: to, Kind: "http", TransferBytes: transferred, TransferProtocol: "http-response", Status: status, Duration: float64(time.Since(start).Microseconds()) / 1000, TargetPod: targetPod})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Lab-Trace", trace)
	w.Header().Set("X-Lab-Pod", os.Getenv("HOSTNAME"))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
func (a *App) echo(w http.ResponseWriter, r *http.Request) {
	ctx, finish := a.operation(r.Context(), "echo.work")
	status := 499
	defer func() { finish(status) }()
	r = r.WithContext(ctx)
	trace := traceHex(ctx, traceID(r))
	c := a.getConfig()
	_, finishDelay := a.operation(ctx, "echo.delay")
	timer := time.NewTimer(time.Duration(c.DelayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		finishDelay(499)
		return
	case <-timer.C:
	}
	finishDelay(200)
	_, finishCPU := a.operation(ctx, "echo.cpu")
	deadline := time.Now().Add(time.Duration(c.CPUMS) * time.Millisecond)
	data := []byte(trace)
	for time.Now().Before(deadline) {
		sum := sha256.Sum256(data)
		data = sum[:]
		select {
		case <-r.Context().Done():
			finishCPU(499)
			return
		default:
		}
	}
	finishCPU(200)
	status = 200
	if r.URL.Query().Get("failure") == "true" || rand.IntN(100) < c.ErrorPercent {
		status = 503
	}
	w.Header().Set("X-Lab-Pod", os.Getenv("HOSTNAME"))
	w.Header().Set("X-Lab-Trace", trace)
	writeJSON(w, status, Object{"trace": trace, "pod": os.Getenv("HOSTNAME"), "role": "echo", "class": r.Header.Get("X-Lab-Class"), "delay_ms": c.DelayMS, "cpu_ms": c.CPUMS, "http_status": status, "payload": strings.Repeat("x", c.PayloadBytes)})
}
func (a *App) callGateway(ctx context.Context, from, trace string, controlledFailure bool) (int, error) {
	ctx, finish := a.operation(traceContext(ctx, trace), from+".request")
	status := 502
	defer func() { finish(status) }()
	trace = traceHex(ctx, trace)
	targetURL := "http://gateway:8080/work?units=1"
	if controlledFailure {
		targetURL += "&failure=true"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Lab-Trace", trace)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	start := time.Now()
	resp, err := a.http.Do(req)
	target := ""
	var transferred *int64
	if err == nil {
		defer resp.Body.Close()
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 16384))
		transferred = &n
		status = resp.StatusCode
		target = resp.Header.Get("X-Lab-Pod")
	}
	a.emit(Event{Trace: trace, From: from, To: "gateway", Kind: "http", TransferBytes: transferred, TransferProtocol: "http-response", Status: status, Duration: float64(time.Since(start).Microseconds()) / 1000, TargetPod: target})
	return status, err
}
func (a *App) generate(ctx context.Context) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	slots := make(chan struct{}, 20)
	var wg sync.WaitGroup
	defer wg.Wait()
	pacer := trafficPacer{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c := a.getConfig()
			count := pacer.take(c, time.Now(), c.Concurrency-len(slots))
			for i := 0; i < count; i++ {
				slots <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-slots }()
					_, _ = a.callGateway(ctx, "generator", id()+id()[:8], false)
				}()
			}
		}
	}
}
func (a *App) runJob(ctx context.Context) error {
	trace := id() + id()[:8]
	ctx, finish := a.operation(traceContext(ctx, trace), "job.execute")
	outcome := 500
	defer func() { finish(outcome) }()
	status, err := a.callGateway(ctx, "job", trace, false)
	if os.Getenv("JOB_FAIL") == "true" {
		a.emit(Event{Trace: trace, From: "job", To: "job", Kind: "job", Status: 500, Message: "Kontrolowana awaria procesu Job"})
		return fmt.Errorf("kontrolowana awaria zadania demonstracyjnego")
	}
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("gateway returned %d", status)
	}
	outcome = status
	a.emit(Event{Trace: trace, From: "job", To: "job", Kind: "job", Status: status, Message: "Zakończono proces Job"})
	return nil
}
