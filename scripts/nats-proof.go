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
	if os.Getenv("LAB_PROOF_MODE") == "outcome-isolation" {
		trace := os.Getenv("LAB_PROOF_TRACE")
		stream, err := js.StreamInfo("OUTCOMES")
		if err != nil {
			return err
		}
		var retained *nats.RawStreamMsg
		for seq := stream.State.LastSeq; seq >= stream.State.FirstSeq && seq > 0; seq-- {
			msg, err := js.GetMsg("OUTCOMES", seq)
			if err != nil {
				return err
			}
			var e struct {
				Trace string `json:"trace"`
			}
			_ = json.Unmarshal(msg.Data, &e)
			if e.Trace == trace {
				retained = msg
				break
			}
		}
		if retained == nil {
			return fmt.Errorf("outcome not found before eviction proof")
		}
		events, err := js.StreamInfo("EVENTS")
		if err != nil {
			return err
		}
		oldLast := events.State.LastSeq
		// Append actual synthetic span messages through the broker; no purge or config change.
		raw, _ := json.Marshal(map[string]any{"id": "retention-proof", "kind": "span", "from": "proof", "to": "proof", "time": time.Now()})
		for range 20001 {
			if _, err = js.Publish("lab.events", raw); err != nil {
				return err
			}
		}
		events, err = js.StreamInfo("EVENTS")
		if err != nil {
			return err
		}
		if events.State.FirstSeq <= oldLast {
			return fmt.Errorf("did not evict prior event window")
		}
		after, err := js.GetMsg("OUTCOMES", retained.Sequence)
		if err != nil {
			return err
		}
		if !bytes.Equal(after.Data, retained.Data) || !after.Time.Equal(retained.Time) {
			return fmt.Errorf("independent outcome changed")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"original_events_evicted": true, "outcome_sequence": retained.Sequence, "outcome_unchanged": true})
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
		stream, err := js.StreamInfo("OUTCOMES")
		if err != nil {
			return err
		}
		for seq := stream.State.LastSeq; seq >= stream.State.FirstSeq && seq > 0; seq-- {
			msg, err := js.GetMsg("OUTCOMES", seq)
			if err != nil {
				return err
			}
			var candidate struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(msg.Data, &candidate); err != nil {
				return err
			}
			if candidate.ID == event.ID {
				hash := sha256.Sum256(msg.Data)
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"sequence": seq, "created": msg.Time, "sha256": hex.EncodeToString(hash[:])})
			}
		}
		return fmt.Errorf("outcome no longer retained")
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
