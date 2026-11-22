# Scoped baseline/variant experiments

Use **Eksperymenty A/B** to create one active experiment, measure a baseline and
then one variant. Each phase receives a server-generated run ID propagated through
HTTP, task messages, native Jobs/CronJobs and OpenTelemetry. Export uses the
`systems-lab-experiment-v1` JSON schema and includes actual source/configuration,
phase outcomes, action timeline, root traces and eligible Kubernetes samples.

A phase first restores known workload settings and waits for ready workers, an
empty task queue and the previously observed native Jobs to finish. Only then does
its measurement clock start. At its deadline or an explicit finish, it stops new
traffic/schedules, drains owned requests/tasks/Jobs and waits for the event consumer
to catch up. Failure to drain or missing events makes the measurement explicitly
partial. Aborting restores baseline settings and preserves a marked partial record.

Completed measurements are separate from the bounded recent-event display. Root
counts/latencies and per-hop payload belong to their run ID. UI charts outside the
experiment comparison remain global cluster observations; the context banner says
so. Changing settings during a phase appears on its timeline and must be considered
when interpreting the initial-configuration table. Partial runs are not eligible
for an unqualified A/B improvement claim.

Resource samples are eligible only when their entire API measurement windows fall
inside the phase and coverage is complete. Repeated identical samples are deduplicated.
Short phases may have no eligible CPU/RAM samples; that is unavailable data, not zero
usage. Resource samples describe the whole lab, not exact run/function CPU accounting.
Node samples require their own eligible timestamps/windows. Global traffic history
and source resource timestamps retain their existing independent semantics.

A controller restart explicitly interrupts live measurement instead of presenting an
uncheckpointed result as complete. Sealed phases and a ready baseline survive in the
existing broker's `lab-experiments` KV bucket. The UI retains ten recent experiments;
storage is limited to 8 MiB and 24 hours. This uses the existing local PVC and is not
offsite recovery. Failed persistence is surfaced in the UI; export the current view
before restarting if a save failed. Raw credentials, arbitrary environments and
Secrets are not experiment fields.

Use `python3 scripts/experiments-smoke.py` for actual scoped A/B, four-hop correlation,
queue/Job trace carriers, draining/export and controller interruption acceptance.
The script uses only this lab's kubeconfig/context/namespace and resets its controls.
