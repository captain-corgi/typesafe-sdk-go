# Contributing to typesafe-sdk-go

Thanks for contributing! This SDK is small and deliberately boring — please
help keep it that way.

## Ground rules

- **Zero runtime dependencies.** Go stdlib only. CI enforces this
  (`Zero dependencies` job); `go.mod` must contain only `module` and `go`
  directives. Never add a `require`, `replace`, `exclude`, or `tool` line.
- **Parity is the contract.** Behavior matches the Python SDK (see
  `AGENTS.md` and `plans/`). Where Python and JS references disagree, the
  Python SDK wins. Deliberate Go deviations are listed in README
  §"Deviations from the Python SDK" — keep that list accurate.
- **Public API changes** must update `README.md`, `CHANGELOG.md`
  (Keep a Changelog format), and `doc.go`.
- **Security-sensitive files** (see `.github/CODEOWNERS`) require code-owner
  review: network and credential handling (`transport.go`, `config.go`,
  `client.go`, `constants.go`), wire encoding (`json.go`, `questions.go`),
  log redaction (`logging.go`), release metadata (`version.go`, `go.mod`),
  CI workflows, and `AGENTS.md`. If your PR touches these, expect extra
  scrutiny — that is not a statement about you, it is about what those files
  can do.

## Development

Go 1.27+. From the repository root:

```sh
gofmt -l .                      # must print nothing
go vet ./...
go test -race ./...
go test ./responses_test.go     # one file, faster loop
```

Integration tests hit the live API and are skipped without credentials:

```sh
TYPESAFE_API_KEY=... go test -run Integration ./...
```

PR CI runs formatting, vet, race and plain tests on a platform matrix,
Staticcheck, Govulncheck, and the zero-dependency check — all secretless.
Live-API integration runs after merge, on trusted pushes only.

## Pull requests

- Keep PRs focused; one concern per PR.
- Add or extend tests for behavior changes. Port test ideas from the Python
  SDK's suite when parity is involved.
- Match the existing code style: table-driven tests, stdlib `testing`,
  `httptest` / mock `RoundTripper`.
- AI-assisted contributions are welcome, but you are responsible for every
  line — review the diff like you wrote it, because you are submitting it.
- By contributing, you agree your contributions are licensed under the
  repository's license (see `LICENSE`).

## Reporting security issues

See [SECURITY.md](SECURITY.md). Please do not use public issues for
vulnerability reports.
