---
name: spec-audit
description: Audits the project against the challenge statement (init.md) and updates the docs/requirements/traceability.md matrix with status and evidence. Use to find what is missing, before milestones/delivery, or when the user asks for a compliance overview.
---

# Audit against the challenge

## Steps

1. Read `init.md` and `docs/requirements/traceability.md`.
2. For each requirement in the matrix, look for **concrete** evidence in the repository:
   - code (`file:line` or package),
   - test (test name and command/build tag),
   - schema (migration),
   - documentation (README/ARCHITECTURE/docs section).
3. Update the status:
   - `—` not started
   - `🚧` partial (explain what is missing in Notes)
   - `✅` implemented **and** with the required test/documentation evidence
   - `⚠️` implemented with risk or divergence from the challenge
4. Never mark `✅` without verifiable evidence. If you couldn't verify it (e.g. the test needs
   containers that aren't available), use `🚧` and note why.
5. Review the disqualifying criteria section separately and highlight any at risk.
6. If a requirement from `init.md` is missing from the matrix, add it with a new ID.

## Output to the user

- Summary per section (x/y done).
- Disqualifying criteria at risk.
- Next 3–5 highest-impact items, considering the scoring weights (§14).
