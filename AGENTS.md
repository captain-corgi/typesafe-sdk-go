# AGENTS.md — typesafe-sdk-go

Go client SDK for the TypeSafe AI API (`https://api.typesafe.ai`), at feature
parity with the **Python SDK v0.7.0**. Zero runtime dependencies — Go stdlib
only; never add a dependency. Module path is `github.com/captain-corgi/typesafe-sdk-go`
but the package is named `typesafe` (like `go-openai`). Requires Go 1.27+.

## Layout

- Flat single `typesafe` package at the repo root — no `pkg/` or `internal/`.
  One file per concern: `client.go`, `transport.go` (retry loop + header merge),
  `questions.go`, `responses.go`, `errors.go`, `retry.go`, `config.go`,
  `json.go`, `logging.go`, `constants.go`, `version.go`, `doc.go`. Tests sit
  alongside (`*_test.go`).
- `examples/` — cookbook programs, each `package main` + stdlib only. They hit
  the **live API** (no mocking) and need `TYPESAFE_API_KEY`; offline behavior
  is the test suite's job.
- `plans/2026-09-19-typesafe-sdk-go/` — `plan.md` (design, file-by-file spec)
  and `contracts.md` (behavioral contracts distilled from the reference SDKs:
  wire forms, headers, error message extraction, retry semantics). Read
  `contracts.md` before changing wire encoding, headers, errors, or retries.
- `references/` — git **submodules**: `typesafe-sdk-python` (v0.7.0, primary
  parity reference) and `typesafe-sdk-js` (secondary cross-check). Read-only;
  never edit. If empty: `git submodule update --init`.
- Governance: `SECURITY.md` (vulnerability reporting), `CONTRIBUTING.md`
  (contribution rules), `.github/CODEOWNERS` (owner-reviewed sensitive paths).
- `.claude/`, `.agentkit/` — tooling config, not part of the SDK.

## Commands

```sh
gofmt -l .                      # CI fails on any output
go vet ./...                    # also compile-checks examples
go test -race ./...             # full suite (CI runs this)
go test ./responses_test.go     # focused file
go test -run Integration ./...  # live API; skipped without TYPESAFE_API_KEY
TYPESAFE_API_KEY=... go run ./examples/quickstart
```

## Rules that matter for edits

- **Parity is the contract.** Where Python and JS references disagree, Python
  v0.7.0 wins. Deliberate Go deviations are listed in README §"Deviations from
  the Python SDK" and plan §6 — keep those lists accurate when behavior
  changes.
- **Public API changes** must update `README.md`, `CHANGELOG.md` (Keep a
  Changelog), and `doc.go`. `version.go`'s `Version` signals the Python parity
  baseline — bump it only with (or toward) a matching Python SDK release.
- **Wire encoding** (`json.go`, `questions.go`): omit nil optionals, preserve
  explicit `null`s nested inside values, always emit `"type"` first, raw
  questions pass through untouched. Go emits sorted keys — that's accepted.
- **Headers**: `Authorization`, `Accept`, `Content-Type`, `User-Agent`,
  `X-TypeSafe-*` are force-set after user merges and cannot be overridden;
  `X-TypeSafe-Retry-Count` is stripped from input and set on retries only.
- **Errors**: taxonomy matched with `errors.As`; subclasses embed `*APIError`
  and `Unwrap` to it; `*TimeoutError` also satisfies `*ConnectionError` and
  `net.Error`. `ResponseValidationError.FieldPath` is dotted
  (`answers.tone.confidence`, `models[1].name`).
- **Logging**: `log/slog`, silent by default, logger name/attribution `typesafe_sdk`;
  secret headers redacted (see `secretHeaders` in `constants.go`).
- **Tests**: stdlib `testing` + `httptest`/mock `RoundTripper`, table-driven,
  ported from the Python suite — plan.md §4 maps each test file to its contract
  coverage. Retry tests are deterministic via package-internal `sleep`/
  `jitterRand` injection hooks. Client must stay safe for concurrent use
  (`-race` runs in CI).

## Gotchas

- Dev environment is Windows (Git Bash); CI is ubuntu-latest on Go 1.27.x +
  stable, triggered on `master`, `develop`, and PRs. Main branch is `master`.
- CI is split by trust boundary: `ci.yml` (push + PR, **secretless** — tests,
  Staticcheck, Govulncheck, zero-dependency check) and `integration.yml`
  (trusted pushes only; sole consumer of the `TYPESAFE_API_KEY` secret held
  in the `typesafe-integration` GitHub environment). Never add secrets or a
  `pull_request_target` trigger to PR CI.
- Integration tests and all examples make real network calls; guard on
  `TYPESAFE_API_KEY`.
- `references/` content is ignored by the build but pinned as submodules —
  don't commit changes inside it.
