---
name: adr
description: Records an architectural decision as a numbered ADR in docs/adr/, updates the index and the summary in ARCHITECTURE.md. Use when a technical choice is made or needs to be proposed (libraries, lock strategy, Money representation, idempotency hash, auth, queues, shutdown...).
---

# Architecture Decision Record

## Steps

1. List `docs/adr/` and take the next number (4 digits, sequential: `0001`, `0002`...).
2. Copy `docs/adr/0000-template.md` to `docs/adr/NNNN-title-in-kebab-case.md`.
3. Fill it in:
   - **Context**: cite the `init.md` section(s) motivating the decision.
   - **Options considered**: at least two, with honest pros and cons.
   - **Decision**: objective and verifiable.
   - **Consequences**: including limitations and accepted risks.
   - **Verification**: how the decision is proven (test, constraint, metric).
4. Initial status: `Proposed` if the user hasn't approved yet; `Accepted` only with explicit approval.
5. Add the row to the index in `docs/adr/README.md`.
6. If `Accepted`, add a short summary to the matching section of `ARCHITECTURE.md` linking to the ADR.
7. If it supersedes a previous ADR, mark the old one `Superseded by NNNN` (don't delete it).

## Rules

- One decision per ADR.
- An accepted ADR is not rewritten; changes create a new ADR.
- Don't invent decisions the user hasn't made: write it as `Proposed` and ask.
