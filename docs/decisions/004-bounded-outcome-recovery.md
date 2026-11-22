
# Bounded outcome recovery (first release)

HTTP results are a best-effort teaching projection, not an audit log. `EVENTS`
retains **at most** one hour, 20,000 events or 32 MiB, whichever is reached first;
spans share that capacity. An offline result-store can only replay events still
present. Under heavy span traffic the replay window can be seconds. Its separate
8 MiB KV projection can also fill before the one-hour TTL. The short restart demo
proves preservation for the demonstrated window, not one-hour lossless capture.
Dedicated bounded outcome retention belongs to the resilience increment; it must
not be described as production delivery/durability until that path is verified.

Superseded in the resilience increment: dedicated OUTCOMES retention plus a
bounded read facade replaces the redundant HTTP KV projection. See ../resilience.md
and the actual retention-pressure proof in ../verification.md. The original first
release limitation and its historical evidence above are preserved.
