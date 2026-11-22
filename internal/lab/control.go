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
	_, err := a.bus.js.Subscribe("lab.events", func(m *nats.Msg) {
		var e Event
		if json.Unmarshal(m.Data, &e) != nil {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.seen[e.ID] {
			return
		}
		a.seen[e.ID] = true
		a.perf.record(e)
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
		edges := map[string]Object{}
		for _, e := range events {
			if time.Since(e.Time) > 30*time.Second {
				continue
			}
			key := e.From + "→" + e.To
			edge, ok := edges[key]
			if !ok {
				edge = Object{"from": e.From, "to": e.To, "count": 0, "errors": 0, "latency_ms": 0.0, "kind": e.Kind}
				edges[key] = edge
			}
			edge["count"] = edge["count"].(int) + 1
			if e.Status >= 400 {
				edge["errors"] = edge["errors"].(int) + 1
			}
			edge["latency_ms"] = e.Duration
		}

		if len(events) > 120 {
			events = events[len(events)-120:]
		}
		reverse := []Event{}
		for i := len(events) - 1; i >= 0; i-- {
			reverse = append(reverse, events[i])
		}
		writeJSON(w, 200, Object{"cluster": cluster, "config": config, "events": reverse, "edges": edges, "queue": a.queueState(), "metrics": a.perf.window(time.Now()), "self": Object{"role": "control", "namespace": Namespace, "discovery": "Kubernetes API (3 s)", "transport": "NATS JetStream", "retention": "events 1h / results 24h", "version": env("LAB_VERSION", "dev")}, "at": time.Now().UTC()})
	})
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
			Duration int `json:"duration_seconds"`
		}
		if e := decode(w, r, &v); e != nil {
			fail(w, e)
			return
		}
		if v.RPS > 0 {
			if v.Duration < 1 || v.Duration > 300 {
				fail(w, fmt.Errorf("duration 1–300 s"))
				return
			}
			v.RunUntil = time.Now().Add(time.Duration(v.Duration) * time.Second).Unix()
		} else {
			v.RunUntil = 0
		}
		if e := v.Config.Validate(); e != nil {
			fail(w, e)
			return
		}
		a.actionMu.Lock()
		defer a.actionMu.Unlock()
		if e := a.kube.saveConfig(v.Config); e != nil {
			fail(w, e)
			return
		}
		a.mu.Lock()
		a.config = v.Config
		a.mu.Unlock()
		a.emit(Event{From: "control", To: "generator", Kind: "action", Message: fmt.Sprintf("Konfiguracja: %d req/s, delay %d ms, errors %d%%", v.RPS, v.DelayMS, v.ErrorPercent)})
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
		result := Object{"ok": true}
		var e error
		switch v.Action {
		case "scale":
			e = a.kube.scale(v.Target, v.Replicas)
		case "restart":
			e = a.kube.restartPod(v.Target)
		case "enqueue":
			result["ids"], e = a.submitTasks(v.Count, v.Failures)
		case "job":
			result["name"], e = a.kube.createJob(v.Failures != 0)
		case "cron":
			e = a.kube.cron(v.Suspended, v.Schedule)
		case "reset":
			c := DefaultConfig()
			e = a.kube.saveConfig(c)
			if e == nil {
				a.mu.Lock()
				a.config = c
				a.mu.Unlock()
				for name, replicas := range map[string]int{"echo": 2, "gateway": 1, "validator": 1, "enricher": 1, "consumer": 1, "generator": 1, "result-store": 1} {
					if e = a.kube.scale(name, replicas); e != nil {
						break
					}
				}
			}
			if e == nil {
				e = a.kube.cron(true, "* * * * *")
			}
		default:
			e = fmt.Errorf("nieznana akcja")
		}
		if e != nil {
			fail(w, e)
			return
		}
		a.emit(Event{From: "control", To: v.Target, Kind: "action", Message: "Akcja: " + v.Action})
		writeJSON(w, 200, result)
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
