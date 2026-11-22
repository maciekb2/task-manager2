# Native CPU autoscaling

In **Odporność systemu**, use **Jedna replika + CPU** to reset to a known state and
start 180 s of synthetic compute traffic on one echo replica. Observe actual
throughput/latency and the worker's CPU; then enable HPA. The lab creates only the
namespace-local `echo` HPA, targeting Deployment echo, with 1–5 replicas and an
absolute average CPU target of **200m per pod**. This is not 200% of its request.

Kubernetes performs the decision and scaling. The dashboard shows its actual
conditions/reasons, current CPU metrics and recommendations, plus Deployment
replicas and ready pods. Its short in-browser replica history uses discovery time;
resource charts retain the Metrics API measurement timestamps. Do not treat them
as the same clock. Missing metrics/conditions remain visible, not inferred healthy.

Scaling up allows two more pods per 15 s; downscaling waits a 30 s stabilization
window and removes at most one per 15 s. Metrics scraping, pod startup, CPU limits,
single-node scheduling and long-lived connections can delay or limit the benefit.
More replicas do not imply lower latency. The CPU work knob bounds a busy loop by
elapsed time; measured container CPU is the evidence, not the configured ms value.

The server persists a 300 s demo deadline. Config edits cannot overwrite it.
Manual stop or Reset removes this HPA and restores two echo replicas. If control
restarts after expiry, recovery performs that cleanup when it returns. While the
controller is down the native HPA continues; the deadline is not an independent
Kubernetes TTL. Manual echo scaling is rejected while the demo is active. HPA is
a global demonstration: close a scoped A/B run first. Queue-driven consumer
scaling is explicitly optional and not implemented in this increment.

`python3 scripts/autoscaling-smoke.py` compares actual fully-contained HTTP
buckets before/after native scaling, confirms real ready pods and CPU conditions,
and tests recovery after a real controller restart with an already-expired
persisted deadline fixture. It does not require or promise a speedup on every
machine. The stable source is the native [Kubernetes HPA documentation](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/).
