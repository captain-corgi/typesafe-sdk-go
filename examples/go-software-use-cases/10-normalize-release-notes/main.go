// Command normalize-release-notes turns human prose into a bounded changelog record.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/10-normalize-release-notes
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type changeRecord struct {
	Text              string  `json:"text"`
	Kind              string  `json:"kind"`
	MigrationRequired bool    `json:"migration_required"`
	Impact            float64 `json:"impact"`
	NeedsReview       bool    `json:"needs_review"`
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	note := "Renamed WithAttempts to WithRetry; callers must update their constructor options."
	if len(os.Args) > 1 {
		note = os.Args[1]
	}
	record := changeRecord{Text: note, Kind: "other", NeedsReview: true}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: note,
		Questions: typesafe.Questions{
			"kind": typesafe.Choice{Instructions: "Which release-note category best fits?", Criteria: typesafe.ChoiceCriteria{
				"feature": "New capability", "fix": "Bug fix", "breaking_change": "Existing callers must change", "other": "Unclear or different change",
			}},
			"migration": typesafe.Noul{Instructions: "Does the note indicate that users need a migration step?"},
			"impact": typesafe.Score{Instructions: "How visible is this change to users?", Criteria: typesafe.ScoreCriteria{
				"Internal only", "Some users affected", "Many users affected",
			}},
		},
	})
	if err == nil {
		kind, impact, migration := resp.Choices()["kind"], resp.Scores()["impact"], resp.Nouls()["migration"].Noul
		if kind.Confidence >= 0.75 && impact.Confidence >= 0.65 && kind.Choice != "other" && (migration <= 0.25 || migration >= 0.80) {
			record.Kind = kind.Choice
			record.Impact = impact.Score
			record.MigrationRequired = migration >= 0.80
			record.NeedsReview = false
		}
	} else {
		fmt.Fprintln(os.Stderr, "classification unavailable; record requires review:", err)
	}
	// The record carries the original prose. Release tooling or a person writes any migration instructions.
	if err := json.NewEncoder(os.Stdout).Encode(record); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
