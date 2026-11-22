package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"net/http"
	"sort"
	"time"
)

func (a *App) consume(ctx context.Context) {
	sub, err := a.bus.js.PullSubscribe("tasks.queue", "workers", nats.Bind("TASKS", "workers"))
	if err != nil {
		fmt.Println("consumer subscribe", err)
		return
	}
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		messages, err := sub.Fetch(1, nats.MaxWait(time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				fmt.Println("fetch", err)
			}
			continue
		}
		for _, msg := range messages {
			a.processMessage(ctx, msg)
		}
	}
}
func (a *App) processMessage(ctx context.Context, msg *nats.Msg) {
	var t Task
	if json.Unmarshal(msg.Data, &t) != nil {
		_ = msg.Term()
		return
	}
	existing, e := a.bus.getTask(t.ID)
	if e == nil && (existing.State == "completed" || existing.State == "dead-letter") {
		_ = msg.AckSync()
		return
	}
	meta, e := msg.Metadata()
	if e != nil {
		_ = msg.Nak()
		return
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": t.TraceParent})
	ctx, finish := a.operation(traceContext(ctx, t.Trace), "consumer.process")
	defer func() { finish(t.Status) }()
	t.FirstStarted = existing.FirstStarted
	if t.FirstStarted.IsZero() {
		t.FirstStarted = time.Now().UTC()
	}
	t.QueueMS = float64(t.FirstStarted.Sub(t.Created).Microseconds()) / 1000
	t.Attempt = meta.NumDelivered
	t.State = "processing"
	if a.bus.putTask(t) != nil {
		_ = msg.NakWithDelay(time.Second)
		return
	}
	a.emit(Event{Trace: t.Trace, From: "nats", To: "consumer", Kind: "queue", Task: t.ID, Attempt: t.Attempt, TransferBytes: byteCount(len(msg.Data)), TransferProtocol: "nats-message", Message: "Odebrano zadanie"})
	status := 503
	var checkErr error
	if t.Failures >= 0 && int(t.Attempt) > t.Failures {
		status, checkErr = a.callGateway(ctx, "consumer", t.Trace)
	}
	t.Status = status
	if checkErr != nil || status >= 400 {
		if t.Attempt >= 3 {
			t.State = "dead-letter"
			raw, _ := json.Marshal(t)
			if _, e = a.bus.js.Publish("tasks.dlq", raw, nats.MsgId(t.ID)); e != nil {
				_ = msg.NakWithDelay(2 * time.Second)
				return
			}
			if e = a.bus.putTask(t); e != nil {
				_ = msg.NakWithDelay(2 * time.Second)
				return
			}
			a.emit(Event{Trace: t.Trace, From: "consumer", To: "dead-letter", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, TransferBytes: byteCount(len(raw)), TransferProtocol: "nats-message", Message: "Limit 3 prób: zadanie w DLQ"})
			_ = msg.AckSync()
		} else {
			t.State = "retry"
			if a.bus.putTask(t) != nil {
				_ = msg.NakWithDelay(2 * time.Second)
				return
			}
			a.emit(Event{Trace: t.Trace, From: "consumer", To: "nats", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, Message: "Ponowienie za 2 s"})
			_ = msg.NakWithDelay(2 * time.Second)
		}
	} else {
		t.State = "completed"
		if a.bus.putTask(t) != nil {
			_ = msg.NakWithDelay(2 * time.Second)
			return
		}
		a.emit(Event{Trace: t.Trace, From: "consumer", To: "result-store", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, Message: "Wynik utrwalony; ACK"})
		_ = msg.AckSync()
	}
}

func (a *App) queueState() Object {
	result := Object{"pending": 0, "in_flight": 0, "redelivered": 0, "dead_letters": 0, "tasks": []Task{}}
	if c, e := a.bus.js.ConsumerInfo("TASKS", "workers"); e == nil {
		result["pending"] = c.NumPending
		result["in_flight"] = c.NumAckPending
		result["redelivered"] = c.NumRedelivered
	} else {
		result["error"] = e.Error()
	}
	if s, e := a.bus.js.StreamInfo("DLQ"); e == nil {
		result["dead_letters"] = s.State.Msgs
	}
	if keys, e := a.bus.tasks.Keys(); e == nil {
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		if len(keys) > 60 {
			keys = keys[:60]
		}
		tasks := []Task{}
		for _, key := range keys {
			if t, e := a.bus.getTask(key); e == nil {
				if (t.State == "queued" || t.State == "retry" || t.State == "processing") && time.Since(t.Created) > time.Hour {
					t.State = "expired"
				}
				tasks = append(tasks, t)
			}
		}
		result["tasks"] = tasks
	}
	return result
}
func (a *App) submitTasks(count, failures int) ([]string, error) {
	if count < 1 || count > 50 || failures < -1 || failures > 2 {
		return nil, fmt.Errorf("liczba 1–50, failures -1 (zawsze) lub 0–2")
	}
	ids := []string{}
	for i := 0; i < count; i++ {
		t := Task{ID: fmt.Sprintf("%013d-%s", time.Now().UnixMilli(), id()[:8]), Trace: id() + id()[:8], State: "queued", Failures: failures, Created: time.Now().UTC()}
		ctx, finish := a.operation(traceContext(context.Background(), t.Trace), "task.enqueue")
		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		t.TraceParent = carrier.Get("traceparent")
		if e := a.bus.putTask(t); e != nil {
			finish(503)
			return ids, e
		}
		raw, _ := json.Marshal(t)
		if _, e := a.bus.js.Publish("tasks.queue", raw, nats.MsgId(t.ID)); e != nil {
			t.State = "publish-failed"
			_ = a.bus.putTask(t)
			finish(503)
			return ids, e
		}
		finish(200)
		a.emit(Event{Trace: t.Trace, From: "control", To: "nats", Kind: "queue", Task: t.ID, TransferBytes: byteCount(len(raw)), TransferProtocol: "nats-message", Message: "Zadanie opublikowane"})
		ids = append(ids, t.ID)
	}
	return ids, nil
}
func (a *App) startResultStore(mux *http.ServeMux) error {
	kv, err := a.bus.js.KeyValue("lab-http-results")
	if err != nil {
		kv, err = a.bus.js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "lab-http-results", TTL: time.Hour, Storage: nats.FileStorage, MaxBytes: 8 << 20})
	}
	if err != nil {
		return err
	}
	_, err = a.bus.js.Subscribe("lab.events", func(m *nats.Msg) {
		var e Event
		if json.Unmarshal(m.Data, &e) != nil || e.From != "gateway" || e.Kind != "http" {
			return
		}
		raw, _ := json.Marshal(e)
		if _, err := kv.Put(fmt.Sprintf("%020d-%s", e.Time.UnixNano(), e.ID), raw); err != nil {
			fmt.Println("result-store:", err)
		}
	}, nats.OrderedConsumer(), nats.DeliverAll())
	if err != nil {
		return err
	}
	mux.HandleFunc("GET /results", func(w http.ResponseWriter, r *http.Request) {
		items := []Event{}
		if keys, e := kv.Keys(); e == nil {
			sort.Sort(sort.Reverse(sort.StringSlice(keys)))
			recent := []string{}
			for _, key := range keys {
				if len(key) > 20 && key[20] == '-' {
					recent = append(recent, key)
				}
			}
			if len(recent) > 50 {
				recent = recent[:50]
			}
			for _, key := range recent {
				entry, e := kv.Get(key)
				if e == nil {
					var event Event
					if json.Unmarshal(entry.Value(), &event) == nil {
						items = append(items, event)
					}
				}
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
		if len(items) > 50 {
			items = items[:50]
		}
		writeJSON(w, 200, items)
	})
	return nil
}
