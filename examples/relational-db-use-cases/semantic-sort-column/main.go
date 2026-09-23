// Command semantic-sort-column derives a rubric score for a work item, to be
// stored as nullable enrichment and used by a later PostgreSQL ORDER BY.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/semantic-sort-column
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

const rubricVersion = "reproducibility-v1"

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	itemID := int64(604)
	tenantID := int64(42) // Authorized tenant from the application session.
	summary := "On version 2.8, save twice within 1 second; the second request returns 500 and discards changes."
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: summary,
		Questions: typesafe.Questions{"reproducibility": typesafe.Score{
			Instructions: "How clearly does this summary describe a reproducible failure? Rate only the description, not customer impact or severity.",
			Criteria: typesafe.ScoreCriteria{
				"No failure described",
				"Failure alleged without an observable effect",
				"Observable failure and affected behavior described",
				"Reproduction steps and observable effect clearly described",
			},
		}},
	})
	if err != nil {
		log.Fatalf("leave review_priority_score NULL and use deterministic ordering: %v", err)
	}
	answer := resp.Scores()["reproducibility"]
	if answer.Confidence < 0.80 { // Illustrative; calibrate with labeled summaries.
		fmt.Printf("confidence %.2f: keep score NULL for item %d\n", answer.Confidence, itemID)
		return
	}

	// The summary predicate avoids writing a score derived from stale text.
	// Recompute whenever summary, rubricVersion, or model version changes.
	fmt.Printf("score %.2f, confidence %.2f\n", answer.Score, answer.Confidence)
	fmt.Printf("SQL: UPDATE work_items SET review_priority_score = $1, rubric_version = $2, model_version = $3 WHERE tenant_id = $4 AND id = $5 AND summary = $6\nargs: %#v\n",
		[]any{answer.Score, rubricVersion, resp.Model, tenantID, itemID, summary})
	fmt.Println("read SQL: SELECT id, summary FROM work_items WHERE tenant_id = $1 ORDER BY review_priority_score DESC NULLS LAST, created_at ASC, id ASC")
}
