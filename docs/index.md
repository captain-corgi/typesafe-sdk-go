# TypeSafe AI SDK for Go — Documentation

[TypeSafe AI](https://typesafe.ai) answers typed, named questions about a piece
of state in a single request ("System One"). This is the documentation for the
Go client SDK: `package typesafe`, zero runtime dependencies, at feature parity
with the Python SDK v0.7.0.

The [README](../README.md) is the quick-start and API quick reference. These
pages explain how the SDK behaves, why it is built the way it is, and where each
behavior lives in the code.

## Document map

| Document | Question it answers | Primary executable owner |
|---|---|---|
| [Getting started](getting-started.md) | How do I install the SDK and make my first call? | [`examples/quickstart`](../examples/quickstart) |
| [Architecture](architecture.md) | How is the SDK organized, and how does one call flow through it? | `client.go`, `transport.go` |
| [Configuration](configuration.md) | How are options, environment variables, timeouts, and the HTTP client resolved? | `config.go`, `client.go` |
| [Questions](questions.md) | How do I ask questions, and what is sent on the wire? | `questions.go` |
| [Responses](responses.md) | How are answers decoded, grouped, and validated? | `responses.go` |
| [Error handling](errors.md) | What errors exist and how do I match them? | `errors.go` |
| [Retries and timeouts](retries.md) | What gets retried, with which delays, under which budget? | `retry.go`, `transport.go` |
| [Wire protocol](wire-protocol.md) | What exactly goes over HTTP, byte for byte? | `transport.go`, `json.go`, `constants.go` |
| [Observability](observability.md) | What does the SDK log, and what is redacted? | `logging.go` |

## Sources of truth

Source and tests at the repo root own behavior; these docs navigate and
explain it.

- [`plans/2026-09-19-typesafe-sdk-go/contracts.md`](../plans/2026-09-19-typesafe-sdk-go/contracts.md)
  is the distilled behavioral contract (wire forms, headers, error message
  extraction, retry semantics), ported from the reference SDKs.
- [`plans/2026-09-19-typesafe-sdk-go/plan.md`](../plans/2026-09-19-typesafe-sdk-go/plan.md)
  owns the design decisions and the file-by-file spec.
- Parity policy: where the Python and JavaScript references disagree, the
  **Python SDK v0.7.0 wins**. Deliberate Go deviations are ledgered in
  [README §"Deviations from the Python SDK"](../README.md#deviations-from-the-python-sdk)
  and plan §6; keep both lists accurate when behavior changes.
- Every behavioral claim in these pages is exercised by the table-driven test
  suite next to each source file (`go test -race ./...`).

## Conventions

- Diagrams are [mermaid](https://mermaid.js.org) and render on GitHub.
- In interface and error diagrams, arrows mean "matches with `errors.As`" —
  Go realizes subtype semantics through embedding and `Unwrap`, not language
  inheritance.
- Pages link to the source file and test file that own each claim rather than
  restating code.

## Route record (maintenance)

This docs route was established 2026-09-19 via the `ak:docs init` workflow:
the ten pages listed in the document map above. No pre-existing documentation
was replaced. README remains the owner of reference tables (options, env vars,
error list, deviations); `docs/` owns behavior, decisions, and diagrams. Future
`update` / `summarize` runs should reconcile against this route.
