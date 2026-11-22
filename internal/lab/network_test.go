package lab

import (
	"testing"
	"time"
)

func TestNetworkMeasuresPayloadPerHopAndExcludesLogicalQueueEvents(t *testing.T) {
	o := NewObservatory()
	now := time.Now().Truncate(5 * time.Second)
	events := []Event{
		{Time: now.Add(-9 * time.Second), From: "generator", To: "gateway", Kind: "http", TransferProtocol: "http-response", TransferBytes: byteCount(100)},
		{Time: now.Add(-7 * time.Second), From: "generator", To: "gateway", Kind: "http", TransferProtocol: "http-response", TransferBytes: byteCount(300)},
		{Time: now.Add(-6 * time.Second), From: "gateway", To: "validator", Kind: "http", TransferProtocol: "http-response", TransferBytes: byteCount(300)},
		{Time: now.Add(-6 * time.Second), From: "control", To: "nats", Kind: "queue", TransferProtocol: "nats-message", TransferBytes: byteCount(120)},
		{Time: now.Add(-6 * time.Second), From: "consumer", To: "nats", Kind: "queue", Message: "Retry/NAK, not a republished payload"},
		{Time: now.Add(-6 * time.Second), From: "echo", To: "echo", Kind: "span", Operation: "echo.work"},
		{Time: now.Add(time.Second), From: "generator", To: "gateway", Kind: "http", TransferProtocol: "http-response", TransferBytes: byteCount(9999)},       // unfinished bucket
		{Time: now.Add(-31 * time.Second), From: "generator", To: "gateway", Kind: "http", TransferProtocol: "http-response", TransferBytes: byteCount(9999)}, // outside window
	}
	// Delivery order must not change measured completion intervals.
	for i := len(events) - 1; i >= 0; i-- {
		o.record(events[i])
	}
	report := o.networkWindow(now)
	if report["bytes"] != int64(820) {
		t.Fatalf("payload total: %v", report)
	}
	rows := report["edges"].([]Object)
	if len(rows) != 3 {
		t.Fatalf("logical events leaked into transfer counts: %v", rows)
	}
	var root Object
	for _, r := range rows {
		if r["from"] == "generator" {
			root = r
		}
	}
	if root["source"] != "gateway" || root["target"] != "generator" {
		t.Fatal("HTTP response direction reversed incorrectly", root)
	}
	if root["count"] != 2 || root["mean_bytes"] != float64(200) || root["mean_interval_ms"] != float64(2000) {
		t.Fatal("incorrect sizes/cadence", root)
	}
	if root["bytes_per_second"] != float64(400)/30 {
		t.Fatal("rate must use explicit 30-second window", root)
	}
}
func TestNetworkWindowDoesNotDependOnDisplayRingAndExpires(t *testing.T) {
	o := NewObservatory()
	now := time.Now().Truncate(5 * time.Second)
	for i := 0; i < 12000; i++ {
		o.record(Event{Time: now.Add(time.Second), From: "gateway", To: "validator", Kind: "http", TransferBytes: byteCount(8192), TransferProtocol: "http-response"})
	}
	if got := o.networkWindow(now)["bytes"]; got != int64(0) {
		t.Fatal("unfinished bucket counted", got)
	}
	if got := o.networkWindow(now.Add(5 * time.Second))["bytes"]; got != int64(12000*8192) {
		t.Fatal("transfer lost under high load", got)
	}
	if got := o.networkWindow(now.Add(35 * time.Second))["bytes"]; got != int64(0) {
		t.Fatal("stopped transfer never expired", got)
	}
}
