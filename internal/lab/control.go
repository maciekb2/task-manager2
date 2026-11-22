package lab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func localHost(host string) bool {
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
func (a *App) authorize(r *http.Request) error {
	if !localHost(r.Host) {
		return fmt.Errorf("panel sterowania dostępny przez localhost")
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return fmt.Errorf("cross-site request rejected")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host {
			return fmt.Errorf("origin mismatch")
		}
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Lab-Token")), []byte(a.actionToken)) != 1 {
		return fmt.Errorf("wymagany token sesji panelu")
	}
	return nil
}
func (a *App) startControl(ctx context.Context, mux *http.ServeMux) error {
	if err := a.initExperiments(); err != nil {
		return err
	}
	a.experimentRoutes(mux)
	go a.runExperiments(ctx)
	go a.recoverRelease(ctx)
	go a.recoverAutoscaling(ctx)
	_, err := a.bus.js.Subscribe("lab.events", func(m *nats.Msg) {
		var e Event
		if json.Unmarshal(m.Data, &e) != nil {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if metadata, err := m.Metadata(); err == nil && metadata.Sequence.Stream > a.eventSequence {
			a.eventSequence = metadata.Sequence.Stream
		}
		if a.seen[e.ID] {
			return
		}
		a.seen[e.ID] = true
		a.perf.record(e)
		a.experiments.record(e)
		a.events = append(a.events, e)
		if len(a.events) > 10000 {
			delete(a.seen, a.events[0].ID)
			a.events = a.events[1:]
		}
	}, nats.OrderedConsumer(), nats.DeliverAll())
	if err != nil {
		return err
	}
	go func() {
		for {
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			req, _ := http.NewRequestWithContext(reqCtx, "GET", "http://observer:8080/snapshot", nil)
			resp, e := a.http.Do(req)
			if e == nil {
				var v Object
				if resp.StatusCode == 200 && json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&v) == nil {
					a.mu.Lock()
					a.cluster = v
					a.perf.resource(v)
					a.mu.Unlock()
				}
				resp.Body.Close()
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, 403, Object{"error": "localhost only"})
			return
		}
		writeJSON(w, 200, Object{"token": a.actionToken})
	})
	mux.HandleFunc("GET /internal/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.getConfig()) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		events := append([]Event{}, a.events...)
		cluster := a.cluster
		config := a.config
		a.mu.RUnlock()
		sort.Slice(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
		edges := a.perf.edges(time.Now())

		if len(events) > 120 {
			events = events[len(events)-120:]
		}
		reverse := []Event{}
		for i := len(events) - 1; i >= 0; i-- {
			reverse = append(reverse, events[i])
		}
		writeJSON(w, 200, Object{"experiment": a.experiments.status(), "network": a.perf.networkWindow(time.Now()), "cluster": cluster, "config": config, "events": reverse, "edges": edges, "queue": a.queueState(), "metrics": a.perf.window(time.Now()), "self": Object{"release_images": Object{"current": env("LAB_IMAGE_CURRENT", ""), "candidate": env("LAB_IMAGE_CANDIDATE", "")}, "role": "control", "namespace": Namespace, "discovery": "Kubernetes API (3 s)", "transport": "NATS JetStream", "retention": "EVENTS max 1h/20000/32MiB; OUTCOMES max 1h/10000/16MiB; task KV max 24h/8MiB", "version": env("LAB_VERSION", "dev")}, "at": time.Now().UTC()})
	})
	mux.HandleFunc("GET /api/network", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.perf.networkReport(time.Now())) })
	mux.HandleFunc("GET /api/performance", func(w http.ResponseWriter, r *http.Request) {
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		writeJSON(w, 200, a.perf.report(time.Now(), minutes))
	})
	mux.HandleFunc("GET /api/otel/trace", func(w http.ResponseWriter, r *http.Request) {
		tid := r.URL.Query().Get("id")
		if _, e := trace.TraceIDFromHex(tid); e != nil {
			fail(w, fmt.Errorf("invalid trace ID"))
			return
		}
		resp, e := a.http.Get("http://jaeger:16686/tracing/api/traces/" + tid)
		if e != nil {
			writeJSON(w, 503, Object{"error": "Jaeger niedostępny"})
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
	})
	mux.HandleFunc("GET /api/trace", func(w http.ResponseWriter, r *http.Request) {
		trace := r.URL.Query().Get("id")
		events := []Event{}
		a.mu.RLock()
		for _, e := range a.events {
			if e.Trace == trace {
				events = append(events, e)
			}
		}
		a.mu.RUnlock()
		sort.Slice(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
		writeJSON(w, 200, events)
	})
	mux.HandleFunc("GET /api/results", func(w http.ResponseWriter, r *http.Request) {
		resp, e := a.http.Get("http://result-store:8080/results")
		if e != nil {
			writeJSON(w, 503, Object{"error": "magazyn wyników niedostępny"})
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("pod")
		if name == "" || strings.ContainsAny(name, "/?#%") {
			fail(w, fmt.Errorf("invalid pod"))
			return
		}
		o, e := a.kube.object(core("pods/" + name))
		if e != nil {
			fail(w, e)
			return
		}
		if textField(o, "metadata", "labels", "app.kubernetes.io/part-of") != "systems-lab" {
			writeJSON(w, 403, Object{"error": "outside lab"})
			return
		}
		raw, e := a.kube.request("GET", core("pods/"+name+"/log?tailLines=60&limitBytes=20000"), nil)
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, Object{"logs": string(raw)})
	})
	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		if e := a.authorize(r); e != nil {
			writeJSON(w, 403, Object{"error": e.Error()})
			return
		}
		var v struct {
			Config
		}
		if e := decode(w, r, &v); e != nil {
			fail(w, e)
			return
		}
		if v.RPS > 0 {
			if v.DurationSeconds < 1 || v.DurationSeconds > 300 {
				fail(w, fmt.Errorf("duration 1–300 s"))
				return
			}
			v.RunUntil = unixSeconds(time.Now().Add(time.Duration(v.DurationSeconds) * time.Second))
		} else {
			v.RunUntil = 0
		}
		if e := v.Config.Validate(); e != nil {
			fail(w, e)
			return
		}
		a.actionMu.Lock()
		defer a.actionMu.Unlock()
		if a.experimentBusy() {
			writeJSON(w, 409, Object{"error": "Próba przygotowuje stan albo kończy pomiar; poczekaj lub przerwij eksperyment."})
			return
		}
		v.AutoscaleUntil = a.getConfig().AutoscaleUntil
		v.ReleaseUntil = a.getConfig().ReleaseUntil
		v.RunID = a.getConfig().RunID
		v.ReleaseChangedAt = a.getConfig().ReleaseChangedAt
		if e := a.kube.saveConfig(v.Config); e != nil {
			fail(w, e)
			return
		}
		a.mu.Lock()
		a.config = v.Config
		a.mu.Unlock()
		a.emit(Event{RunID: v.RunID, From: "control", To: "generator", Kind: "action", Message: fmt.Sprintf("Konfiguracja: %d req/s, delay %d ms, errors %d%%", v.RPS, v.DelayMS, v.ErrorPercent)})
		writeJSON(w, 200, v.Config)
	})
	mux.HandleFunc("POST /api/action", func(w http.ResponseWriter, r *http.Request) {
		if e := a.authorize(r); e != nil {
			writeJSON(w, 403, Object{"error": e.Error()})
			return
		}
		var v struct {
			Action    string `json:"action"`
			Target    string `json:"target"`
			Replicas  int    `json:"replicas"`
			Count     int    `json:"count"`
			Failures  int    `json:"failures"`
			Suspended bool   `json:"suspended"`
			Schedule  string `json:"schedule"`
		}
		if e := decode(w, r, &v); e != nil {
			fail(w, e)
			return
		}
		a.actionMu.Lock()
		defer a.actionMu.Unlock()
		if v.Action != "reset" && a.experimentBusy() {
			writeJSON(w, 409, Object{"error": "Próba przygotowuje stan albo kończy pomiar; poczekaj lub przerwij eksperyment."})
			return
		}
		result := Object{"ok": true}
		var e error
		switch v.Action {
		case "scale":
			if v.Target == "echo" && a.getConfig().AutoscaleUntil > 0 {
				e = fmt.Errorf("wyłącz HPA przed ręcznym skalowaniem echo")
			} else {
				e = a.kube.scale(v.Target, v.Replicas)
			}
		case "autoscale-on", "autoscale-off":
			if a.experiments != nil && a.experiments.status()["active"] != "" {
				e = fmt.Errorf("najpierw zakończ eksperyment A/B")
			} else {
				e = a.changeAutoscaling(v.Action == "autoscale-on")
			}
		case "restart":
			e = a.kube.restartPod(v.Target)
		case "enqueue":
			result["ids"], e = a.submitTasks(v.Count, v.Failures)
		case "job":
			result["name"], e = a.kube.createJob(v.Failures != 0, a.getConfig().RunID)
		case "cron":
			e = a.kube.cron(v.Suspended, v.Schedule, a.getConfig().RunID)
		case "release-demo", "rollback-demo":
			var image string
			image, e = a.changeRelease(v.Action == "release-demo")
			result["image"] = image
		case "reset":
			if a.experiments != nil {
				view := a.experiments.view()
				if active, _ := view["active"].(string); active != "" {
					_, e = a.experimentAction("abort", active, "", Config{}, 0, 0)
					break
				}
			}
			e = a.resetLab()
		default:
			e = fmt.Errorf("nieznana akcja")
		}
		if e != nil {
			fail(w, e)
			return
		}
		a.emit(Event{RunID: a.getConfig().RunID, From: "control", To: v.Target, Kind: "action", Message: fmt.Sprintf("Zlecenie: %s; cel %s; repliki %d", v.Action, v.Target, v.Replicas)})
		if v.Action == "job" {
			a.emit(Event{RunID: a.getConfig().RunID, From: "control", To: "job", Kind: "job-submitted", Task: fmt.Sprint(result["name"]), Message: "Utworzono Job"})
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/protection", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://gateway:8080/protection", nil)
		resp, err := a.http.Do(req)
		if err != nil {
			writeJSON(w, 503, Object{"error": "gateway protection status unavailable"})
			return
		}
		defer resp.Body.Close()
		var status Object
		if err = json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&status); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, resp.StatusCode, status)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		n := len(a.events)
		c := a.config
		a.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE lab_recent_events gauge\nlab_recent_events %d\n# TYPE lab_generator_configured_rps gauge\nlab_generator_configured_rps %d\n", n, c.RPS)
	})
	return nil
}
