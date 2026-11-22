// Operator-only proof helper; the application does not expose broker credentials.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	nc, err := nats.Connect("nats://127.0.0.1:18089", nats.Token(os.Getenv("LAB_PROOF_TOKEN")), nats.Timeout(3*time.Second))
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		return err
	}
	if os.Getenv("LAB_PROOF_MODE") == "delete-consumer" {
		return js.DeleteConsumer("TASKS", "workers")
	}
	if os.Getenv("LAB_PROOF_MODE") == "entry" {
		var event struct {
			ID   string    `json:"id"`
			Time time.Time `json:"time"`
		}
		if err = json.Unmarshal([]byte(os.Getenv("LAB_PROOF_EVENT")), &event); err != nil {
			return err
		}
		kv, err := js.KeyValue("lab-http-results")
		if err != nil {
			return err
		}
		entry, err := kv.Get(fmt.Sprintf("%020d-%s", event.Time.UnixNano(), event.ID))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(entry.Value())
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"revision": entry.Revision(), "created": entry.Created(), "sha256": hex.EncodeToString(hash[:])})
	}
	kv, err := js.KeyValue("lab-results")
	if err != nil {
		return err
	}
	id := os.Getenv("LAB_PROOF_TASK")
	before, err := kv.Get(id)
	if err != nil {
		return err
	}
	var task struct {
		State string `json:"state"`
	}
	if err = json.Unmarshal(before.Value(), &task); err != nil {
		return err
	}
	if task.State != "completed" && task.State != "dead-letter" {
		return fmt.Errorf("proof requires a terminal synthetic task")
	}
	streamBefore, err := js.StreamInfo("EVENTS")
	if err != nil {
		return err
	}
	ack, err := js.Publish("tasks.queue", before.Value(), nats.MsgId(fmt.Sprintf("proof-%d", time.Now().UnixNano())))
	if err != nil {
		return err
	}
	if ack.Duplicate {
		return fmt.Errorf("broker deduplicated publication instead of delivering it")
	}
	end := time.Now().Add(10 * time.Second)
	acknowledged := false
	for time.Now().Before(end) {
		info, err := js.ConsumerInfo("TASKS", "workers")
		if err != nil {
			return err
		}
		if info.AckFloor.Stream >= ack.Sequence && info.NumAckPending == 0 {
			acknowledged = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !acknowledged {
		return fmt.Errorf("duplicate was not actually acknowledged")
	}
	after, err := kv.Get(id)
	if err != nil {
		return err
	}
	if after.Revision() != before.Revision() || !bytes.Equal(after.Value(), before.Value()) {
		return fmt.Errorf("duplicate changed terminal result")
	}
	streamAfter, err := js.StreamInfo("EVENTS")
	if err != nil {
		return err
	}
	if streamAfter.State.LastSeq != streamBefore.State.LastSeq {
		return fmt.Errorf("duplicate unexpectedly executed application work; stop unrelated traffic before proof")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"acknowledged_sequence": ack.Sequence, "terminal_result_revision": after.Revision(), "application_events_unchanged": true})
}
