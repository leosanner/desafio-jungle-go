# 0010 — Per-wallet concurrency

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §6.2, §8; matrix IDs GAR-06, GAR-07, WAL-06, CON-01, ELIM-04, ELIM-07

## Context

The wallet is the financial aggregate: identity, player, currency, balance, version and timestamps.
Debits must keep the balance non-negative. Writer conflicts must not drop a committed update
(§6.2). Coordination is **per wallet**; global locks are forbidden; independent wallets must
proceed in parallel (GAR-06). Balance updates must prevent lost updates (GAR-07). The service must
be correct with N instances, each with its own connections and memory (ELIM-07).

Mandatory scenario (`init.md` §8): a wallet with **100.00 BRL** receives two distinct **80.00 BRL**
bets at the same time. One bet is processed, the other is rejected for insufficient funds, the
final balance is **20.00 BRL**, and the ledger has a single debit. Replays must not change that
outcome. Other wallets must keep processing in parallel.

The lock lives inside the unit-of-work transaction ([ADR 0009](0009-sql-unit-of-work.md)). Application
mutexes or an in-memory lock map cannot satisfy N instances.

## Options considered

### Option A — `SELECT ... FOR UPDATE` plus version-conditioned `UPDATE`

Inside `UnitOfWork.Within`, lock the wallet row, rehydrate, mutate in memory, append ledger and
transaction rows, then `UPDATE` the wallet with the locked row’s version in the `WHERE` clause.

- Pros: Serializes writers on that wallet only; the 100.00 / two 80.00 case needs no retry loop
  for the loser (the second writer sees 20.00 and the domain rejects); row locks work across
  instances; the version predicate is a second check against lost updates; independent wallets
  do not block each other.
- Cons: Hold time equals the rest of the callback; a slow callback blocks other writers on the
  **same** wallet (acceptable; that is the serialization we want).

### Option B — Pure optimistic concurrency (version `UPDATE` without `FOR UPDATE`)

Read the wallet, mutate, `UPDATE ... WHERE id = $1 AND version = $2`; on 0 rows, retry with a
limited backoff.

- Pros: No row lock during domain work; readers that do not need to write are unaffected.
- Cons: The 100.00 / two 80.00 case is retry-noisy: one writer loses the race, re-reads 20.00,
  then rejects. Under load, retries amplify. Lost-update protection depends entirely on the
  application retrying correctly; a missed retry path looks like success with a stale read.

### Option C — PostgreSQL advisory lock keyed by wallet id

`pg_advisory_xact_lock` (or session lock) on a hash of `wallet_id` inside the same transaction,
then write as usual.

- Pros: Works with N instances; can lock before the row exists (insert races).
- Cons: Less visible than a row lock (`pg_locks` / `FOR UPDATE` on `wagering.wallets`); easy to
  hash-collide or lock the wrong namespace; session-level locks are easy to leak. Acceptable,
  but weaker as the documented primary strategy than locking the wallet row itself.

## Decision

**Primary strategy:** pessimistic row lock on the wallet, then a version-conditioned update, all
inside the unit-of-work transaction.

1. `SELECT ... FROM wagering.wallets WHERE id = $1 FOR UPDATE`
2. Rehydrate the domain `Wallet` from that row and mutate in memory (`Debit` / `Credit` / apply).
3. Write ledger and transaction rows on the same tx.
4. `UPDATE wagering.wallets SET ... WHERE id = $1 AND version = $2` where `$2` is the **locked
   row’s** version (the value before this operation’s increment).

**0 rows** on that `UPDATE` is a programming/concurrency **invariant failure** (lost update). It is
not retried silently. With `FOR UPDATE`, a second writer cannot obtain the row until the first
commits, so 0 rows means a bug (wrong version passed, update outside UoW, or a missing lock), not
a normal race.

Forbidden as coordination:

- a process-global mutex
- a session-level advisory lock on a **constant** (one lock for the whole service)
- a table-level lock
- an in-memory lock map keyed by wallet id

Independent wallets proceed in parallel because only the locked **row** is contested. This works
with N instances because locks live in PostgreSQL (ELIM-07, GAR-06).

Schema also has `CHECK (balance_minor >= 0)` as a last line of defense (GAR-03, GAR-08, ELIM-04).
The domain still rejects insufficient funds before the write; the check exists so a missed lock
cannot persist a negative balance.

Reference lookup for `REFUND` / `ROLLBACK` happens in the **same** tx. Lock the **target wallet**
(the operation’s wallet), not a second row for its own sake. If a future use case must lock two
wallets, acquire locks in `id` order to avoid deadlock — **not** needed in Phase 3.

Optimistic-only (`UPDATE WHERE version` without `FOR UPDATE`) is **rejected** as the primary
strategy: the mandatory two-bet scenario is simpler and less retry-noisy with pessimistic
serialization. Advisory locks remain a possible supplement later (e.g. insert-before-row), not
the documented default.

## Consequences

- Positive: GAR-06/07, WAL-06, CON-01 and ELIM-04/07 have an explicit, instance-safe mechanism;
  the two-bet test has a single serialization point; negative balance has both domain and schema
  guards.
- Negative / accepted risks: writers on the same wallet queue on the row lock; a long `Within`
  callback delays other operations on that wallet only. `CHECK (balance_minor >= 0)` turning into
  a check-violation is mapped as a classifiable error ([ADR 0009](0009-sql-unit-of-work.md)), not
  treated as success.
- Known limitations: opening a wallet (insert of a new row) may still need a uniqueness constraint
  on `(player_id, currency)` rather than `FOR UPDATE` of a missing row; that is a schema concern,
  not a second locking strategy. Two-wallet lock ordering is deferred.

## Verification

- Integration test: wallet 100.00 BRL, two concurrent distinct 80.00 BRL bets → one `PROCESSED`,
  one `INSUFFICIENT_FUNDS`, balance 20.00, one ledger debit; replays do not change the result.
- Same test with **three processes** (separate connections, no shared memory).
- Concurrent operations on **two different** wallets complete without waiting on each other
  (no global lock).
- `UPDATE ... AND version = $2` returning 0 rows fails the operation (invariant error), not a
  silent success.
- Schema: `CHECK (balance_minor >= 0)` present; a direct SQL debit below zero is rejected by
  PostgreSQL.
- Code search: no package-level `sync.Mutex` / lock map guarding all wallets; no
  `pg_advisory_lock` on a constant.
