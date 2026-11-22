# Systems Lab — a technical portfolio walkthrough

Audience: technical interviewer, engineer or team lead. Target: 5–10 minutes of
presentation after the supported environment has bootstrapped. Cold downloads are
not part of that presentation-time promise. Use synthetic data only.

## What this demonstrates

Go HTTP/concurrency, separate Kubernetes process lifecycles, bounded experiment
controls, JetStream durability/retry/DLQ, and evidence from Kubernetes and native
OpenTelemetry traces. Roles share code/image deliberately; this keeps a local lab
reproducible. A single node is not evidence of HA or production capacity.

## 1. Find the bottleneck

1. Start normal traffic; show request rate, HTTP chain and one native trace.
2. Choose the slow-service scenario. Explain configured versus completed rate and
   how concurrency/latency limit throughput. Inspect `echo.delay` in the waterfall.
3. Compare CPU work: the wait span and CPU span both take wall time but consume
   resources differently. CPU samples have their own timestamps/windows.
4. Return to normal conditions. Show the changed request times; do not attribute
   one pod CPU sample to a single request or add inclusive span durations.
5. In Network, compare payload sizes. Show the four response hops and explain why
   their sum counts the same response four times.

## 2. Keep work through failures

1. Stop the consumer and enqueue three tasks. Show broker backlog independently
   of the displayed task list.
2. Resume; submit a task with one forced failure. Open its trace: the first actual
   HTTP chain returns 503, the second succeeds after retry. Show the persisted
   terminal result and acknowledgement ordering.
3. Submit a permanently failing task and show DLQ after three application attempts.
4. Explain at-least-once delivery, task-ID result deduplication, and why this is not
   a transaction around arbitrary external side effects.

## 3. Restore service and roll back a version

1. Delete one HTTP-worker pod from its inspector. Show the old identity, the
   replacement, and the return to desired readiness. Persistent data does not
   automatically follow an arbitrary stateless pod.
2. Run the documented version-change/rollback acceptance once available. Show
   immutable source image identities, real replacement pods, traffic through them,
   and recovery of the original image. A successful rollout command alone is not
   the acceptance criterion.
3. Reset the lab. Show stopped traffic, baseline replicas and suspended CronJob.

## Evidence to show

- `docs/architecture.md`: control/discovery and data-plane boundaries.
- `docs/verification.md`: versions, actual checks and remaining limitations.
- `docs/roadmap.md`: accepted goal and work still outstanding.
- PR/build link: distinguish hosted automation from local runtime receipts.

The version-change demonstration and recorded presentation are tracked deliverables
in roadmap stage 2; this document does not claim they have already passed.
