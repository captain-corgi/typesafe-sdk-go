// Command migration-worklist classifies a keyset page of legacy rows into
// bounded migration buckets. It prints idempotent staging writes and a fixed
// query that applies only separately reviewed mappings.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/migration-worklist
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

const rubricVersion = "legacy-flags-v1"

type legacyRow struct {
	id            int64
	sourceVersion int64
	flag          string
}

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	tenantID, checkpoint := int64(42), int64(1000)
	fmt.Println("page SQL: SELECT id, source_version, legacy_flags FROM legacy_records WHERE tenant_id = $1 AND id > $2 ORDER BY id LIMIT 100")
	fmt.Printf("page args: %#v\n", []any{tenantID, checkpoint})
	// Example rows returned by that query. Persist the last processed ID as a
	// checkpoint only after staging writes have committed.
	rows := []legacyRow{{id: 1001, sourceVersion: 7, flag: "card charge disputed"}, {id: 1002, sourceVersion: 3, flag: "could not enter account"}}
	for _, row := range rows {
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State: row.flag,
			Questions: typesafe.Questions{"migration_class": typesafe.Choice{
				Instructions: "Which known migration bucket fits this legacy flag? Use needs_review for ambiguity.",
				Criteria: typesafe.ChoiceCriteria{
					"billing":      "Payments, charges, and invoices",
					"access":       "Login and account access",
					"needs_review": "Ambiguous or not enough information",
					"out_of_scope": "Clearly outside this migration",
				},
			}},
		})
		if err != nil {
			fmt.Printf("row %d: keep legacy form; API failed: %v\n", row.id, err)
			break // Do not checkpoint past an unstaged row.
		}
		answer := resp.Choices()["migration_class"]
		bucket := answer.Choice
		if answer.Confidence < 0.85 { // Illustrative; calibrate on labeled legacy rows.
			bucket = "needs_review"
		}
		switch bucket {
		case "billing", "access", "needs_review", "out_of_scope":
		default:
			bucket = "needs_review"
		}
		fmt.Printf("row %d: propose %s (confidence %.2f)\n", row.id, bucket, answer.Confidence)
		fmt.Printf("SQL: INSERT INTO migration_staging (tenant_id, record_id, source_version, proposed_class, rubric_version, model_version, review_state) VALUES ($1, $2, $3, $4, $5, $6, 'pending') ON CONFLICT (tenant_id, record_id, source_version, rubric_version) DO NOTHING\nargs: %#v\n",
			[]any{tenantID, row.id, row.sourceVersion, bucket, rubricVersion, resp.Model})
		checkpoint = row.id
	}
	fmt.Printf("commit staging transaction, then persist checkpoint %d\n", checkpoint)
	fmt.Println("reviewed apply SQL: UPDATE legacy_records AS r SET migration_class = s.reviewed_class FROM migration_staging AS s WHERE r.tenant_id = $1 AND r.id = s.record_id AND r.source_version = s.source_version AND s.tenant_id = r.tenant_id AND s.rubric_version = $2 AND s.review_state = 'approved' AND s.reviewed_class IN ('billing', 'access')")
	fmt.Printf("reviewed apply args: %#v\n", []any{tenantID, rubricVersion})
}
