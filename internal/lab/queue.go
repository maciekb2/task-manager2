package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"net/http"
	"sort"
	"time"
)

func (a *App) consume(ctx context.Context) {
	setReady := func(ready bool) { a.mu.Lock(); a.consumerReady = ready; a.mu.Unlock() }
	defer setReady(false)
	for ctx.Err() == nil {
		_, err := a.bus.js.AddConsumer("TASKS", &nats.ConsumerConfig{Durable: "workers", FilterSubject: "tasks.queue", AckPolicy: nats.AckExplicitPolicy, AckWait: 20 * time.Second, MaxAckPending: 50})
		var sub *nats.Subscription
		if err == nil {
			sub, err = a.bus.js.PullSubscribe("tasks.queue", "workers", nats.Bind("TASKS", "workers"))
		}
		if err != nil {
			fmt.Println("consumer subscribe", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		setReady(true)
		for ctx.Err() == nil {
			messages, err := sub.Fetch(1, nats.MaxWait(time.Second))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					continue
				}
				fmt.Println("consumer fetch; rebinding", err)
				break
			}
			for _, msg := range messages {
				a.processMessage(ctx, msg)
			}
		}
		setReady(false)
		_ = sub.Unsubscribe()
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
	ctx = runContext(otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": t.TraceParent}), t.RunID)
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
	a.emit(Event{RunID: t.RunID, Trace: t.Trace, From: "nats", To: "consumer", Kind: "queue", Task: t.ID, Attempt: t.Attempt, TransferBytes: byteCount(len(msg.Data)), TransferProtocol: "nats-message", Message: "Odebrano zadanie"})
	status, checkErr := a.callGateway(ctx, "consumer", t.Trace, t.Failures < 0 || int(t.Attempt) <= t.Failures)
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
			a.emit(Event{RunID: t.RunID, Trace: t.Trace, From: "consumer", To: "dead-letter", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, TransferBytes: byteCount(len(raw)), TransferProtocol: "nats-message", Message: "Limit 3 prób: zadanie w DLQ"})
			_ = msg.AckSync()
		} else {
			t.State = "retry"
			if a.bus.putTask(t) != nil {
				_ = msg.NakWithDelay(2 * time.Second)
				return
			}
			a.emit(Event{RunID: t.RunID, Trace: t.Trace, From: "consumer", To: "nats", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, Message: "Ponowienie za 2 s"})
			_ = msg.NakWithDelay(2 * time.Second)
		}
	} else {
		t.State = "completed"
		if a.bus.putTask(t) != nil {
			_ = msg.NakWithDelay(2 * time.Second)
			return
		}
		a.emit(Event{RunID: t.RunID, Trace: t.Trace, From: "consumer", To: "result-store", Kind: "queue", Task: t.ID, Attempt: t.Attempt, Status: status, Message: "Wynik utrwalony; ACK"})
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
		result["dlq_observed"] = true
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
		t := Task{RunID: a.getConfig().RunID, ID: fmt.Sprintf("%013d-%s", time.Now().UnixMilli(), id()[:8]), Trace: id() + id()[:8], State: "queued", Failures: failures, Created: time.Now().UTC()}
		ctx, finish := a.operation(runContext(traceContext(context.Background(), t.Trace), t.RunID), "task.enqueue")
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
		a.emit(Event{RunID: t.RunID, Trace: t.Trace, From: "control", To: "nats", Kind: "queue", Task: t.ID, TransferBytes: byteCount(len(raw)), TransferProtocol: "nats-message", Message: "Zadanie opublikowane"})
		ids = append(ids, t.ID)
	}
	return ids, nil
}

// The outcome source is the bounded broker stream itself. Avoid a second KV
// projection with unique keys: it used to fill and could replay only span retention.
func (a *App) startResultStore(mux *http.ServeMux) error {
	mux.HandleFunc("GET /results", func(w http.ResponseWriter, r *http.Request) {
		stream, err := a.bus.js.StreamInfo("OUTCOMES", nats.Context(r.Context()))
		if err != nil {
			writeJSON(w, 503, Object{"error": "outcome stream unavailable"})
			return
		}
		result := []Event{}
		for seq := stream.State.LastSeq; seq >= stream.State.FirstSeq && seq > 0 && len(result) < 50; seq-- {
			msg, err := a.bus.js.GetMsg("OUTCOMES", seq, nats.Context(r.Context()))
			if err != nil {
				if errors.Is(err, nats.ErrMsgNotFound) {
					continue
				}
				writeJSON(w, 503, Object{"error": "outcome read incomplete"})
				return
			}
			var e Event
			if json.Unmarshal(msg.Data, &e) == nil && time.Since(e.Time) <= time.Hour {
				result = append(result, e)
			}
		}
		writeJSON(w, 200, result)
	})
	return nil
}
