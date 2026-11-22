package lab

import (
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"time"
)

type Bus struct {
	nc    *nats.Conn
	js    nats.JetStreamContext
	tasks nats.KeyValue
}

func connectBus() (*Bus, error) {
	var nc *nats.Conn
	var err error
	for i := 0; i < 60; i++ {
		nc, err = nats.Connect(env("NATS_URL", "nats://nats:4222"), nats.Token(osToken()), nats.Timeout(2*time.Second), nats.MaxReconnects(-1))
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	js, err := nc.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		return nil, err
	}
	return &Bus{nc: nc, js: js}, nil
}
func (b *Bus) initialize() error {
	configs := []*nats.StreamConfig{
		{Name: "OUTCOMES", Subjects: []string{"lab.outcomes"}, Storage: nats.FileStorage, MaxAge: time.Hour, MaxMsgs: 10000, MaxBytes: 16 << 20},
		{Name: "EVENTS", Subjects: []string{"lab.events"}, Storage: nats.FileStorage, MaxAge: time.Hour, MaxMsgs: 20000, MaxBytes: 32 << 20},
		{Name: "TASKS", Subjects: []string{"tasks.queue"}, Storage: nats.FileStorage, Retention: nats.WorkQueuePolicy, MaxAge: time.Hour, MaxMsgs: 10000, MaxBytes: 16 << 20, Discard: nats.DiscardNew},
		{Name: "DLQ", Subjects: []string{"tasks.dlq"}, Storage: nats.FileStorage, MaxAge: 24 * time.Hour, MaxMsgs: 1000, MaxBytes: 4 << 20},
	}
	for _, c := range configs {
		info, err := b.js.StreamInfo(c.Name)
		if err != nil {
			if _, err = b.js.AddStream(c); err != nil {
				return fmt.Errorf("stream %s: %w", c.Name, err)
			}
			continue
		}
		next := info.Config
		if next.MaxAge != c.MaxAge || next.MaxMsgs != c.MaxMsgs || next.MaxBytes != c.MaxBytes || next.Discard != c.Discard {
			next.MaxAge = c.MaxAge
			next.MaxMsgs = c.MaxMsgs
			next.MaxBytes = c.MaxBytes
			next.Discard = c.Discard
			if _, err = b.js.UpdateStream(&next); err != nil {
				return fmt.Errorf("reconcile stream %s: %w", c.Name, err)
			}
		}
	}
	_, err := b.js.AddConsumer("TASKS", &nats.ConsumerConfig{Durable: "workers", FilterSubject: "tasks.queue", AckPolicy: nats.AckExplicitPolicy, AckWait: 20 * time.Second, MaxAckPending: 50})
	if err != nil {
		return err
	}
	kv, err := b.js.KeyValue("lab-results")
	if err != nil {
		kv, err = b.js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "lab-results", Storage: nats.FileStorage, TTL: 24 * time.Hour, MaxBytes: 8 << 20, History: 1})
	}
	if err != nil {
		return err
	}
	info, err := b.js.StreamInfo("KV_lab-results")
	if err != nil {
		return err
	}
	next := info.Config
	if next.MaxAge != 24*time.Hour || next.MaxBytes != 8<<20 || next.MaxMsgsPerSubject != 1 {
		next.MaxAge = 24 * time.Hour
		next.MaxBytes = 8 << 20
		next.MaxMsgsPerSubject = 1
		if _, err = b.js.UpdateStream(&next); err != nil {
			return fmt.Errorf("reconcile task KV: %w", err)
		}
	}
	b.tasks = kv
	return nil
}
func (b *Bus) readyTasks() error {
	var err error
	for i := 0; i < 60; i++ {
		b.tasks, err = b.js.KeyValue("lab-results")
		if err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}
func (b *Bus) emit(e Event) error {
	raw, _ := json.Marshal(e)
	_, err := b.js.Publish("lab.events", raw)
	if e.From == "gateway" && e.Kind == "http" {
		_, outcomeErr := b.js.Publish("lab.outcomes", raw, nats.MsgId(e.ID))
		if err == nil {
			err = outcomeErr
		}
	}
	return err
}
func (b *Bus) putTask(t Task) error {
	t.Updated = time.Now().UTC()
	if t.State == "completed" || t.State == "dead-letter" {
		t.Finished = t.Updated
		t.TotalMS = float64(t.Finished.Sub(t.Created).Microseconds()) / 1000
	}
	raw, _ := json.Marshal(t)
	_, err := b.tasks.Put(t.ID, raw)
	return err
}
func (b *Bus) getTask(key string) (Task, error) {
	var t Task
	e, err := b.tasks.Get(key)
	if err == nil {
		err = json.Unmarshal(e.Value(), &t)
	}
	return t, err
}
