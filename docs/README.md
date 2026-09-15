# Documentation

| Folder / file | Contents |
| --- | --- |
| [`adr/`](adr/) | Architecture Decision Records: one numbered technical decision per file |
| [`requirements/traceability.md`](requirements/traceability.md) | Matrix: challenge requirement → status → evidence (code, test, docs) |
| [`api/`](api/) | HTTP contracts: endpoints, authentication, status codes, error bodies, `failureCode` |
| [`events/`](events/) | Message contracts: SQS input, outbox output events, routing |
| [`runbooks/`](runbooks/) | Reproducible procedures: integration tests, multiple instances, failure simulation, load |
| [`roadmap.md`](roadmap.md) | Implementation phases |

Deliverable documents required by the challenge live at the repository root:

- [`README.md`](../README.md) — how to run and test.
- [`ARCHITECTURE.md`](../ARCHITECTURE.md) — decision summary, linking to the ADRs.

The original challenge statement (in Portuguese) is [`init.md`](../init.md).
