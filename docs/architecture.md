# Architecture and learning contracts

## Discovery and control

The observer queries Kubernetes every three seconds and returns a projection of
the lab's resources. It reads nodes cluster-wide; workloads, pod metrics and events
are limited to `systems-lab`. It does not return arbitrary container environments,
ConfigMap contents or Secret values. Services/selectors and live pod readiness are
read from the API rather than manufactured by the interface. The control service
caches observer snapshots and the UI flags stale discovery timestamps.

The controller has separate RBAC: the one experiment ConfigMap, named demo
deployment scale subresources, pod reads/logs/deletion in the lab namespace, Job
creation the named CronJob and the bounded echo HPA. Its HTTP interface further restricts pod deletion
to the managed demo components. Kubernetes RBAC cannot select pods by label, so
the application restriction complements the namespace boundary. No component has
cluster-admin, host mounts, host networking, a Docker socket or Secret-read RBAC.

The shared Go image starts one role per Deployment. Code shares transport, event
schema and discovery helpers; scaling/failure/process lifecycles are independent.
The web service has no service-account token. Workload services use the lab broker
credential; only control/observer mount a Kubernetes service-account credential.
This local demo shares a NATS account; it is not a security-isolated tenant model.

## HTTP and live metrics

`generator → gateway → validator → enricher → echo` is actual HTTP. The gateway
fixes its target to validator, which fixes its target to enricher, which fixes its
target to echo. No caller-supplied URL is fetched. Requests have a 32-character
correlation identifier, independently timed hops and observed destination pod IDs.
Parameters are read from the controller's persisted ConfigMap; workers refresh their cached configuration asynchronously; the generator refreshes
with a 250 ms interval and other data-plane roles with a one-second interval when
control is reachable. New pods load configuration before listening. Scoped requests require a matching run epoch (otherwise a bounded refresh or explicit 409); ordinary cached work remains independent of control availability. Failed background refreshes back off and never block request pacing. If control is
unavailable they retain their last configuration. Generator deadlines remain
absolute and bounded even during a controller interruption.

Readiness is based on the application HTTP endpoint. The observer also exposes
Metrics Server pod CPU/RAM data; the interface's traffic/latency/error numbers are
derived from real NATS-carried application events over a 30-second window.
They are samples of completed requests, not configured target rates. A small
`/metrics` endpoint on control exports retained-event count and configured rate.
Neither discovery annotations nor dependency links prove traffic occurred.

## JetStream

* `TASKS` uses work-queue retention and one shared durable pull consumer `workers`.
* Multiple consumer pods compete for work. Pending and unacknowledged counts come
  from JetStream consumer state, not the UI task list.
* A task's failure scenario explicitly controls its first 0/1/2 attempts or all
  attempts. After the third application failure it is published to `DLQ`.
* State is persisted in KV before acknowledgement. A completed/dead-letter task ID
  is recognized on redelivery. Broker publication uses the task ID as its message
  deduplication ID. This is at-least-once delivery with terminal-result deduplication;
  it does not make arbitrary external side effects exactly-once or transactional.
* HTTP errors are outcomes, not random synthetic task success. For queued checks,
  a non-successful HTTP result triggers retry and ultimately DLQ.
* `EVENTS` retains correlated hop/action events. Control replays them after restart;
  its in-memory recent ring is bounded to 10000 display events; traffic metrics and
  topology windows are aggregated independently. Topology uses 30 closed seconds
  and a current-activity timestamp, excluding function spans from network edges.
  Gateway outcomes use the separate bounded OUTCOMES stream, read by result-store.
  This avoids span-pressure eviction and a redundant unique-key KV projection.
  See [resilience contracts](resilience.md) for actual bounds and guarantees.

## Kubernetes Job/CronJob

A fixed Job template calls the same gateway once and exits. A deliberate process
failure demonstrates native Job backoff (`backoffLimit: 2`, 90-second deadline).
The CronJob starts suspended, uses `concurrencyPolicy: Forbid`, a one-/five-minute
schedule, small history limits and ten-minute cleanup for finished Jobs. Panel
inputs cannot replace the pod template, image, command or service account.

## Reproducibility and scope

`scripts/lab.sh` uses only its project kubeconfig and named Minikube profile.
`deploy/render.py` produces Kubernetes JSON using only Python's standard library.
The lab image tag derives from its source inputs; another `make deploy` builds a
new image when they change. Unchanged images are reused. Bootstrap creates broker
credentials/configuration only if absent and reconciles declarative resources. A
stable Secret digest rolls the broker and its clients after credential recreation.
Control reconciles stream and task-KV retention bounds on startup.

Minikube/Kubernetes/NATS/Go versions are pinned in code. This is a local development
release path with hosted GitHub Actions checks that create the isolated lab and
exercise real runtime and rollback paths. Results are revision-specific in
[verification](verification.md); there is no production readiness claim. Full production
auth, public ingress, HA, offsite recovery, packet-level network discovery are outside the local lab. OpenTelemetry/Jaeger
are installed locally by the same bootstrap; their storage is intentionally bounded.

References: [Minikube start](https://minikube.sigs.k8s.io/docs/commands/start/),
[Kubernetes CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/),
[NATS JetStream](https://docs.nats.io/learn/).


## Learning and performance release

The Polish learning UI verifies fresh request IDs and live resource postconditions.
It does not grant completion based on a timer. The free-play controls are hidden
while learning. See [the Polish walkthrough](learning-observability.md).

The observer now reads node resource samples through `metrics.k8s.io` as well as
pod samples. No host agent, host mount, `/proc` scraper or workstation metrics are
added. Samples retain their API timestamps and measurement windows. Freshness is
checked against those windows; missing data remains missing. The control service
keeps up to 15 minutes of resource snapshots in memory. CPU/RAM belong to pods or
the Kubernetes node, not individual requests.

Requests are instrumented with OpenTelemetry SDK, W3C trace propagation and
OTLP/HTTP export to a namespace-local Jaeger. Named spans cover HTTP stages,
validation, enrichment, delay, CPU work, queue publication and processing attempts.
The task carrier preserves trace context through retry. Inclusive/nested span
durations must not be summed. The UI queries actual Jaeger spans for waterfalls;
`/tracing/` exposes its native UI through the existing loopback web proxy.

Jaeger is configured for in-memory retention of up to 5000 traces and loses them
on restart. It is not a persistent production tracing platform. Metrics/API history
resets with control; task and HTTP outcome persistence remains in JetStream.

Traffic and p95 use closed, aligned 30-second windows (at most five seconds behind
now), independent of the display event ring. P95 uses empirical nearest rank.
Functions aggregate measured wall time; they are selected instrumented operations,
not a statistical profiler of arbitrary Go functions.

NATS and Jaeger configuration hashes are part of their pod templates, so a changed
configuration triggers a real rollout on reconciliation. A single generator replica
maintains the global configured traffic cap; worker/consumer replicas remain
independently scalable. HTTP outcome keys are create-only on replay, and retrieval
filters by original event age in addition to the KV retention budget.

## Correlation, resilience and automatic response

[Scoped experiments](experiments.md) snapshot each phase and seal observed facts
before resetting resources. Only fully contained CPU measurement windows qualify;
short phases can have no comparable resource sample. OTel spans carry run IDs.

[Resilience](resilience.md) adds per-process gateway admission and circuit state,
HTTP cancellation and two fixed internal fan-out branches. [Native HPA](autoscaling.md)
acts on actual echo CPU with bounded pod limits and persisted cleanup intent.
Neither control loop monitors or scales the host.
