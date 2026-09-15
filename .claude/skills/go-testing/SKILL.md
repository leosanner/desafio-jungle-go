---
name: go-testing
description: Project test strategy and patterns — unit, integration with real containers (PostgreSQL, Keycloak, LocalStack), concurrency, multiple instances, failure/recovery and -race. Use when writing or reviewing tests, or when deciding how to prove a guarantee from the challenge.
---

# Testing

Source requirements: `init.md` §13 (mandatory verification) and §8 (concurrency).

## Levels

| Level | Scope | Infra | How to run |
| --- | --- | --- | --- |
| Unit | Domain and use cases with simple fake ports | None | `go test ./...` |
| Integration | Adapters, migrations, constraints, inbox/outbox, auth, Fx start/stop | Real containers | build tag defined in an ADR (e.g. `integration`) |
| System / multi-instance | ≥3 independent processes, failures and restarts | Docker Compose | script/Makefile documented in `docs/runbooks/` |

- Unit tests don't depend on network, wall clock or execution order. Inject clock and ID generator.
- Integration tests do **not** replace PostgreSQL, IdP and SQS with mocks. Mocks only for things outside scope.
- Always run `go test -race` on applicable packages.
- If build tags are used, document the command in `README.md` and `CLAUDE.md`.

## Patterns

- Table-driven tests with `t.Run(tc.name, ...)`; `t.Parallel()` when safe.
- Compare errors with `errors.Is`/`errors.As`, never by string.
- Helpers call `t.Helper()`; cleanup with `t.Cleanup`.
- Unique IDs per test so tests can run in parallel against the same database.
- Financial assertions always check **three things**: stored balance, ledger entries and transaction
  states. At the end, balance == Σcredits − Σdebits.

## Concurrency

- Use a barrier for a simultaneous start:
  ```go
  start := make(chan struct{})
  var wg sync.WaitGroup
  for i := 0; i < n; i++ {
      wg.Add(1)
      go func() { defer wg.Done(); <-start; /* request */ }()
  }
  close(start)
  wg.Wait()
  ```
- Counters with `sync/atomic`; results collected through a channel or a guarded slice.
- Never use `time.Sleep` for synchronization; poll with a deadline (`require.Eventually` or own helper).
- Cross-process concurrency (≥3 instances with their own connections and memory) cannot be proven with
  goroutines in one process — use real instances (Compose or `exec.Command` on the binary).

## Mandatory scenarios (§13) — keep all of them tracked

Unit: `Money` parsing/operations, scale, limits, invalid input, currency mismatch, wallet invariants,
state transitions, rules for the 5 external kinds, zero-amount policy per kind, internal opening
(metadata and events), payload conflict for the same key.

Integration: migrations (up/down), constraints, ledger immutability, financial atomicity, inbox,
redelivery, concurrent outbox, retry, DLQ, recovery after restart, Fx composition start/stop with
resource release.

Auth: real IdP; missing/invalid/expired credentials; provider isolation (queries and replays);
restriction of internal operations; no financial side effect and no data leak on denied access.

Concurrency and recovery:
1. Same bet sent 50× in parallel → a single debit.
2. Two 80.00 bets on a 100.00 balance → 1 processed, 1 rejected for insufficient funds, balance 20.00, 1 debit.
3. Distinct wallets in parallel.
4. Relevant scenarios with ≥3 instances.
5. Consumer killed after commit and before delete → redelivery without duplication.
6. Two publishers on the same outbox → recovery, `eventId` preserved.
7. `REFUND`/`ROLLBACK` before its reference → later resolution or rejection on expiry.
8. Restart preserves idempotency, pending work and consistency; `PENDING` resumed by another instance.
+ Scenarios crossing HTTP and SQS for the same operation.
+ Duplicate tests must prove that **application** deduplication kicked in (not just SQS FIFO dedup).

## Failure simulation

- Injectable failure points (test hooks/flags) between steps: after commit / before SQS delete;
  after publish / before marking the outbox row. Document the mechanism in an ADR and in `docs/runbooks/`.
- `kill -9` the process for abrupt termination; stop containers for temporary unavailability.

## When done

- Update the evidence column in `docs/requirements/traceability.md` with the test name.
