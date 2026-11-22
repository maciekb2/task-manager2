# Bounded resilience experiments

The **Odporność systemu** view contains repeatable 30 s presets and explicit live
configuration. Reset restores the base scenario. Use the A/B form for scoped
comparisons, changing one intervention at a time.

| Mechanism | Reachable problem | What to observe | Cost / limit |
|---|---|---|---|
| Gateway timeout | Worker delay exceeds deadline | Root 504, shorter wait, cancelled delay/fan-out spans | Work may have started; cancellation is not a transactional rollback |
| Backpressure | More requests than available slots | Immediate 429 while admitted requests finish | Per gateway process, no waiting queue; caller must decide retry/backoff |
| Circuit breaker | Consecutive dependency failures | Three admitted 5xx, then local 503 with fewer downstream calls; one probe after 3 s | Per process, state resets on restart/scoped epoch; rejected requests still fail |
| Parallel fan-out | Need inventory and pricing to enrich work | Two actual service calls with overlapping native spans; extra payload/CPU | Both branches required; latency follows the slower branch and either failure aborts aggregation |
| Duplicate delivery | At-least-once redelivery of a terminal task | Broker ACK floor advances, no new work or terminal KV revision | Protects this synthetic terminal result; concurrent side effects before persisting a result are not exactly-once |

`inventory` waits half the configured branch delay; `pricing` waits the full delay.
They use fixed internal targets, propagated trace/run IDs and synthetic responses.
No URL/command/image can be supplied through these controls. A breaker has a single
half-open probe; completions admitted before a trip cannot close the newer circuit.

## Independent bounded HTTP outcomes

Gateway outcomes are published to `OUTCOMES` (`lab.outcomes`), separate from spans.
Its retention is **at most** 1 h, 10,000 messages or 16 MiB, whichever comes first.
The result-store is a read facade for the last 50 retained outcomes. It does not
maintain another unique-key KV projection, so it cannot fill a redundant cache or
renew old entries on replay. Older `lab-http-results` KV data is preserved as
historical data but no longer written or required. Terminal task idempotency still
uses `lab-results`; it remains bounded to 24 h / 8 MiB.

The source can outlive a stopped result facade and is unaffected by `EVENTS`
eviction. Publishing telemetry still requires the single local broker: this is
not an audit log or an HA/durable external business transaction guarantee. If both
stream limits and offline duration are exceeded, outcomes are lost by design.

Checks: `make test`, `python3 scripts/resilience-smoke.py` and
`python3 scripts/durability-smoke.py`. The resilience proof actually stops the
facade, completes HTTP work, appends 20,001 synthetic span messages to evict the old
EVENTS window, then verifies the unchanged outcome and its restored API visibility.
