---
name: go-conventions
description: Go code conventions for this project (layering, errors, context, money, naming, formatting). Use when writing, refactoring or reviewing any .go file in the wagering service.
---

# Project Go conventions

Also read the current decisions in `docs/adr/` — they take precedence over generic advice in this skill.

## Layers and dependencies

- **Domain** (entities, value objects, errors, domain events, ports/interfaces) is plain Go.
  Forbidden imports: `go.uber.org/fx`, `net/http`, AWS SDK, `pgx`/`database/sql`, concrete loggers.
- **Application** (use cases) orchestrates domain + ports; takes `context.Context`; defines the SQL
  transaction boundary through a port (e.g. unit of work / `TxRunner`) without leaking driver types.
- **Adapters** (HTTP, SQS, PostgreSQL, Keycloak, outbox publisher) implement ports.
- **Composition** (Fx) lives only in the wiring package and `cmd/`. Plain constructors, no `fx.In` in the domain.
- Dependencies point inward: adapters → application → domain. Never the other way around.
- Check with `go list -deps ./<domain-package>/... | grep -E 'fx|net/http|aws|pgx|sql'` (must be empty).

## Modeling

- Encapsulated state: unexported fields, explicit getters, transition methods with business names
  (`Debit`, `Credit`, `MarkProcessed`, `Reject`).
- Validating constructors (`NewX(...) (X, error)`); separate **rehydration** (`RehydrateX(...)`) that
  does not re-apply movements, transitions or emit events.
- The zero value of a value object must be detectable and rejected (e.g. `Money{}` without currency is invalid).
- Immutable value objects: value receivers returning new values.

## Money

- Never `float32`/`float64`. No `strconv.ParseFloat`, no `json.Number` converted to float, no `NUMERIC`
  scanned into a float.
- Arithmetic only between compatible currencies; overflow handled explicitly if the representation is `int64`.
- External JSON: `{"amount":"25.00","currency":"BRL"}` — amount is always a string with two decimal places.
- Quick regression check: `grep -rn 'float' --include='*.go' .` and justify every hit.

## Errors

- Classifiable domain errors: sentinels (`var ErrInsufficientFunds = errors.New(...)`) or types used with
  `errors.As`. Always wrap with `fmt.Errorf("context: %w", err)`.
- Business rejection ≠ transient failure ≠ permanent failure. The error type must make the distinction
  without string comparison.
- `panic` only for programming bugs (impossible internal invariants), never for business rules.
- Mapping error → HTTP status / `failureCode` / SQS action lives in the adapter, not in the domain.

## Context and I/O

- Every method doing I/O takes `ctx context.Context` as the first parameter and honors cancellation.
- Don't store `context.Context` in structs. Don't use `context.Background()` outside `main`/lifecycle/tests.
- Goroutines always have a clear owner: whoever starts one guarantees it ends (see `fx-module` skill).

## Style

- `gofmt`/`goimports` mandatory; `go vet ./...` clean.
- Short, singular package names; no `util`/`common`/`helpers`.
- Small interfaces, declared by the consumer.
- No mutable global variables, no `init()` with side effects.
- Structured JSON logs (`log/slog` or whatever an ADR defines) with `correlationId`, `messageId`,
  `transactionId`, `walletId`, `providerId` when available. **Never** log tokens, secrets or full
  financial payloads.
- Comments explain *why* (especially concurrency and transaction decisions), not *what*.

## Checklist before finishing

- [ ] `gofmt -l .` empty and `go vet ./...` clean
- [ ] Domain has no infrastructure imports
- [ ] No floats in any monetary path
- [ ] Errors classifiable and wrapped with `%w`
- [ ] I/O takes `context.Context`
- [ ] Tests added/updated (`go-testing` skill)
