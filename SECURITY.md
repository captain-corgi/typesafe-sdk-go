# Security Policy

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Use [GitHub private vulnerability reporting](https://github.com/captain-corgi/typesafe-sdk-go/security/advisories/new)
for this repository. If that is not possible, email the maintainer via the
address listed on their GitHub profile.

Please include:

- a description of the issue and its impact,
- steps or a proof of concept to reproduce it,
- affected versions or commits,
- any suggested mitigation.

You should receive an initial response within a few days. Please do not
disclose the issue publicly until a fix is released and you have been given
the go-ahead (coordinated disclosure).

## Scope

This SDK is a thin HTTP client. Security issues of interest include anything
that could compromise users of the SDK:

- credential handling: where the API key is sent, logged, or leaked
- redirect and TLS behavior that could exfiltrate credentials
- log redaction failures that expose secrets or sensitive bodies
- response parsing issues with memory-safety or correctness impact
- supply-chain issues (this SDK is stdlib-only by contract — see `go.mod`)

Issues in the TypeSafe API service itself are out of scope here; report them
through TypeSafe's own channels.

## Supported versions

| Version | Supported |
|---|---|
| `master` | yes |
| tagged releases | the latest minor line |

The project is pre-1.0; fixes land on `master` and in tagged releases as
they are cut.
