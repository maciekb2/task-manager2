# Systems Lab — portfolio goal and delivery roadmap

Accepted by the owner on 2026-11-22. This file tracks the active delivery goal;
checkboxes require implementation and verification, not just a plan or code.

## Purpose

A one-command local Kubernetes demonstrator of designing, deploying, observing
and diagnosing distributed systems. A technical interviewer should see a problem,
follow evidence to its cause, apply a change and compare the result in a 5–10
minute presentation. Guided learning is a secondary use case.

One repository, synthetic workloads, isolated `systems-lab` Minikube and project
kubeconfig. Cluster metrics only; no workstation monitoring or public lab ingress.
Independently deployed roles share Go code/image deliberately. Explain that tradeoff
rather than presenting deployment count as proof of architectural maturity.

## Delivery order and acceptance

### 1. Trustworthy foundation — verified locally; hosted CI pending

- [x] Apply NATS/Jaeger configuration changes through repeatable rollouts.
- [x] Preserve generator duration precision and rate while control is unavailable.
- [x] Keep topology counts complete under span-heavy traffic independently of the
      bounded recent-event display; retain bounded memory and duplicate handling.
- [x] Plot resource samples at their measurement timestamps and expose missing
      data/freshness accurately.
- [x] Resolve confirmed current review defects in affected paths; run focused
      regression tests and actual lab acceptance. Preserve previous accepted proof
      for unchanged behavior.

### 2. First portfolio release

- [ ] Performance demo: provoke a bottleneck, inspect trace/resources, apply a
      change and show its measured effect.
- [ ] Queue demo: backlog, retries/DLQ and duplicate handling with persisted outcomes.
- [ ] Kubernetes demo: pod recovery plus an actual version update and tested rollback.
- [ ] Interview-oriented README, architecture decisions, demo script and short
      visual presentation. Explicitly separate local checks, CI, deployment and
      recovery proof.
- [ ] Verify supported clean-checkout bootstrap and automated checks on the selected
      existing delivery path. Do not invent CI success from local commands.
- [ ] Publish a reviewed release after acceptance. Communicate predecessor migration
      and archive the predecessor only after acceptance of its replacement.

### 3. Reproducible experiments and comparison

- [ ] Run identifiers/configuration snapshots and bounded experiment history.
- [ ] Baseline versus intervention comparison with compatible measurement windows.
- [ ] Shared action/metric/trace timeline and exportable evidence without credentials.
- [ ] Repeating/aborting a run restores a known state and does not mix old work into
      current results.

### 4. Deeper resilience

- [ ] Bounded timeout and backpressure scenarios with visible consequences.
- [ ] Actual duplicate delivery and idempotent terminal results; explain the limits
      of at-least-once guarantees and external side effects.
- [ ] Circuit breaker and fan-out, each with a problem, protection, measurable cost
      and recovery scenario. Add services when they demonstrate new behavior.

### 5. Automatic response

- [ ] HPA: visible scaling reason, actual ready pods and measured effect/limits.
- [ ] Optional queue-driven consumer scaling after the core HPA demonstration.

## Definition of done

Each delivered increment has source, relevant checks, deployed version, runtime
postconditions and remaining limits recorded in `verification.md`. A new developer
can run the supported environment from documented prerequisites. Demonstrations
are repeatable and return to baseline. Single-node results are not production
capacity/HA claims. CPU samples are not exact per-request CPU costs; application
payload transfer is not a packet/interface measurement.

The full goal remains active until required stages are accepted. Optional features
are explicitly identified above; they are not silently substituted for core work.
