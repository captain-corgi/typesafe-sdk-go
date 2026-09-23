# TypeSafe AI SDK for Go — Examples

Copy-paste starting points: from `go get` to a working use case in minutes.
Every example is a single `package main` program using **only the Go standard
library plus this SDK**, and every one runs against the **live API** — set
`TYPESAFE_API_KEY` first:

```sh
export TYPESAFE_API_KEY=ts_live_...
go run ./examples/quickstart
```

Without a key, each example prints a friendly hint and exits. Offline
behavior is the test suite's job, not the examples'.

The [`automation-use-cases/`](automation-use-cases) and
[`task-categories/`](task-categories) catalogs mirror the docs
[use-case map](https://docs.typesafe.ai/concepts/use-case-map) one-for-one.
Four more catalogs implement the 60 proposed Go patterns in
[`plans/20260923-jev-go-software-use-cases/`](../plans/20260923-jev-go-software-use-cases/):
general Go software, REST and GraphQL APIs, relational databases, and testing.
Two basics stay at the root.

## Basics

| Example | Mirrors (docs page) | Demonstrates |
|---|---|---|
| [`quickstart`](quickstart) | Quickstart | Minimal client from env; one call with all three primitives; typed answers, probabilities, confidence, usage |
| [`retries-errors`](retries-errors) | SDK usage docs | `errors.As` across the taxonomy (incl. `RateLimitError.RetryAfterMs`); custom `RetryPolicy`; per-call `Model`/`Timeout`/`ExtraHeaders`; `TYPESAFE_LOG_LEVEL=debug` |

## Automation use cases

One directory per entry of the docs
[example automation use cases](https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases)
list, under [`automation-use-cases/`](automation-use-cases).

| Example | Docs use case | Demonstrates |
|---|---|---|
| [`search-and-retrieval`](automation-use-cases/search-and-retrieval) | Search and retrieval | Per-passage `Noul` answer gate + relevance `Score`; ranked context window for RAG |
| [`scientific-discovery`](automation-use-cases/scientific-discovery) | Scientific discovery | Inclusion/exclusion `Noul` battery + reporting check + relevance `Score`; include / exclude / review verdict |
| [`model-routing`](automation-use-cases/model-routing) | Model routing | Intent `Choice` + difficulty `Score` + reasoning `Noul`; cheapest-model routing table with a confidence gate |
| [`llm-guardrails`](automation-use-cases/llm-guardrails) | LLM guardrails | `Noul` hazard battery + harm-severity `Score`; strict/permissive threshold policies; block/review/support/pass routing with precedence |
| [`semantic-code-linting`](automation-use-cases/semantic-code-linting) | Semantic code linting | Team-convention `Noul` battery + severity `Score`; CI-style findings with a nonzero exit on violation |
| [`predictive-features`](automation-use-cases/predictive-features) | Feature extraction for predictive modeling | Free text → feature map of `Noul`s + normalized sentiment `Score`; deterministic feature row for a classical model |
| [`recruiting`](automation-use-cases/recruiting) | Recruiting | Competency `Score`s + hard-requirement `Noul`s + recommendation `Choice`; rubric and model merged conservatively |
| [`lead-generation`](automation-use-cases/lead-generation) | Lead generation | Structured map state; multiple `Score` answers merged into a weighted composite and priority buckets |
| [`customer-support`](automation-use-cases/customer-support) | Customer support | Six questions in one call (incl. a `RawQuestion` passthrough); Go decision tree — escalate bugs, flag refunds, priority SLA |
| [`insurance-claims`](automation-use-cases/insurance-claims) | Insurance claims | FNOL `Choice` + complexity `Score` + missing-info/fraud `Noul`s; straight-through / adjuster review / SIU referral |
| [`financial-crime`](automation-use-cases/financial-crime) | Financial crime | Typology `Noul`, entity-resolution `Noul`, alert `Choice`, risk `Score`; prioritized investigator routing |
| [`legal-compliance`](automation-use-cases/legal-compliance) | Legal and compliance | Prohibited-claim `Noul` battery + missing-disclaimer check + severity `Score`; approve / revise / escalate to legal |
| [`ecommerce-marketplaces`](automation-use-cases/ecommerce-marketplaces) | E-commerce marketplaces | Category/condition normalization `Choice`s, prohibited/counterfeit `Noul`s; publish / hold / remove |
| [`moderation-trust-safety`](automation-use-cases/moderation-trust-safety) | Moderation / trust & safety | Company-specific T&S `Noul` battery + severity `Score`; allow/warn/review/block policy matrix |
| [`advertising`](automation-use-cases/advertising) | Advertising | Brand-safety, claims, and ad-page-alignment `Noul`s + creative `Score` + audience-fit `Choice` |
| [`gaming`](automation-use-cases/gaming) | Gaming | Credible-cheating and toxic-chat `Noul`s + frustration `Score` + churn `Noul`; sorted action list |
| [`risk-assessment`](automation-use-cases/risk-assessment) | Risk assessment | Risk-type `Choice`, four-level severity `Score`, control/recurrence `Noul`s; priority register entry |
| [`demand-forecasting`](automation-use-cases/demand-forecasting) | Demand forecasting | Per-note demand signals map-reduced into an aggregate forecast adjustment with risk flags |
| [`knowledge-graphs`](automation-use-cases/knowledge-graphs) | Graphs and knowledge graphs | Relation `Choice` + contradiction `Noul` per subject-object triple; commit / flag / queue with a confidence gate |

## Task categories

One directory per row of the docs
[example task categories](https://docs.typesafe.ai/concepts/use-case-map#example-task-categories)
table, under [`task-categories/`](task-categories).

| Example | Docs category | When to use | Demonstrates |
|---|---|---|---|
| [`classification`](task-categories/classification) | Classification | One category wins | Topic `Choice` with sorted probabilities; confidence gate between auto-tagging and a human queue |
| [`detection`](task-categories/detection) | Detection | Probability one property is present | Spam/phishing/promotional `Noul` battery with per-signal thresholds |
| [`scoring`](task-categories/scoring) | Scoring | Ordered rubric answer | Response-quality `Score` (with rubric probabilities) + resolves `Noul`; ticket next step |
| [`routing`](task-categories/routing) | Routing | Category picks next code path | Voice-banking intent `Choice`; confidence gates — < 0.6 human agent, ≥ 0.85 auto-acts, in between confirm |
| [`search`](task-categories/search) | Search | Find items matching an NL query | Per-item match `Noul` with explicit criteria; threshold turns probabilities into a matched set |
| [`retrieval`](task-categories/retrieval) | Retrieval | Workflow needs relevant context | Per-passage relevance `Score`; best-first packing of a RAG context block under a word budget |
| [`ranking`](task-categories/ranking) | Ranking | Order by semantic relevance/quality | Map-reduce re-ranking: per-passage relevance `Score`, sort, top-k reduce |
| [`verification`](task-categories/verification) | Verification | Check artifact for failure modes | Citation-support `Noul` + support-strength `Score` + overstatement check; verified / review / FAIL |
| [`ml-feature-extraction`](task-categories/ml-feature-extraction) | ML feature extraction | Downstream classical ML needs semantic signals | Review → feature map emitted as one deterministic model-input row |
| [`structured-data-extraction`](task-categories/structured-data-extraction) | Structured data extraction | Recover known fields from unstructured input | Enum fields as `Choice`s, booleans as `Noul`s, buckets as `Score`s; low-confidence fields flagged for review |

## Go software use cases

These programs implement the 20 proposed patterns in the
[Go software plan](../plans/20260923-jev-go-software-use-cases/go-software-use-cases.md).
The Go program keeps dispatch, validation, and side effects; Jev answers
bounded questions about meaning.

| # | Example | Pattern |
|---|---|---|
| 1 | [`01-semantic-command-switch`](go-software-use-cases/01-semantic-command-switch) | Route natural-language commands with a semantic `switch` |
| 2 | [`02-bind-closed-set-arguments`](go-software-use-cases/02-bind-closed-set-arguments) | Bind allowlisted functions and enum arguments |
| 3 | [`03-semantic-lint`](go-software-use-cases/03-semantic-lint) | Review Go diffs against team conventions |
| 4 | [`04-select-runtime-strategy`](go-software-use-cases/04-select-runtime-strategy) | Select a runtime strategy from several signals |
| 5 | [`05-rerank-search-shortlist`](go-software-use-cases/05-rerank-search-shortlist) | Rerank a search shortlist |
| 6 | [`06-classify-ci-failures`](go-software-use-cases/06-classify-ci-failures) | Classify CI failures before diagnostics |
| 7 | [`07-correlate-duplicate-reports`](go-software-use-cases/07-correlate-duplicate-reports) | Suggest duplicate bug or incident reports |
| 8 | [`08-compare-acceptance-tests`](go-software-use-cases/08-compare-acceptance-tests) | Compare acceptance criteria with tests |
| 9 | [`09-semantic-reviewer-routing`](go-software-use-cases/09-semantic-reviewer-routing) | Add a semantic reviewer to path-based ownership |
| 10 | [`10-normalize-release-notes`](go-software-use-cases/10-normalize-release-notes) | Normalize release notes into bounded records |
| 11 | [`11-select-id-from-log`](go-software-use-cases/11-select-id-from-log) | Select an exact ID extracted from a log |
| 12 | [`12-parse-relative-dates`](go-software-use-cases/12-parse-relative-dates) | Resolve relative dates against a Go clock |
| 13 | [`13-navigate-codebase`](go-software-use-cases/13-navigate-codebase) | Navigate an existing codebase hierarchy |
| 14 | [`14-suggest-tool`](go-software-use-cases/14-suggest-tool) | Suggest an installed tool from a registry |
| 15 | [`15-screen-untrusted-input`](go-software-use-cases/15-screen-untrusted-input) | Screen untrusted text before an LLM call |
| 16 | [`16-check-generated-output`](go-software-use-cases/16-check-generated-output) | Check generated output before returning it |
| 17 | [`17-filter-retrieved-context`](go-software-use-cases/17-filter-retrieved-context) | Filter retrieved context before generation |
| 18 | [`18-verify-proposed-tool-call`](go-software-use-cases/18-verify-proposed-tool-call) | Check a proposed tool call against the request |
| 19 | [`19-check-documentation-citations`](go-software-use-cases/19-check-documentation-citations) | Check citations in generated documentation |
| 20 | [`20-incident-semantic-features`](go-software-use-cases/20-incident-semantic-features) | Produce features for incident analytics |

## API use cases

These programs implement the ten proposed REST and GraphQL patterns in the
[API plan](../plans/20260923-jev-go-software-use-cases/api-use-cases.md).
They demonstrate where a semantic decision fits after the usual request,
schema, and authorization checks.

| # | Example | Pattern |
|---|---|---|
| 1 | [`narrative-field-agreement`](api-use-cases/narrative-field-agreement) | Check a request narrative against structured fields |
| 2 | [`natural-language-filters`](api-use-cases/natural-language-filters) | Translate a search hint into bounded filters |
| 3 | [`legacy-webhook-routing`](api-use-cases/legacy-webhook-routing) | Route a verified legacy webhook with an underspecified payload |
| 4 | [`upstream-problem-types`](api-use-cases/upstream-problem-types) | Map upstream error prose to stable problem types |
| 5 | [`openapi-description-drift`](api-use-cases/openapi-description-drift) | Review semantic drift in an OpenAPI change |
| 6 | [`graphql-report-category`](api-use-cases/graphql-report-category) | Resolve a GraphQL enum from stored prose |
| 7 | [`graphql-legacy-union`](api-use-cases/graphql-legacy-union) | Resolve a union for legacy records |
| 8 | [`graphql-claim-predicate`](api-use-cases/graphql-claim-predicate) | Add a semantic predicate to a query result |
| 9 | [`graphql-topic-subscription`](api-use-cases/graphql-topic-subscription) | Filter a subscription stream by event meaning |
| 10 | [`graphql-deprecated-field`](api-use-cases/graphql-deprecated-field) | Suggest replacements for deprecated fields |

## Relational database use cases

These programs illustrate the ten proposed PostgreSQL patterns in the
[relational database plan](../plans/20260923-jev-go-software-use-cases/relational-db-use-cases.md).
They call Jev live, then show the allowlisted SQL and arguments an application
would pass to its own database driver. They do not require a database driver.

| # | Example | Pattern |
|---|---|---|
| 1 | [`allowlisted-select`](relational-db-use-cases/allowlisted-select) | Natural-language filters to a tenant-scoped, parameterized `SELECT` |
| 2 | [`controlled-dimension`](relational-db-use-cases/controlled-dimension) | Normalize prose to an existing dimension row |
| 3 | [`semantic-tags`](relational-db-use-cases/semantic-tags) | Populate a many-to-many tag table |
| 4 | [`semantic-sort-column`](relational-db-use-cases/semantic-sort-column) | Materialize an ordered semantic column |
| 5 | [`entity-alignment`](relational-db-use-cases/entity-alignment) | Propose matches before an import merge |
| 6 | [`existing-foreign-key`](relational-db-use-cases/existing-foreign-key) | Resolve prose to an existing foreign-key target |
| 7 | [`typed-join-relation`](relational-db-use-cases/typed-join-relation) | Label a relationship represented by a join row |
| 8 | [`structured-update-guard`](relational-db-use-cases/structured-update-guard) | Check a prose update against structured columns |
| 9 | [`version-contradictions`](relational-db-use-cases/version-contradictions) | Flag contradictions across versioned rows |
| 10 | [`migration-worklist`](relational-db-use-cases/migration-worklist) | Build a review worklist for a data migration |

## Testing use cases

These are opt-in live evaluation and review commands for the 20 proposed
patterns in the [testing plan](../plans/20260923-jev-go-software-use-cases/testing-use-cases.md).
Routine `go test` and fuzz targets stay offline and deterministic. A semantic
answer can flag a case for review, but cannot make a failing test pass.

| # | Example | Pattern |
|---|---|---|
| 1 | [`requirement-coverage-review`](testing-use-cases/requirement-coverage-review) | Link a behavior requirement to its test |
| 2 | [`negative-case-gaps`](testing-use-cases/negative-case-gaps) | Find missing negative cases in a test plan |
| 3 | [`duplicate-scenarios`](testing-use-cases/duplicate-scenarios) | Suggest semantically duplicate cases |
| 4 | [`test-name-assertions`](testing-use-cases/test-name-assertions) | Compare a test name with its assertions |
| 5 | [`mock-fixture-intent`](testing-use-cases/mock-fixture-intent) | Check that a mock fixture depicts the intended scenario |
| 6 | [`error-message-contract`](testing-use-cases/error-message-contract) | Review error prose against a semantic contract |
| 7 | [`cli-help-meaning`](testing-use-cases/cli-help-meaning) | Review CLI help and diagnostics |
| 8 | [`localized-message`](testing-use-cases/localized-message) | Check meaning across localized messages |
| 9 | [`router-fixture-evaluation`](testing-use-cases/router-fixture-evaluation) | Evaluate a router on labeled fixtures |
| 10 | [`question-regression-evaluation`](testing-use-cases/question-regression-evaluation) | Compare question or model changes with a baseline |
| 11 | [`rest-message-result`](testing-use-cases/rest-message-result) | Compare REST response prose with result fields |
| 12 | [`graphql-error-code`](testing-use-cases/graphql-error-code) | Compare GraphQL error prose with a stable code |
| 13 | [`workflow-narrative`](testing-use-cases/workflow-narrative) | Check narrative continuity across services |
| 14 | [`audit-note-state`](testing-use-cases/audit-note-state) | Compare an audit note with committed state |
| 15 | [`webhook-route-review`](testing-use-cases/webhook-route-review) | Check webhook narrative routing in an end-to-end fixture |
| 16 | [`fuzz-seed-curation`](testing-use-cases/fuzz-seed-curation) | Curate reviewed fuzz seeds from bug reports |
| 17 | [`fuzz-failure-triage`](testing-use-cases/fuzz-failure-triage) | Classify a minimized fuzz failure |
| 18 | [`fuzz-symptom-clusters`](testing-use-cases/fuzz-symptom-clusters) | Suggest groups of related fuzz failures |
| 19 | [`test-log-triage`](testing-use-cases/test-log-triage) | Triage intermittent test logs |
| 20 | [`surviving-mutation-review`](testing-use-cases/surviving-mutation-review) | Review a surviving code mutation |

Conventions shared by every example:

- a header comment naming the pattern and its run command;
- readable stdout showing the answer and the Go policy applied to it;
- both plain-string and structured-map `State` values across the catalog;
- live SDK calls with no network mocking, including an error or uncertain path.

Thresholds in the examples illustrate control flow. Measure them on labeled
application data before using them for production decisions.

The use-case map: <https://docs.typesafe.ai/concepts/use-case-map>.
