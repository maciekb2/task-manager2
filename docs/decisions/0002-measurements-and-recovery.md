# ADR 0002 — observed facts, bounded faults and recovery evidence

Status: accepted implementation direction, 2026-11-22.

## Measurement contracts

Separate configured rates from completed requests. Aggregate counters independently
of a bounded display ring. Retain source timestamps/windows for Kubernetes resource
samples and gaps when absent; CPU/RAM are not exact per-request costs. Native OTel
spans measure selected inclusive wall time and must not be added as independent
costs. HTTP payload bytes count once per measured hop, excluding protocol overhead.

The control service can restart without forcing data-plane requests to synchronously
wait for configuration. Background refresh retains a bounded absolute traffic deadline.
Replay reconstructs eligible event history subject to the broker retention budget;
this does not turn in-memory telemetry into an offsite or unlimited history store.

## Release demonstration

Bootstrap builds a normal image and a deliberately faulty worker variant from the
same source. A linker-set flag makes the variant return HTTP503 for work; the process
health endpoint stays ready. The panel can choose only those server-configured local
images and only for the `echo` workload. It cannot supply an arbitrary registry,
command, manifest, namespace or service URL.

Persist rollback intent before changing the image. After sixty seconds, control
restores the base image; restarting control preserves the original deadline and
resumes recovery. If control itself is unavailable, recovery resumes on its return.
Manual rollback/reset also restore the base image. Bootstrap reconciles canonical
images. This deliberately demonstrates why process readiness is insufficient for
business acceptance.

Verification requires distinct runtime image IDs and actual replacement pods, a
fresh HTTP503 from the candidate, and fresh HTTP200 from the restored base image.
A successful patch/rollout response alone is not release or rollback proof.
