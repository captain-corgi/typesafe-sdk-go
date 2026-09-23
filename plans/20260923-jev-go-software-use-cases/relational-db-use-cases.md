# Jev with relational databases: 10 PostgreSQL use cases

Research date: 2026-09-23. These are **proposed PostgreSQL adaptations** of documented TypeSafe semantic decision patterns, not claims that TypeSafe ships a PostgreSQL integration or has measured these exact workflows. The companion [Go software use cases](go-software-use-cases.md) cover general routing, search, and code tooling. This file focuses on table writes, joins, imports, history, and migrations. Examples assume an application written in Go with its own PostgreSQL driver; the TypeSafe Go SDK itself needs no database dependency.

Jev evaluates a text or JSON-text `state` with bounded `Choice`, `Noul`, and `Score` questions. [TypeSafe defines their answer shapes](https://docs.typesafe.ai/primitives): `Choice` selects from supplied labels, `Noul` estimates the probability of a yes/no statement, and `Score` positions an item on an ordered rubric. Go maps those values to reviewed SQL operations. A model answer never becomes SQL text, a table/column name, or an unverified primary key. Use [query parameters](https://go.dev/doc/database/sql-injection) for values. PostgreSQL still owns [constraints](https://www.postgresql.org/docs/current/ddl-constraints.html), [row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html), and [transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html). Set decision thresholds from labeled examples and route uncertain answers to a review path, as [TypeSafe's confidence guidance](https://docs.typesafe.ai/confidence) recommends.

## 1. Compile natural-language filters to an allowlisted `SELECT`

- **Placement:** Before a read query against a table such as `issues(tenant_id, status, kind, opened_at)`.
- **Jev decision:** Separate `Choice` questions map “show unresolved regressions from last week” to bounded values such as `status=open|closed|any`, `kind=regression|feature|other`, and `time_window=last_week|last_month|unspecified`. Include `unspecified` rather than inventing a filter.
- **Go/SQL action:** Go maps labels to fixed predicates and calculates date boundaries from a real clock. It executes a parameterized query scoped by `tenant_id`, for example `WHERE tenant_id = $1 AND status = $2 AND opened_at >= $3`. SQL identifiers and operators come from code, never from Jev.
- **Fallback:** Ask for clarification when the requested filter is outside the allowed vocabulary or confidence is low; run the ordinary unfiltered, authorized query only if that is an explicit product behavior.
- **Evidence:** **Adaptation** of TypeSafe's [bounded Choice primitive](https://docs.typesafe.ai/primitives) and [pre-parsed extraction pattern](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook); Go's [SQL parameter guidance](https://go.dev/doc/database/sql-injection).

## 2. Normalize incoming prose to a controlled dimension row

- **Placement:** During ingestion of `records(description, category_id)`, where `category_id` references a reviewed `categories` table.
- **Jev decision:** `Choice` among the category codes supplied by Go, including `other`. The description may use synonyms that a deterministic lookup does not cover.
- **Go/SQL action:** Go resolves the selected code to an existing category ID, then inserts the record and FK in one transaction. The category list can be hierarchical: load the permitted children of one category and make a follow-up Choice only when the parent choice determines the next options.
- **Fallback:** Insert the raw description with `category_id = NULL` or a pending classification state; do not create a category or manufacture an ID from model output.
- **Evidence:** **Adaptation** of TypeSafe's [Choice guidance](https://docs.typesafe.ai/primitives) and [hierarchical classification cookbook](https://docs.typesafe.ai/cookbooks/hierarchical_classification); PostgreSQL [foreign-key constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).

## 3. Populate a many-to-many semantic tag table

- **Placement:** After inserting a text row, before adding associations to `record_tags(record_id, tag_id)`.
- **Jev decision:** Ask one `Noul` per independently applicable tag, such as “does this note report a retry failure?” and “does it describe a compatibility break?” This permits multiple tags, unlike a single-category Choice.
- **Go/SQL action:** Go maps positive, calibrated signals to existing tag IDs and inserts only those pairs, with a unique constraint on `(record_id, tag_id)`. SQL can then filter and aggregate tags through ordinary joins and indexes.
- **Fallback:** Leave uncertain tags absent and queue the row for review or later reclassification; preserve the source text so labels can be revised.
- **Evidence:** **Adaptation** of TypeSafe's [independent multi-question design](https://docs.typesafe.ai/primitives) and [feature extraction use case](https://docs.typesafe.ai/concepts/use-case-map); PostgreSQL [unique and foreign-key constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).

## 4. Materialize an ordered semantic column for SQL ordering

- **Placement:** In a write-side worker that enriches `work_items(summary, review_priority_score, rubric_version)`.
- **Jev decision:** A `Score` rates one narrow property of the summary on named levels, for example whether it expresses a clear, reproducible failure, from “no failure described” to “reproduction and effect clearly described.” This is a rubric value, not a measured service-level metric.
- **Go/SQL action:** Store the score and rubric/model version as derived data. A later `SELECT ... ORDER BY review_priority_score DESC NULLS LAST` can combine it with deterministic fields such as creation time and explicit severity. Recompute when the source text, rubric, or model version changes.
- **Fallback:** Store `NULL` on API failure or uncertainty and use the existing deterministic order; avoid treating an absent semantic value as zero.
- **Evidence:** **Adaptation** of TypeSafe's [Score primitive](https://docs.typesafe.ai/primitives), [composite scoring pattern](https://docs.typesafe.ai/patterns/composite-scoring), and PostgreSQL [`ORDER BY`](https://www.postgresql.org/docs/current/queries-order.html).

## 5. Reconcile imported entities before an upsert or merge

- **Placement:** Import pipeline for a catalogue or account-like table where source systems describe the same entity differently.
- **Jev decision:** First use exact keys or an indexed [PostgreSQL trigram](https://www.postgresql.org/docs/current/pgtrgm.html) search to retrieve a small candidate set. For each pair, a three-level `Score` distinguishes different, uncertain, and same; companion `Noul` questions compare semantic fields.
- **Go/SQL action:** Keep imported source IDs and candidate database IDs separate. Go records a proposed match and permits a reviewed `INSERT ... ON CONFLICT` or merge procedure only after identity rules and FK effects have been checked.
- **Fallback:** Preserve a new unmerged row or put the pair in a curator queue. Never rewrite existing foreign keys because the model alone said “same.”
- **Evidence:** TypeSafe **directly documents the pair-scoring and curator path** in its [entity alignment cookbook](https://docs.typesafe.ai/cookbooks/entity_alignment); the import, candidate SQL, and upsert are **PostgreSQL adaptations**. PostgreSQL documents [`ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html).

## 6. Resolve a prose reference to an existing foreign-key target

- **Placement:** Attaching a note such as “caused by the gateway rollout” to `deployments(id, service, started_at)` through `incident_deployments`.
- **Jev decision:** Go first uses service, time range, tenant, and indexed text search to shortlist *existing* deployments. A `Choice` selects one candidate ID label or `none`; the model does not generate IDs.
- **Go/SQL action:** Go maps that label back to the ID fetched from PostgreSQL, rechecks scope and existence, and inserts the association under an FK constraint. An audit row can retain the chosen candidate set and question version.
- **Fallback:** If there are no candidates, competing candidates, or low confidence, save the note unattached and request human linking.
- **Evidence:** **Adaptation** of TypeSafe's [pre-parsed candidate selection](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook), which chooses only supplied values; PostgreSQL [full-text indexes](https://www.postgresql.org/docs/current/textsearch-indexes.html) and [FK constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).

## 7. Label the relationship represented by a join row

- **Placement:** Creating a typed edge in `item_relations(source_id, target_id, relation_type)` after both endpoint rows have been selected by SQL.
- **Jev decision:** A `Choice` classifies the text describing the pair as `duplicates`, `blocks`, `caused_by`, `related`, or `none`. The question evaluates the *relationship*, not identity or a general category of either row.
- **Go/SQL action:** Go checks that the endpoint IDs exist, applies any direction and cycle rules, and inserts an allowed relation type in a transaction. Database checks/FKs still reject invalid types and missing endpoints.
- **Fallback:** Store a proposed edge for review or leave rows unlinked when the relation is ambiguous. Do not infer graph edges from unrelated table-wide pairs.
- **Evidence:** TypeSafe lists [knowledge-graph relationship classification](https://docs.typesafe.ai/concepts/use-case-map) as a use case; representing that result in a PostgreSQL join table is an **adaptation**. PostgreSQL documents [checks and FKs](https://www.postgresql.org/docs/current/ddl-constraints.html) and [transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html).

## 8. Check a prose update against structured columns

- **Placement:** Before committing a mutation to a row such as `incidents(status, resolution_note, resolved_at)`.
- **Jev decision:** A `Noul` asks whether a proposed note contradicts the proposed structured state, for example a `status=resolved` update whose note says the failure still reproduces. Separate questions can check separate semantic tensions; exact timestamp and enum validation remain in code.
- **Go/SQL action:** Go holds the mutation for clarification or writes a pending review record. Once resolved, it updates structured columns and note atomically. The database still enforces `NOT NULL`, `CHECK`, FK, and transactional requirements.
- **Fallback:** If Jev fails or reports uncertainty, keep the old authoritative row and the attempted update in a review queue; do not silently change status.
- **Evidence:** TypeSafe discusses [checking contradictions between records or claims](https://docs.typesafe.ai/concepts/use-case-map) and [one narrow Noul judgment](https://docs.typesafe.ai/primitives); this mutation guard is a **PostgreSQL adaptation**. See [constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).

## 9. Flag semantic contradictions across versioned rows

- **Placement:** A history/audit worker comparing a new `record_versions` row with its immediately preceding version, after SQL has selected the same stable record ID.
- **Jev decision:** A `Noul` asks whether the new narrative makes an incompatible claim about the same fact, rather than merely adding detail. A second `Noul` may ask whether the newer text explicitly retracts or corrects the older claim.
- **Go/SQL action:** Go inserts a `version_conflicts(old_version_id, new_version_id, ...)` review item with model/rubric version and both immutable row IDs. The actual version history remains intact; reviewers or deterministic business rules decide which value is current.
- **Fallback:** If the pair is unclear, leave it as an unresolved comparison. Do not overwrite or delete historical rows.
- **Evidence:** TypeSafe's [knowledge-graph use cases include contradiction detection](https://docs.typesafe.ai/concepts/use-case-map); comparing adjacent PostgreSQL versions and storing a review edge is an **adaptation**. PostgreSQL [transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html) preserve related writes as a unit.

## 10. Build a review worklist for a data migration

- **Placement:** Before converting a legacy free-text column into a new structured schema, for example moving `legacy_flags` into `migration_class` and related tables.
- **Jev decision:** A narrow `Choice` or `Noul` identifies which *known* migration bucket a legacy row appears to belong to, including `needs_review` and `out_of_scope`. The model never writes a migration or interprets SQL syntax.
- **Go/SQL action:** A Go batch job pages through rows by primary key, stores proposed decisions with source-row version and rubric version in a staging table, then applies only reviewed mappings with parameterized SQL. The migration can be rerun idempotently from its checkpoint.
- **Fallback:** Leave uncertain rows in the legacy form and surface them in a manual queue. Keep rollback and validation queries deterministic.
- **Evidence:** **Adaptation** of TypeSafe's [structured extraction/classification use cases](https://docs.typesafe.ai/concepts/use-case-map) and [bounded Choice](https://docs.typesafe.ai/primitives) to a database migration. PostgreSQL [transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html) and [constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) protect the actual data changes.

## Shared implementation rules

1. **Shortlist with SQL first.** For pairwise comparison, FK resolution, or matching, apply tenant predicates and indexed exact/full-text/trigram search before a Jev call. Jev sees only the small authorized candidate set, and it cannot find a row SQL omitted. [PostgreSQL full-text index guidance](https://www.postgresql.org/docs/current/textsearch-indexes.html); [TypeSafe's shortlist pattern](https://docs.typesafe.ai/cookbooks/rerank_typesafe).
2. **Separate inference from integrity.** Store the raw source, Jev result, rubric/model version, and reviewed outcome when a result will affect durable data. Map outputs to approved codes or fetched IDs; do not execute generated SQL. Keep database constraints, RLS, transactions, and authorization as the final gates. [TypeSafe primitives](https://docs.typesafe.ai/primitives); [PostgreSQL constraints](https://www.postgresql.org/docs/current/ddl-constraints.html).
3. **Calibrate by action.** Auto-populating a reversible tag needs a different threshold from merging entities or changing status. Measure on labeled rows from the actual application and route uncertain or failed calls to explicit pending states. [TypeSafe confidence](https://docs.typesafe.ai/confidence).
