# Local acceptance — 2026-11-22

Application source version: `cde6fe2866361d8d` (computed from image/manifests inputs
by `scripts/source-version.py`). Local image ID:
`sha256:9c8c3ae0315bebe90427f8acd8abb6253269a4dbf727f2c776b8201efdc50715`.

Environment: dedicated Minikube profile `systems-lab`, Kubernetes `v1.37.2`,
containerd, 4 CPU / 6144 MiB RAM. Eleven deployments; twelve normal application/
broker pods with two HTTP workers, plus transient Job pods. Metrics Server data
and Kubernetes API discovery were available. The operator's pre-existing current
context was preserved; all lab mutations used the project kubeconfig.

## Accepted behavior

1. Discovery returns actual deployments, nodes and services without API errors.
2. Unauthenticated mutations and infrastructure/foreign-namespace actions are
   rejected. Observer Secret reads and controller mutations in `default` are denied.
3. Real successful HTTP travels through generator, gateway, validator, enricher,
   and the HTTP worker.
4. One request correlates across four measured hops; the result-store persists
   its outcome; a bounded generator run reaches its configured deadline.
5. Configured HTTP 503 propagates through the chain.
6. Scaling creates three genuinely ready HTTP worker pods.
7. Deleting one demo pod results in a replacement from its Deployment.
8. Scaling the consumer to zero actually stops all consumer pods.
9. Enqueued messages remain pending while no consumer exists.
10. Resuming the consumer completes three queued tasks after one failure/two attempts.
11. A permanently failing task reaches DLQ after its third attempt.
12. A successful Kubernetes Job completes.
13. A deliberately failing Job retries and reaches a terminal failure.
14. Enabling the CronJob actually creates and completes a new scheduled Job.
15. Reset restores stopped traffic, two HTTP workers and a suspended CronJob.

Go unit/race checks passed for resource/time bounds, origin/token checks and
forbidden scale targets. JavaScript/Python/shell syntax and whitespace checks
passed. Gitleaks scanning of staged source found no secrets. Visual inspection
covered the desktop panel and its live topology/queue/Job views.

`make stop` followed by `make up` also passed: all eleven deployments recovered
with the same source image; four task records, the DLQ message and HTTP outcomes
survived on the local PVC. Bootstrap reused existing resources/credentials and
restored the localhost panel without changing the operator's active context.

The first cluster acceptance run stopped in the test fixture when a running Job
returned `conditions: null`. The fixture was corrected to handle that valid
Kubernetes response. Already successful HTTP/queue checks were retained; the
existing failed Job and CronJob checks were completed in a targeted continuation.
This is not a claim of an uninterrupted first-pass test run.

Local receipts/logs/screenshots are under ignored `.state/` and `output/playwright/`.
They are not application dependencies and are not committed. This record covers a
local development release, not hosted CI/CD, production, offsite recovery or HA.

## Learning and observability extension — 2026-11-22

Final deployed source version: `6cf14461eac7315b`. Twelve deployments (Jaeger is
the additional component), thirteen normal pods with two HTTP workers.

* Nine guided lessons completed through the live browser UI, each waiting for its
  actual postcondition: HTTP correlation, resource discovery, pod replacement,
  three ready replicas, backlog without consumers, retry success, Job, a genuinely
  scheduled CronJob, and CPU spans plus fresh Kubernetes metrics.
* Six focused API/runtime checks passed for fresh HTTP, native Jaeger spans across
  five services, real function timings, pod/node CPU/RAM history, queue timing and
  producer/consumer trace propagation, and the same-origin native Jaeger UI.
* Final-source checks confirmed successful and deliberately failed native Jobs
  expose `job.execute` spans, correct outcomes and Kubernetes start timestamps.
  The Jaeger JSON API translates the HTTP status attribute to `http.status_code`;
  the probe was corrected to use that actual API representation.
* The native waterfall was opened in the live panel. Desktop learning/dashboard
  and trace screenshots are retained under ignored `output/playwright/`.
* Unit/race checks cover empirical p95 at small sample counts, the full advertised
  traffic window independently of the display ring, Kubernetes quantity units,
  missing resource samples, and the previously accepted control boundaries.
* Observer Secret access was checked in the **lab namespace**, not merely default.
  The observer can read node metrics through the Kubernetes API. No host metrics
  agent, host filesystem mount or direct workstation performance probe was added.

The focused OTel checks ran at source `e907b7b96f3a68b5`; all nine lessons ran at
`8288f4b15dc8d828`. Final `6cf14461eac7315b` adds native Job outcome spans and their
timing table; those changes received targeted runtime/UI checks. Unchanged passing
HTTP/queue/lesson behavior was reused rather than rerunning the full old suite.

Display metrics now use independent closed 30-second buckets and empirical
nearest-rank p95. HTTP result retrieval caps time-sortable keys before fetching
values; expired queue records are presented as expired. Smoke assertions require
fresh observations from their current run.

CPU/RAM are resource samples with API timestamps/windows, not per-span CPU bills.
Named spans measure selected functions' inclusive wall time. Control chart history
is held for at most 15 minutes; Jaeger holds at most 5000 traces in volatile memory.

## Navigation and live rendering refinement — 2026-11-22

A single labeled navigation replaces the duplicated rail and horizontal tabs.
Section URLs support browser back/forward. On small screens the same menu becomes
horizontally scrollable at the top; the experiment controls follow the content.
The topology scrolls within its panel rather than shrinking service labels.

The panel polls every 3 seconds and performance history every 5 seconds while
visible. Returning to the page refreshes state immediately. SVG elements are
updated in place, preserving node identity, selection, keyboard focus and animation
phase. Unchanged table markup is retained; focused interactive lists defer content
replacement until focus leaves, and preserve their scroll offset. Inspector replica
drafts survive polling until the selected service or actual replica count changes.

Connection counts cover the available events in the last 30 seconds; moving dashed
lines indicate events less than 5 seconds old. A lightweight local timer stops
animation after activity expires, independently of polling/network failure. Plain
dashed links remain declared dependencies. Reduced-motion preferences disable
animation. CPU/RAM measurement scope remains Kubernetes only.

Accepted deployed source: `ade3ef24f00e5216`. A live browser run covered the single
menu and all six sections, history back/forward, SVG/node identity across polls,
keyboard focus, unsaved replica input, observed HTTP activity and its animation
expiry, all 13 topology nodes, declared-link toggling, performance chart identity
and filter retention. All six views were checked at 390 px without page overflow;
reduced motion disabled the view animation. Desktop and mobile screenshots were
inspected. The first run exposed replica drafts being reset after blur; that defect
was corrected before the successful acceptance. Unit/race checks and JS syntax
checks passed. All twelve deployments are ready; test traffic is stopped.

This is a targeted UI acceptance. Earlier HTTP/queue/Job/lesson evidence above is
reused; the complete older cluster suite was not rerun for presentation changes.

## Application network traffic — 2026-11-22

Deployed source: `6d4508532b6ae99f`. Added one **Ruch sieciowy** section, independent
five-second transfer buckets, 30-second averages, per-hop payload sizes/completion
cadence, KiB/s/Mbit/s, a five-minute history and response-size control (0–8192 extra
bytes). Map transfer mode labels rates and reverses HTTP arrows for returning
response bodies. Text labels sit above service boxes rather than underneath them.

Byte observations come from actual HTTP response reads and serialized task/DLQ
payloads at instrumented publish/delivery points. Unmeasured/logical queue events
are excluded. This does not measure interface capacity, packets, protocol headers,
ACK/NAK, NATS KV, telemetry, Kubernetes API traffic or TCP/TLS/CNI overhead. The
same payload crossing four HTTP hops contributes to four hop counters. Configured
cadence and measured completion cadence are labeled separately. History is in
controller memory; recent events can be replayed from the existing NATS stream.

* `make test` passed with regression coverage for exact closed-window boundaries,
  out-of-order observations, transfer direction, per-hop rates and intervals,
  excluding logical queue events, 12,000 observations independent of the display
  ring, stopped-traffic expiry and payload bounds.
* `python3 scripts/network-smoke.py` passed ten focused runtime checks: zero/8 KiB
  payload cases, exactly 8192 additional observed bytes on each of four HTTP hops,
  closed-window rate arithmetic, one queue publication/two retry deliveries with
  identical payload sizes, NAK exclusion, queue throughput and zero recent HTTP
  throughput after stopping.
* Live browser acceptance covered the new menu, payload/rate controls, nominal
  50 ms cadence at 20 req/s, measured flows/table, stable chart DOM, map rates and
  response direction, original event mode and a 390 px layout without page overflow.
  At 20 req/s with 8 KiB additional payload, a five-second sample measured about
  653 KiB/s across the four HTTP hops, including the JSON body.
* API/browser checks ran on `eed0d95cf2ea0202`; final `6d4508532b6ae99f` only moves
  long map labels above boxes. Its focused browser check verified no label/service
  rectangle overlap and the stopped generator. Existing backend evidence is reused.

Screenshots and detailed receipts remain under ignored `output/playwright/` and
`.state/network-*`. This is a local lab release, not a production network audit.

## Portfolio foundation — 2026-11-22

Accepted roadmap: `roadmap.md`; first foundation image `04bb4e48318bbfdc`.
The entire portfolio goal remains active; this is one verified increment.

* Generator deadlines retain fractional Unix seconds, new runs emit their first
  request immediately, and pacing uses elapsed time with bounded catch-up.
  Configuration refresh is asynchronous/single-flight with retry backoff. The data
  plane retains its cache and absolute deadline during control interruption.
* Topology counts use independent one-second buckets (30 closed seconds), plus
  current activity timestamps; span volume no longer truncates graph counts.
  Resource plots use/deduplicate actual Metrics API timestamps per component/node.
  HTTP history comes from independent five-second buckets, including intervals
  without resource observations.
* NATS/Jaeger pod templates carry hashes of their actual rendered configuration.
  Both changed configurations caused real pod replacement; restoring canonical
  configuration replaced them again. JetStream task history survived both changes.
* Generator scale is restricted to one replica, preserving the advertised global
  traffic cap. Failed queue attempts now perform real HTTP through all four hops
  with a bounded per-request failure flag; the first attempt returns 503 and retry
  returns 200. Result-store uses create-only event keys and age-filtered retrieval
  so replay does not renew existing outcomes or display expired event data.
* Unit/race checks passed: short/fractional deadlines, pacing/concurrency bounds,
  nonblocking/single-flight configuration refresh, span-heavy topology counts,
  measurement timestamps, independent traffic history and existing boundaries.
* Seven live foundation checks passed, including three one-second/one-RPS runs
  and HTTP throughout a real twelve-second control outage, recovered from the
  retained stream without extending the deadline. Three config-rollout checks and
  ten existing network checks passed. A focused trace assertion additionally
  confirmed failed and successful queue attempts each traverse four HTTP hops.
* Browser acceptance confirmed resource/traffic histories, generator-cap explanation
  and preserved map keyboard focus. Expected API connection errors during the
  controlled outage recovered; no JavaScript exception was observed.

The public-repository CI workflow reuses native make/Python entrypoints and performs
an isolated checkout bootstrap on an ephemeral hosted runner. Its actual run is
recorded separately when available; adding YAML is not CI/CD acceptance. Workflow
scope is this public lab, not private WatchMyApp/infrastructure repositories.

Hosted foundation acceptance passed at commit `e2f08e4`, source
`04bb4e48318bbfdc`: [CI run 45062176658](https://github.com/maciekb2/task-manager2/actions/runs/45062176658).
The run completed unit/race/syntax checks, a clean-checkout Minikube bootstrap,
full native HTTP/queue/Job/CronJob smoke, focused OTel/resource/queue, payload
accounting, control-outage and config update/restoration checks. A redundant
push-triggered run was cancelled before integration; subsequent workflow changes
restrict branch pushes to main and use pull-request events for feature branches.
This receipt does not claim the later portfolio release controls were in that build.

## Portfolio walkthrough and verified rollback — 2026-11-22

Local source `ad538c0bf1329cba` adds the portfolio landing page, bounded release
controls and actual worker image changes. Bootstrap builds/loads both the base
image and an `echo503` linker-flag variant with a source-derived `-demo-fault` tag.
The panel accepts no client image/command/namespace input and targets only echo.
Recovery intent is persisted before deploying the candidate and expires after 60 s.

* Unit/race and syntax checks passed. Release selection rejects unconfigured or
  foreign-image values. Existing unchanged foundation evidence is reused.
* Runtime checked distinct image IDs, base HTTP200, ready candidate pods returning
  actual HTTP503 and manual rollback to fresh HTTP200 from base pods.
* The first release fixture stopped at a transient web-proxy HTTP502 immediately
  after a control rollout. It had already accepted base/candidate/manual rollback.
  The fixture now waits for the actual session API, including during cleanup, and
  retains receipts on failure. A targeted continuation read the real action history:
  control was recreated between candidate deployment and automatic restoration;
  restoration happened 60.989 s after the candidate action, retained the original
  recovery schedule, restored base image IDs and produced fresh HTTP200.
* Actual UI interaction then verified performance/native waterfall, live map and
  network payload controls, queue retry and candidate/manual-rollback evidence.
  The 53-second recording in `assets/systems-lab-demo.mp4` is from that live run.
  Eight views were checked at 390 px without document overflow. No JS exception
  occurred; API errors during the deliberate controller restart are expected/retried.
* `scripts/durability-smoke.py` passed actual broker redelivery: publication was not
  broker-deduplicated, the consumer acknowledged its new sequence, the terminal KV
  revision/value stayed identical and no application work event was produced.
  A real result-store restart/replay preserved the HTTP outcome's original KV
  revision, creation timestamp and value hash. Broker credentials were kept only
  in process memory and an owned loopback port-forward; no credential is a receipt.

Hosted CI for this later increment is recorded when available. The foundation CI
receipt above is not misrepresented as acceptance of these new release controls.

Portfolio CI at `7e3026a` passed the earlier bootstrap/core/observability/network/
outage/config checks but stopped in the durability fixture: it assumed a new HTTP
outcome would be present after one second. The result store is asynchronous and
was catching up after broker reconfiguration. The fixture now waits for the exact
persisted trace outcome, rather than equating task completion with storage proof.
The affected local config-restart → durability sequence passed with that wait.
The original failed run remains visible:
[45063952285](https://github.com/maciekb2/task-manager2/actions/runs/45063952285).

## Scoped experiments — implementation checkpoint

The experiment lifecycle, propagated run IDs, independent per-phase statistics,
whole-window resource eligibility and JSON export passed unit/race checks. Focused
runtime acceptance passed eleven checks: actual faster/slower A/B, distinct phase
IDs, all four HTTP hops, no invented CPU sample for short runs, queue retry/native
Job carriers, native OTel run tags, drain/seal, export and explicit controller
interruption with sealed historical results preserved. The receipt is ignored
`.state/experiments-smoke-results.json`; this checkpoint does not yet claim the
later comparison UI or hosted experiment CI has been accepted.
### Review corrections (2026-11-22)

New echo/generator processes load persisted configuration before listening.
Consumer readiness reflects a bound subscription; a deleted durable is recreated
and rebound without replacing the process. Liveness has its separate endpoint.
Job execution spans preserve upstream HTTP failure codes. Release evidence uses
the action boundary and a responding pod of the desired image. The graph shows an
observed DLQ stream separately from retries; traffic controls follow live config
and preserve explicitly labelled drafts. Stop applies only to the live scenario.

Local: race tests passed; readiness acceptance proved new pods under 100% failure
and processing after actual durable deletion by the same consumer UID/restart
count. The first probe incorrectly required both Service backends to receive
traffic (HTTP connection reuse does not guarantee that); corrected to require an
actual new backend response and reject observed 200s. Browser acceptance verified
live/draft reconciliation, Stop semantics and DLQ discovery/inspector. Deployed
source for those checks: `22ace94559fb9622`. A subsequent small persistence-order
correction for the release boundary is subject to the next canonical CI run.

Hosted CI `45066294325` passed all first-release checks, including actual faulty
image, manual rollback and timed rollback after controller restart, at `fe96a4c`:
https://github.com/maciekb2/task-manager2/actions/runs/45066294325 . This result does
not certify later source. HTTP outcome replay capacity remains explicitly bounded
as documented in decisions/004; a dedicated retention path is deferred to stage 4.

CI `45068716761` exposed a fixture discovery race: after a completed rollout the
observer could still report the previous pod names and their pre-scenario 200.
The startup check now obtains post-rollout pod identities from Kubernetes and
waits for discovery to report those identities before examining responses. The
original failed run remains retained; no application default escaped a new pod
in that evidence. Other steps through durable replay passed in that run.

### Experiment UI acceptance

At deployed source `77c45c3cdfc55667`, actual browser actions created two 90 s
phases, downloaded the JSON export and opened a native scoped waterfall. Baseline:
694 responses, mean 6.08 ms, 7.71 req/s; 400 ms delay variant: 200 responses, mean
407.91 ms, 2.22 req/s. Both summaries were complete. Baseline had one eligible
13/13-pod CPU/RAM window; variant had none. The UI explicitly reported the missing
window and did not invent a zero comparison. Ninety seconds does not guarantee a
fully contained window with this Metrics Server cadence, so the suggested duration
is now 180 s. The two response-time charts now use a shared vertical scale. Those
small presentation changes await the next deploy/render check. API lifecycle
acceptance also passed eleven checks after scoped admission was introduced.

### Resilience runtime acceptance

Deployed source `15c725cf2a9e19ec`: unit/race checks passed. Actual cluster checks
proved gateway deadline/504, admission/429 with successful admitted work, open
breaker suppression and successful half-open recovery. Native traces proved two
parallel lookups overlapping by more than 100 ms, actual branch payload events and
cancellation on deadline. An HTTP task completed while result-store was offline;
20,001 synthetic span messages evicted the prior EVENTS window, while OUTCOMES
sequence 106 and its payload/time stayed unchanged and became visible after facade
restart. Ten distinct checks accepted (one fan-out check was repeated during the
continuation). The original fixture stopped on Jaeger's transient 404 before SDK
export; its bounded polling now tolerates that expected state. Prior protection
checks were reused rather than rerun. Canonical CI remains pending for this stage.

### Native HPA runtime acceptance

Source `b72801f3c99720a7`: race tests passed; six actual autoscaling checks passed.
At unchanged workload settings, one replica produced 926 responses / 20 s
(46.3 req/s); bucket P95 586.505–677.044 ms. Native HPA reported 499m average CPU
against 200m/pod, recommended 3 replicas, and Kubernetes made 3 ready. The later
20 s window had 999 responses (49.95 req/s), bucket P95 147.384–153.976 ms, no errors.
These are this local synthetic run, not a capacity guarantee. Config edits kept
the server deadline. A real control restart with an operator-injected expired
persisted deadline removed HPA and restored 2 ready pods. No host metrics used.

Resilience browser checks confirmed the live protection table and both observed
fan-out edges. The first edge assertion incorrectly required a vertical SVG path
with zero-width bounds to be Playwright-visible; DOM attachment is the correct
check, with visual screenshot inspection. Mobile inspection found a real table
overflow; horizontal table containment is fixed in the next presentation deploy.
Experiment CI `45070416641` passed unit/race and syntax, but bootstrap failed before
creating the profile: Minikube's lookup of the pinned Kubernetes patch hit the
shared runner IP's anonymous GitHub API rate limit (403). The bootstrap step now
uses the job's existing read-only ephemeral GITHUB_TOKEN, supported by the pinned
Minikube source (`pkg/minikube/download/gh/gh.go`). It is step-scoped, not written
to manifests or runtime, and no external account secret is introduced. Cleanup
skips a profile that never produced a kubeconfig. Original failure retained.
