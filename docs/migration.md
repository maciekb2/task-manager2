# Task Manager → Systems Lab

The repository's previous `main` was a small Go/gRPC Task Manager prototype.
Its source is retained under `legacy/v1`, with its original Git history. Those
modules are historical and are not part of the v2 bootstrap or test entrypoints.

The separate `maciekb2/task-manager` evolved further, with JetStream, Redis,
observability and a graph visualizer spread between `main`, `dev` and old PRs.
Systems Lab carries forward the demonstrated concepts in a unified local example:
real HTTP work, distinct processing stages, live topology, retry/DLQ, and Kubernetes
execution. It does not claim that every old branch or PR was merged or validated.

Migration sequence:

1. Keep both Git histories and the old main revision reachable.
2. Deliver and verify the runnable lab in this repository.
3. Select any remaining uniquely useful examples from the predecessor's PRs.
4. Update the predecessor README with the successor link and scope difference.
5. Archive the predecessor after owner acceptance of the replacement.

Archiving/closing old PRs is separate from launching this lab. No old repository
or branch is deleted as part of the bootstrap.
