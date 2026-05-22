# Non-Functional Requirements

---

## NFR-01 — Correctness of Lease Atomicity

The lease operation must be implemented as a single atomic SQLite transaction. It must be impossible for two concurrent `POST /api/tasks/lease` requests to return the same task. SQLite's serialised write mode and WAL provide the necessary isolation; the implementation must not introduce additional race conditions at the application layer.

**Acceptance:** Concurrent lease requests in a stress test must never return the same task ID from the same `queued` state.

---

## NFR-02 — Fencing Token Enforcement

`POST /api/tasks/{id}/complete` and `POST /api/tasks/{id}/fail` must reject requests with a fencing token that does not match the current attempt's token. The server must return `409 Conflict` for stale tokens.

**Acceptance:** A worker that sleeps past its lease expiry and then calls `complete` receives `409`; the task remains in whatever state the new owner set.

---

## NFR-03 — Automatic Lease Expiry

Tasks whose `lease_expires_at` has passed and whose `attempt_count < max_attempts` must be automatically requeued to `queued`. Expiry is detected either on next read or by a background goroutine running at a configurable interval (default: 10 seconds). Tasks that exhaust `max_attempts` must transition to `timed_out`.

**Acceptance:** A leased task with no heartbeat transitions back to `queued` within 2× the expiry check interval after its `lease_expires_at` passes.

---

## NFR-04 — Performance Budget

| Operation | Target |
|-----------|--------|
| `POST /api/tasks/lease` | < 50 ms p99 under 50 concurrent agents on a laptop |
| `GET /api/tasks` (100 results) | < 100 ms p99 |
| `GET /api/metrics` | < 200 ms p99 |
| Dashboard page load | < 500 ms on localhost |

SQLite WAL mode is sufficient for the local-first use case. No additional caching layer is required in v1.

---

## NFR-05 — Single Binary, Zero External Dependencies

The service must compile to a single static binary with no required external processes (no Redis, no Postgres, no message broker). The only runtime file is the SQLite database.

**Acceptance:** `go build` produces a working binary; a fresh checkout needs only Go and no additional runtime installs.

---

## NFR-06 — Log Volume Handling

Task logs can be high-volume. The API must accept log entries efficiently and support paginated queries. The database must not block normal task operations during log writes.

**Acceptance:** Ingesting 1,000 log lines per minute across 10 concurrent agents does not degrade lease or heartbeat latency beyond NFR-04 targets.

---

## NFR-07 — Graceful Shutdown

On `SIGINT` or `SIGTERM`, the service must finish in-flight requests (up to a configurable drain timeout, default 5 s) before closing the database connection and exiting.

---

## NFR-08 — Idempotency of Agent Registration

`POST /api/agents/register` called multiple times with the same `agent_id` must upsert rather than error. Agents restart and re-register frequently; duplicate registrations must not accumulate stale rows.

---

## NFR-09 — Observability

The service must log structured JSON lines to stdout at startup and for each error. No dependency on an external logging service. Human-readable mode toggled by a flag for local development.

---

## NFR-10 — Simplicity Constraint

Any proposed feature that requires more than one new dependency, a non-trivial schema migration, or distributed state must be deferred to v2 with documented rationale.
