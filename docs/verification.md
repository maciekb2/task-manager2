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
