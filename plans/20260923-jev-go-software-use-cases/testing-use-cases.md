# Jev in Go software testing: 20 use cases

Research date: 2026-09-23. These are **proposed testing adaptations** of TypeSafe's documented bounded-question patterns, not published Jev testing features or measured results. They complement the [general Go software](go-software-use-cases.md), [API](api-use-cases.md), and [relational database](relational-db-use-cases.md) use cases. The focus is test design, semantic checks around deterministic tests, fuzz-corpus work, and diagnosis.

## Testing boundary

Jev takes a short text or JSON-text state and answers `Choice` (one of specified labels), `Noul` (probability of yes), or `Score` (position on a defined rubric). Ask one narrow judgment per question and combine answers in Go. `Choice` and `Score` provide confidence; `Noul` does not. Treat low confidence, middle-range `Noul`, `other`, and API errors as an **inconclusive review result**, not as a passing test. Set thresholds against labeled local examples. [TypeSafe primitives](https://docs.typesafe.ai/primitives); [confidence guidance](https://docs.typesafe.ai/confidence).

Go remains the test oracle for values, protocol conformance, panic behavior, schema, SQL constraints, coverage, race detection, timing, resource use, and security properties. Do not claim that a Jev judgment proves correctness. TypeSafe's [Jev 1.13 limitations](https://docs.typesafe.ai/model-jaggedness/jev-1.13) specifically caution against arithmetic, date comparisons, generated text, and assumed consistency between differently worded or differently typed questions. In particular, Jev should not replace a property assertion inside `FuzzXxx`. Go's [fuzzing guide](https://go.dev/doc/security/fuzz/) requires fast, deterministic targets independent of shared state.

**Reproducibility pattern:** Keep ordinary `go test ./...` offline. Put live Jev evaluations in an explicitly selected, credential-gated suite or a separate evaluation command. Save the input fixture, human-reviewed label, question instructions and criteria, model identifier, thresholds, and actual typed answer with each evaluation run; review changes to any of them. This freezes the *test setup*, although a remote model is still an external dependency. For unit tests of a Go adapter around Jev, inject a fake decision interface or give the SDK an `httptest` server through `WithBaseURL`/`WithHTTPClient`; an HTTP loopback URL also requires `WithAllowInsecureHTTP()`. Assert request/response plumbing and fallback behavior without contacting the API. Use live runs to measure disagreement on labeled examples, not to make one probabilistic answer the sole correctness oracle. [Go `testing`](https://pkg.go.dev/testing); [`httptest`](https://pkg.go.dev/net/http/httptest); [local SDK options](../../client.go); [TypeSafe confidence](https://docs.typesafe.ai/confidence).

## Test design and unit-test review

### 1. Link a behavior requirement to the test that claims to cover it

- **Phase/location:** PR review tool over a requirement paragraph and one `TestXxx` case description or concise code excerpt.
- **Jev question:** `Noul`: “Does this test actually exercise the behavior stated in `requirement`, including the named outcome?”
- **Go action and fallback:** Annotate likely mismatches for a reviewer; keep the requirement ID and test name in the report. On uncertainty, request review. Coverage instrumentation shows executed lines, while this judgment concerns the meaning of the behavior. An answer never marks the requirement verified by itself.
- **Evidence:** **Adaptation** of [TypeSafe's narrow verification questions](https://docs.typesafe.ai/primitives) to Go [`TestXxx` tests](https://pkg.go.dev/testing) and [coverage tooling](https://go.dev/doc/build-cover).

### 2. Find missing negative behavior in table-driven test plans

- **Phase/location:** Before implementation or during review of a table-driven `TestXxx`.
- **Jev question:** For each named failure requirement, `Noul`: “Do any of these short test-case descriptions cover rejection when `condition` holds?” Ask separately for missing credentials, malformed content, and conflicting state; do not ask Jev to count cases.
- **Go action and fallback:** Propose a specific missing row for a developer to author and assert. When uncertain, leave the gap open for review. Go still asserts the exact error type, status, or state transition.
- **Evidence:** **Adaptation** of [independent TypeSafe questions](https://docs.typesafe.ai/primitives); Go's [`testing` package](https://pkg.go.dev/testing) supports subtests and table-driven organization.

### 3. Spot semantically duplicate test cases

- **Phase/location:** Review of a large test-case table with differently worded fixture names and comments.
- **Jev question:** Given two short scenario summaries, `Choice`: “Do these exercise `the_same_behavior`, `different_behaviors`, or is there `insufficient_context`?”
- **Go action and fallback:** Surface candidate duplicates to a reviewer so they can keep, combine, or distinguish them. First compare exact inputs and assertions in Go; a Jev result does not establish that two tests are redundant, particularly when one differs at a boundary.
- **Evidence:** **Adaptation** of [bounded Choice](https://docs.typesafe.ai/primitives) to Go [subtests](https://pkg.go.dev/testing#T.Run).

### 4. Check whether a test name describes its assertion

- **Phase/location:** Static review of a `TestXxx` or `t.Run` name, setup summary, and assertion excerpt.
- **Jev question:** `Noul`: “Does `test_name` promise a behavior that `assertions` do not check?”
- **Go action and fallback:** Emit a review hint with both spans. A developer may rename the test or add an assertion. If Jev is unsure, leave it to review; the tool does not infer that passing code is correct from a good name.
- **Evidence:** **Adaptation** of [TypeSafe semantic judgment](https://docs.typesafe.ai/primitives) to the naming and failure-reporting conventions of [Go `testing`](https://pkg.go.dev/testing).

### 5. Review whether a mock fixture represents the intended scenario

- **Phase/location:** Unit-test fixture review for `httptest` or a fake repository.
- **Jev question:** `Noul`: “Does this mocked response text depict the failure described by `scenario`, rather than a different failure?”
- **Go action and fallback:** Flag likely fixture drift for a developer. Go verifies headers, status, JSON shape, calls, and retry counts exactly; Jev only checks the prose in the mock body against the scenario. Keep an inconclusive result as review-only.
- **Evidence:** **Adaptation** of [TypeSafe's focused Noul](https://docs.typesafe.ai/primitives); Go [`httptest`](https://pkg.go.dev/net/http/httptest) supplies deterministic HTTP fixtures.

### 6. Review human-readable error messages against a semantic contract

- **Phase/location:** Unit test of an error formatter whose exact wording may evolve.
- **Jev question:** `Noul`: “Does `actual_message` clearly tell the user that the API key is missing and how to supply it?” Use the specific semantic contract as state.
- **Go action and fallback:** Report a possible wording regression for review. Keep deterministic assertions on error type, wrapping, redaction, and required stable tokens; a Jev probability alone must not turn a failing contract into a pass.
- **Evidence:** **Adaptation** of [TypeSafe Noul](https://docs.typesafe.ai/primitives) to [`testing.T` assertions](https://pkg.go.dev/testing#T.Error).

### 7. Review CLI help and diagnostic output for intended meaning

- **Phase/location:** Unit or snapshot review of a command's `--help` or diagnostic text.
- **Jev question:** `Choice`: “Does this output explain `required_flag`, `optional_flag`, `both`, or `neither`?” Criteria describe the two flags precisely.
- **Go action and fallback:** Surface a semantic diff when the selected meaning changes. Exact flag names, exit status, and machine-readable output remain Go assertions. On low confidence, send the output to a reviewer rather than accepting it.
- **Evidence:** **Adaptation** of [TypeSafe Choice](https://docs.typesafe.ai/primitives) to Go [example and test functions](https://pkg.go.dev/testing#hdr-Examples).

### 8. Check meaning preserved across localized messages

- **Phase/location:** Unit test or localization review over a source message and one translated message.
- **Jev question:** `Noul`: “Does `translation` preserve the source's instruction to retry later without promising that the operation succeeded?”
- **Go action and fallback:** Flag likely semantic drift for a bilingual reviewer. Go checks placeholder names, formatting, and escape rules exactly. Jev is a screening signal, not a certification of translation quality; measure its performance for each language pair on separately labeled examples before using a threshold.
- **Evidence:** **Adaptation** of [TypeSafe's pairwise semantic judgments](https://docs.typesafe.ai/primitives) and [structured state](https://docs.typesafe.ai/concepts/state).

### 9. Evaluate a semantic router against labeled unit fixtures

- **Phase/location:** Separate live evaluation suite for an application's Jev-based router, using human-labeled phrases.
- **Jev question:** `Choice`: “Which supported action does `user_text` request?” Options are the production labels plus `other`.
- **Go action and fallback:** Aggregate a confusion matrix and abstention rate over the fixed fixture set. A label mismatch is evidence to inspect, not proof that Jev or the human label is wrong. Ordinary router unit tests use recorded answers or a fake decision interface; do not require live access for every `go test`.
- **Evidence:** **Adaptation** of TypeSafe's [intent routing pattern](https://docs.typesafe.ai/patterns/intent-routing) and [confidence calibration advice](https://docs.typesafe.ai/confidence) to Go [`testing`](https://pkg.go.dev/testing).

### 10. Detect regressions after changing question criteria or the model

- **Phase/location:** Opt-in evaluation job before deploying a revised Jev question or model identifier.
- **Jev question:** Re-run the same bounded `Choice`/`Noul`/`Score` questions used by the application on versioned labeled fixtures; for example, `Noul`: “Does this report describe a failed retry?”
- **Go action and fallback:** Compute per-class error and abstention metrics in Go and compare with a reviewed baseline. Inspect changed cases before release. Pin the evaluation's model and question version; do not compare Noul and Choice thresholds as though their probabilities were interchangeable.
- **Evidence:** **Adaptation** of [TypeSafe's confidence testing guidance](https://docs.typesafe.ai/confidence) and [Jev's structural-invariant warning](https://docs.typesafe.ai/model-jaggedness/jev-1.13) to a Go test/evaluation workflow.

## Integration and end-to-end testing

### 11. Check REST response prose against structured result fields

- **Phase/location:** `httptest` or staging API integration test after JSON and status assertions.
- **Jev question:** `Noul`: “Does `message` claim the operation succeeded while `status` and `result` say it failed?”
- **Go action and fallback:** Flag a contradiction for review; Go still asserts status code, schema, IDs, and database effects. If Jev fails or is uncertain, keep the semantic check inconclusive without hiding deterministic failures.
- **Evidence:** **Adaptation** of [TypeSafe verification](https://docs.typesafe.ai/primitives); Go [`httptest`](https://pkg.go.dev/net/http/httptest) supports HTTP integration fixtures.

### 12. Check GraphQL error prose against a stable error code

- **Phase/location:** GraphQL resolver integration test with response JSON containing `errors[].message` and an application-owned extension code.
- **Jev question:** `Choice`: “Which condition does this error message describe: `not_found`, `unauthorized`, `validation`, or `other`?”
- **Go action and fallback:** Compare the chosen meaning with the deterministic code and show a possible mismatch to a reviewer. Go checks GraphQL shape, null propagation, and code exactly; an uncertain classification does not invalidate the structural test.
- **Evidence:** **Adaptation** of [TypeSafe Choice](https://docs.typesafe.ai/primitives) to the [GraphQL error response specification](https://spec.graphql.org/September2025/#sec-Errors).

### 13. Check narrative continuity across a service workflow

- **Phase/location:** Integration test of a request crossing two or more Go services, using a small authorized subset of captured event summaries.
- **Jev question:** `Noul`: “Does the downstream event describe the same requested operation as the upstream request?” Keep IDs and timestamps as separate fields.
- **Go action and fallback:** Mark likely narrative drift for investigation. Go asserts trace IDs, ordering, delivery, and state transitions exactly; Jev does not perform identifier or time comparison. Limit state to the two relevant excerpts.
- **Evidence:** **Adaptation** of [TypeSafe structured-state questions](https://docs.typesafe.ai/primitives) and its [large-state caution](https://docs.typesafe.ai/model-jaggedness/jev-1.13); Go [integration coverage guidance](https://go.dev/doc/build-cover).

### 14. Check audit-note meaning against committed database state

- **Phase/location:** PostgreSQL integration test after a transaction changes a row and writes an audit note.
- **Jev question:** `Noul`: “Does `audit_note` say the request was rejected even though `committed_status` is approved?”
- **Go action and fallback:** Raise a possible semantic mismatch for review. The test directly queries and compares stored rows, constraints, and transaction outcome. On uncertainty, do not infer what the database contains from the note.
- **Evidence:** **Adaptation** of [TypeSafe Noul](https://docs.typesafe.ai/primitives) to [PostgreSQL transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html).

### 15. Check webhook narrative routing in an end-to-end fixture

- **Phase/location:** Integration test of a verified webhook adapter with a legacy payload that carries a free-text change description.
- **Jev question:** `Choice`: “Does `description` report `created`, `modified`, `removed`, or `other`?”
- **Go action and fallback:** Compare the route selected by the Go adapter with a human-labeled fixture and inspect disagreement. Signature verification, deduplication, and resulting state are asserted deterministically. Keep the live Jev portion opt-in; use a fake decision in routine offline tests.
- **Evidence:** **Adaptation** of [TypeSafe intent routing](https://docs.typesafe.ai/patterns/intent-routing) to Go [`httptest` integration](https://pkg.go.dev/net/http/httptest).

## Fuzzing support outside the fuzz target

### 16. Curate fuzz seeds from natural-language bug reports

- **Phase/location:** Offline corpus-preparation tool before running `FuzzXxx`.
- **Jev question:** `Choice`: “Which known parser boundary does this report describe: `empty_input`, `invalid_utf8`, `nested_delimiter`, `oversized_field`, or `other`?”
- **Go action and fallback:** Match a label to an existing, reviewed seed fixture, then add that exact byte sequence with `f.Add` or under `testdata/fuzz/<Name>`. Jev does not invent arbitrary bytes. Unclear reports go to manual seed design; the fuzz target retains deterministic assertions.
- **Evidence:** **Adaptation** of [TypeSafe Choice](https://docs.typesafe.ai/primitives) to Go's [seed corpus workflow](https://go.dev/doc/security/fuzz/) and [Jev's generation limitation](https://docs.typesafe.ai/model-jaggedness/jev-1.13).

### 17. Classify a minimized fuzz failure for triage

- **Phase/location:** After the fuzz run, once Go has saved a reproducing input and deterministic failure output.
- **Jev question:** `Choice`: “What does this short failure report describe: `panic`, `round_trip_mismatch`, `unexpected_error`, `timeout`, or `other`?”
- **Go action and fallback:** Route the saved artifact to the relevant owner and attach the label as advisory metadata. Go replays the corpus entry and proves the failure still occurs; Jev neither discovers nor confirms the bug. On uncertainty, leave it unclassified.
- **Evidence:** **Adaptation** of [TypeSafe's classification primitive](https://docs.typesafe.ai/primitives) to Go's [fuzz failure and replay process](https://pkg.go.dev/testing#hdr-Fuzzing).

### 18. Group reports of fuzz failures that describe the same symptom

- **Phase/location:** Offline triage of multiple reproducible corpus entries, after exact stack/hash grouping.
- **Jev question:** For a pair of short failure summaries, `Choice`: “Do these describe `same_symptom`, `different_symptoms`, or `unclear`?”
- **Go action and fallback:** Suggest a cluster to a reviewer; retain every corpus entry until Go tests show it is safely redundant. A shared symptom can arise from different defects, and a Jev label must never delete a regression input automatically.
- **Evidence:** **Adaptation** of [TypeSafe bounded Choice](https://docs.typesafe.ai/primitives) to Go's [persistent fuzz corpus](https://go.dev/doc/security/fuzz/).

## Test-run diagnosis and maintenance

### 19. Triage intermittent `go test` failures from logs

- **Phase/location:** CI post-processing after a failed `go test -json` run, especially when reruns disagree.
- **Jev question:** `Choice`: “Does this short failure excerpt most clearly indicate `assertion`, `timeout`, `external_dependency`, `race_report`, or `other`?”
- **Go action and fallback:** Attach a tentative label and compare repeated runs; preserve raw logs and exit codes. The label does not waive a failure or establish flakiness. Go's race detector and repeated deterministic runs remain the evidence for concurrency defects.
- **Evidence:** **Adaptation** of [TypeSafe classification](https://docs.typesafe.ai/primitives) to Go [`go test` output](https://pkg.go.dev/cmd/go#hdr-Test_packages) and the [race detector](https://go.dev/doc/articles/race_detector).

### 20. Review surviving code mutations against intended behavior

- **Phase/location:** Offline mutation-testing review after a tool or developer changes one condition and the existing Go suite still passes.
- **Jev question:** `Noul`: “Does this small mutation appear to change the behavior promised by `requirement`?” Supply the requirement and focused before/after excerpt, not an entire repository.
- **Go action and fallback:** Prioritize likely material survivors for a developer to turn into a failing test. Compilation, test execution, and the new regression assertion remain deterministic; a Jev judgment cannot prove the mutant equivalent or the suite complete. Send uncertain cases to review.
- **Evidence:** **Adaptation** of [TypeSafe's focused verification questions](https://docs.typesafe.ai/primitives) and [small-state guidance](https://docs.typesafe.ai/model-jaggedness/jev-1.13) to Go [`testing`](https://pkg.go.dev/testing).

## Practical order of adoption

Start with review-only cases such as 1, 5, 17, or 19 and measure agreement with developers on a labeled sample. Then use 9 or 10 to evaluate a Jev-backed application feature before release. Keep all routine unit and fuzz tests reproducible without network access; record Jev inputs and answer versions when the optional live suite runs. Never use a single probabilistic classification to suppress a failing Go test or discard a fuzz counterexample. [TypeSafe confidence](https://docs.typesafe.ai/confidence); [Go fuzzing](https://go.dev/doc/security/fuzz/).
