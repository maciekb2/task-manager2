# ADR 0001 — a local portfolio system with independently deployed roles

Status: accepted scope, 2026-11-22.

## Context

The primary audience is a technical interviewer. The system must demonstrate
engineering decisions and observable consequences in a short walkthrough. It must
also run from one repository without private platform credentials or services.

## Decision

Keep an isolated Minikube profile, source-derived application images and one-command
bootstrap. Deploy separate Go roles independently while sharing their source/image.
Use actual HTTP, NATS JetStream and Kubernetes resources rather than UI simulation.
Retain guided lessons as a secondary mode; the first screen presents three demos.

## Consequences

Shared libraries reduce setup and duplicated transport/instrumentation code. A
source change rebuilds the shared artifact and can redeploy several roles; this is
not independent repository/release ownership for each microservice. Separate pods
still expose real readiness, scheduling, scaling, communication and failure behavior.

The broker is single-node with bounded local persistence. Traces are bounded and
volatile. These choices keep the lab small; they do not demonstrate production HA,
offsite recovery, multi-tenancy or cross-node capacity. Public distribution contains
synthetic workloads and no private application implementations or credentials.
