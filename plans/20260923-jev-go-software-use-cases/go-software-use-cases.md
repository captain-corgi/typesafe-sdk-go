# Jev in Go software: 20 use cases

Research date: 2026-09-23. Scope: ways to use Jev while writing and operating a conventional Go application, rather than industry-specific business workflows. Items 1–5 carry forward the five programming patterns discussed earlier; items 6–20 add fifteen more.

## The programming boundary

Jev evaluates a `state` and narrow questions. A `Choice` returns one label from a set supplied by the caller plus probabilities and confidence; a `Noul` returns the probability of a yes/no statement; a `Score` returns a position on an ordered rubric plus probabilities and confidence. The Go program still owns the `if`, `switch`, function calls, validation, authorization, and side effects. This is [TypeSafe's recommended architecture](https://docs.typesafe.ai/concepts/how-to-build-with-system-one), not a way to execute Go code through a model. See the [primitive reference](https://docs.typesafe.ai/primitives) and [System One overview](https://docs.typesafe.ai/concepts/system-one).

Use ordinary Go for exact predicates: `err != nil`, status codes, permissions, arithmetic, parsing known formats, and comparisons of already structured fields. Jev is useful when the missing predicate is semantic: *what does this text mean, which known option fits, or how strongly does this evidence support a claim?* Go's [`go/ast`](https://pkg.go.dev/go/ast) and [`regexp`](https://pkg.go.dev/regexp) should still handle syntax and literal patterns where those suffice.

The TypeSafe sources below document the model patterns, often in Python. The Go-specific applications marked **adaptation** are proposals derived from those patterns; they are not claims that TypeSafe published or measured that exact Go workflow. Repository examples are runnable Go cookbook programs that call the live API when `TYPESAFE_API_KEY` is set.

### Shared Go integration shape

Create one `*typesafe.Client` at application startup, then pass the request or job `context.Context` to `SystemOne`. Put an `other` or `none` option in a `Choice` when inputs may fall outside the known set. Apply a reviewed fallback when a request fails or a result is uncertain. Thresholds in examples are illustrative; [TypeSafe recommends measuring them on your own data](https://docs.typesafe.ai/confidence).

```go
resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
    State: input,
    Questions: typesafe.Questions{
        "action": typesafe.Choice{
            Instructions: "Which supported operation does this request ask for?",
            Criteria: typesafe.ChoiceCriteria{
                "search": "Find existing documentation",
                "status": "Show the current system status",
                "other":  "No supported operation fits",
            },
        },
    },
})
if err != nil {
    return err
}
action := resp.Choices()["action"]
if action.Choice == "other" || action.Confidence < reviewThreshold {
    return errNeedsClarification
}
switch action.Choice {
case "search":
    return searchDocs(ctx)
case "status":
    return showStatus(ctx)
default:
    return errNeedsClarification
}
```

This is an integration sketch: `client`, `input`, `reviewThreshold`, `errNeedsClarification`, and the handlers are supplied by the host app. The [repository quickstart](../../examples/quickstart/main.go) shows client setup.

## Original five software patterns

### 1. Semantic `switch` over natural-language commands

- **Go location:** HTTP handler, CLI command parser, or chat command adapter.
- **Jev decision:** `Choice` among a bounded set such as `search_docs`, `show_status`, `explain_config`, and `other`. The state is the user's command, perhaps with a short list of available capabilities.
- **Go action:** Check confidence and dispatch with a `switch` or `map[string]func(...)`. This can replace a growing keyword/regex classifier, while the handlers remain ordinary Go functions.
- **Evidence:** [Intent routing](https://docs.typesafe.ai/patterns/intent-routing); [Go routing example](../../examples/task-categories/routing/main.go). The command labels above are a Go adaptation.

### 2. Bind closed-set arguments to existing Go functions

- **Go location:** Command adapter in front of a fixed function registry.
- **Jev decision:** A `Choice` selects an allowlisted function; separate `Choice` questions select enum arguments, and `Noul` questions decide optional flags or whether an argument was stated. For example, “show hourly errors for last week” can select `show_errors`, `resolution=hour`, `window=week`.
- **Go action:** Validate each selected enum against the target function's accepted values, leave unstated optional arguments at their Go defaults, and invoke the function. Numeric quantities and arbitrary strings need code-side parsing or another bounded extraction step.
- **Evidence:** [Function calling cookbook](https://docs.typesafe.ai/cookbooks/function_calling). Go adaptation of the documented closed-set dispatch pattern.

### 3. Semantic lint in a Go CI command

- **Go location:** PR checker that reads changed Go code and the team's written conventions.
- **Jev decision:** A battery of `Noul` questions about contextual conventions, such as whether a new path bypasses the usual validation layer, plus a `Score` for severity.
- **Go action:** Post findings for review or fail a narrowly scoped policy check after calibration. Keep `gofmt`, `go vet`, compiler checks, and AST-based rules for exact violations.
- **Evidence:** [Semantic code linting in TypeSafe's use-case map](https://docs.typesafe.ai/concepts/use-case-map); [repository example](../../examples/automation-use-cases/semantic-code-linting/main.go). The repository sample evaluates Python code; sending a Go diff is an adaptation.

### 4. Select a runtime strategy from several qualitative signals

- **Go location:** Worker dispatcher or LLM gateway written in Go.
- **Jev decision:** Ask `Choice` for task family, `Score` for difficulty, and `Noul` for whether multi-step reasoning is needed in one request.
- **Go action:** A routing table chooses the fast path, standard path, specialist model, or review path. The cost, capacity, and hard policy constraints stay as deterministic Go conditions.
- **Evidence:** [Model routing use case](https://docs.typesafe.ai/concepts/use-case-map); [Go model-routing example](../../examples/automation-use-cases/model-routing/main.go).

### 5. Rerank a search function's shortlist

- **Go location:** Documentation or source-search service after its existing lexical/index lookup.
- **Jev decision:** A `Noul` or `Score` evaluates how directly each candidate passage answers the query.
- **Go action:** Sort the shortlist by the returned signal and keep the top items. Jev cannot recover a document omitted by the initial search; use the index to produce candidates first.
- **Evidence:** [Re-ranking cookbook](https://docs.typesafe.ai/cookbooks/rerank_typesafe); [Go search and retrieval example](../../examples/automation-use-cases/search-and-retrieval/main.go). Applying it to source-code documentation is an adaptation.

## Fifteen additional software patterns

### 6. Classify CI failures before running diagnostics

- **Go location:** CI webhook receiver or a command that consumes the failing job's log excerpt.
- **Jev decision:** `Choice` among `compile`, `test_assertion`, `dependency`, `environment`, `timeout`, and `other`; optionally a `Noul` for “does this look intermittent?”
- **Go action:** Select a diagnostic runbook, attach a label, or request a rerun. Do not let the classification erase the original log or substitute for the actual exit status.
- **Evidence:** **Adaptation** of [TypeSafe's classification primitive](https://docs.typesafe.ai/primitives) and [routing pattern](https://docs.typesafe.ai/patterns/intent-routing).

### 7. Correlate duplicate bug or incident reports

- **Go location:** Issue-ingestion worker after a cheap title, tag, or full-text shortlist.
- **Jev decision:** Compare a new report with one candidate in structured state. A three-level `Score` says `different`, `possibly_same`, or `same`; companion `Noul` questions check whether symptoms, environment, and reproduction steps agree.
- **Go action:** Suggest a duplicate link, or queue ambiguous pairs for review. Preserve issue IDs and exact versions in Go; do not merge records solely on a model result.
- **Evidence:** **Adaptation** of the [entity-alignment cookbook](https://docs.typesafe.ai/cookbooks/entity_alignment), which uses a shortlist, an ordered outcome, and companion yes/no signals.

### 8. Compare acceptance criteria with tests

- **Go location:** PR review helper that receives one requirement and a shortlist of relevant test names/bodies.
- **Jev decision:** One `Noul` per requirement–test pair asks whether the test meaningfully exercises the stated behavior; another can ask whether it covers the failure path.
- **Go action:** Report likely gaps with links to the exact tests. A positive answer is evidence for a reviewer, not proof of coverage; test execution and coverage measurement remain in Go tooling.
- **Evidence:** **Adaptation** of [verification questions](https://docs.typesafe.ai/primitives) and [independent question composition](https://docs.typesafe.ai/concepts/how-to-build-with-system-one).

### 9. Add semantic reviewer routing to path-based ownership

- **Go location:** PR bot after deterministic CODEOWNERS matching.
- **Jev decision:** `Choice` over cross-cutting concerns such as `api_contract`, `security_boundary`, `performance`, `observability`, and `other`, using a focused diff summary as state.
- **Go action:** Add an extra reviewer or checklist for the selected concern. Existing path-based owner requirements remain authoritative; a model result should not remove required reviewers.
- **Evidence:** **Adaptation** of [intent routing](https://docs.typesafe.ai/patterns/intent-routing) and [TypeSafe's semantic code-linting use case](https://docs.typesafe.ai/concepts/use-case-map).

### 10. Normalize release-note prose into a bounded change record

- **Go location:** Release tooling that reads manually written change notes.
- **Jev decision:** `Choice` for `feature`, `fix`, `breaking_change`, or `other`; `Noul` for “does this require a migration step?”; optional `Score` for user-visible impact.
- **Go action:** Store a typed record and draft changelog sections. Go or a human decides the actual version and release contents; Jev does not invent missing migration instructions.
- **Evidence:** **Adaptation** of [structured-data extraction](https://docs.typesafe.ai/concepts/use-case-map) and the [repository's typed extraction example](../../examples/task-categories/structured-data-extraction/main.go).

### 11. Select the relevant ID from a noisy log without inventing it

- **Go location:** Incident or build-log parser when several commit SHAs, build IDs, or issue IDs appear in one report.
- **Jev decision:** Go `regexp` first extracts candidate strings. A `Choice` asks which candidate is the failed build, rollback target, or cited commit; include `none`.
- **Go action:** Copy the selected candidate verbatim and verify it against the repository or build database. The model supplies the role, while Go retains the exact bytes.
- **Evidence:** **Adaptation** of [pre-parsed value extraction](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook); Go's [`regexp` package](https://pkg.go.dev/regexp).

### 12. Parse relative dates in developer commands

- **Go location:** A CLI or internal scheduler accepting “rerun this next Thursday” or “expire the branch on Friday.”
- **Jev decision:** Separate `Choice` questions identify whether a date is present, weekday, week offset, and other bounded date parts.
- **Go action:** Resolve the parts against an explicit clock and timezone, construct a `time.Time`, validate it, and ask for clarification when parts are uncertain. Exact ISO dates can go straight to Go's time parser.
- **Evidence:** **Adaptation** of the [date-extraction cookbook](https://docs.typesafe.ai/cookbooks/date_extraction_cookbook), which leaves relative-date arithmetic to code.

### 13. Navigate a large codebase hierarchy from a description

- **Go location:** Developer search or repository browser with a known directory/module tree.
- **Jev decision:** `Choice` selects a likely child at each level from directory descriptions; retain more than one plausible path when an early choice is uncertain.
- **Go action:** Traverse existing paths and present candidate files or packages. It never creates a path from free text; actual filesystem lookup confirms every candidate.
- **Evidence:** **Adaptation** of [hierarchical classification](https://docs.typesafe.ai/cookbooks/hierarchical_classification), whose published examples include a source-code hierarchy.

### 14. Suggest a tool, plugin, or agent skill from a registry

- **Go location:** Agent harness or developer assistant written in Go.
- **Jev decision:** A `Choice` ranks tool or skill descriptions; `Noul` questions decide whether any tool is needed and whether the leading candidates actually fit. For a large catalog, rerank a shortlist using fuller descriptions.
- **Go action:** Load or propose the selected capability only if it is installed and allowed for the current request. Keep the actual permission and execution checks in Go.
- **Evidence:** [Skill-suggestion cookbook](https://docs.typesafe.ai/cookbooks/skill_suggestion). Mapping its dispatcher into Go is an adaptation.

### 15. Screen untrusted text before an LLM call

- **Go location:** Middleware around an LLM-backed endpoint.
- **Jev decision:** `Noul` questions detect attempts to override instructions, solicit restricted behavior, or expose sensitive data; a `Score` rates severity.
- **Go action:** Pass, review, or reject based on a reviewed policy table. This is a semantic signal, not an authorization mechanism; exact permissions and data access remain deterministic.
- **Evidence:** [Guardrails cookbook](https://docs.typesafe.ai/cookbooks/llm_guardrails); [Go guardrails example](../../examples/automation-use-cases/llm-guardrails/main.go).

### 16. Check generated output before returning it

- **Go location:** The response path after an LLM or template-assisted generator.
- **Jev decision:** Ask whether the draft contains a forbidden claim, private information, or advice outside the application's scope, and rate the possible harm. The questions must describe the output side of the policy.
- **Go action:** Return, hold for review, or replace the draft with a fixed fallback. Jev classifies the output; it does not rewrite or sanitize the text.
- **Evidence:** The [guardrails cookbook](https://docs.typesafe.ai/cookbooks/llm_guardrails) explicitly checks both LLM inputs and outputs. Go integration follows the [repository example](../../examples/automation-use-cases/llm-guardrails/main.go).

### 17. Filter retrieved context before generation

- **Go location:** A Go RAG pipeline between retrieval and prompt assembly.
- **Jev decision:** For each query–passage pair, ask independent `Noul` questions: relevant, contains useful evidence, contradicts the query's assumption, or attempts to instruct the model.
- **Go action:** Put useful evidence and contradictions into separate context blocks and drop irrelevant or suspicious passages. This is different from use case 5, which only orders candidates.
- **Evidence:** [Classifying RAG passages cookbook](https://docs.typesafe.ai/cookbooks/classifying_rag_passages). Go adaptation of its passage-routing step.

### 18. Verify a proposed tool call against the user's request

- **Go location:** Agent tool-execution boundary or audit processor.
- **Jev decision:** Send the user request, proposed function name, and bounded arguments as structured state. `Noul` questions ask whether the call addresses the request and whether an argument changes the requested scope; a `Choice` can label the mismatch.
- **Go action:** Flag or pause ambiguous calls and retain the full trace for review. Validate the function name, types, paths, permissions, and side-effect authorization with deterministic Go checks.
- **Evidence:** **Adaptation** of [TypeSafe's tool-call-trace verification guidance](https://docs.typesafe.ai/concepts/how-to-build-with-system-one) and [LLM guardrail patterns](https://docs.typesafe.ai/cookbooks/llm_guardrails).

### 19. Check citations in generated technical documentation

- **Go location:** Documentation publishing pipeline or assistant response validator.
- **Jev decision:** After Go confirms that a quoted span exists in the referenced source, a `Choice` classifies the relationship between the claim and surrounding passage as `supports`, `contradicts`, or `says_nothing`.
- **Go action:** Publish supported claims, flag uncertain ones, and reject missing quotes by string matching without a model call. Keep source location and version attached to each verdict.
- **Evidence:** [Citation-check cookbook](https://docs.typesafe.ai/cookbooks/citation_check); [Go verification example](../../examples/task-categories/verification/main.go). Using this for API docs is an adaptation.

### 20. Produce semantic features for incident analytics

- **Go location:** Offline pipeline that converts incident narratives or traces into numeric columns for a statistical classifier or dashboard.
- **Jev decision:** `Noul` probabilities for properties such as “mentions a dependency regression” or “describes customer-visible failure,” plus a normalized `Score` for impact.
- **Go action:** Store a versioned feature schema and join the semantic columns with measured facts such as duration, error rate, and affected service. Train or aggregate in ordinary code; do not ask Jev to calculate those numbers.
- **Evidence:** **Adaptation** of [TypeSafe's feature-extraction use case](https://docs.typesafe.ai/concepts/use-case-map) and [Go feature-extraction example](../../examples/task-categories/ml-feature-extraction/main.go).

## Implementation guidance across the 20 cases

1. **Start with a measurable semantic gap.** Compare a simple deterministic baseline against labeled examples. Jev is valuable when wording variation defeats that baseline; the model call is unnecessary for an exact Go predicate.
2. **Make outputs bounded.** Define all `Choice` options, including an escape hatch; make `Score` levels descriptive; ask one narrow yes/no judgment per `Noul`. [Primitive design guidance](https://docs.typesafe.ai/primitives).
3. **Keep the control policy inspectable.** Ask independent questions in one `SystemOne` call when they share state, then combine results in Go. Use Go tables and thresholds for the final action. [Composition guide](https://docs.typesafe.ai/concepts/how-to-build-with-system-one); [speculative fan-out](https://docs.typesafe.ai/patterns/fan-out).
4. **Treat uncertainty as a program input.** `Choice` and `Score` include confidence; `Noul` is itself a probability and has no separate confidence field. Use labeled examples to set thresholds, and preserve an error/uncertain path. [Primitive answer shapes](https://docs.typesafe.ai/primitives); [confidence guidance](https://docs.typesafe.ai/confidence).
5. **Keep external calls scoped.** Pass the request/job context to the SDK, send only the relevant text, and account for API latency and failure in synchronous Go handlers. Jev currently evaluates text and JSON text records; image, audio, and video need another processing step. [Go `context` package](https://pkg.go.dev/context); [System One overview](https://docs.typesafe.ai/concepts/system-one).

## Source scope

The primary research sources are [TypeSafe's documentation index](https://docs.typesafe.ai/llms.txt), [architecture guide](https://docs.typesafe.ai/concepts/how-to-build-with-system-one), [primitives](https://docs.typesafe.ai/primitives), the linked TypeSafe patterns and cookbooks above, and the Go standard library references. Local examples demonstrate this repository's API surface. No model-quality measurements for the proposed Go adaptations are claimed here.
