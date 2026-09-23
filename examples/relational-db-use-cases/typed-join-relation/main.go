// Command typed-join-relation classifies the relationship between two
// SQL-selected rows, then applies fixed endpoint, type, direction, and cycle
// checks before proposing a parameterized join-table insert.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/typed-join-relation
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	tenantID := int64(42)
	sourceID, targetID := int64(301), int64(408)
	// These are endpoints the application selected with tenant-scoped SQL.
	authorizedIDs := map[int64]bool{301: true, 408: true, 510: true}
	if sourceID == targetID || !authorizedIDs[sourceID] || !authorizedIDs[targetID] {
		log.Fatal("endpoints are missing, outside scope, or identical")
	}
	// Current directed blocks edges came from an ordinary scoped SQL query.
	blocks := map[int64][]int64{408: {510}}
	statement := "Issue 301 blocks issue 408; issue 408 cannot be closed until 301 is fixed."
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"source_id": sourceID, "target_id": targetID, "statement": statement},
		Questions: typesafe.Questions{"relation": typesafe.Choice{
			Instructions: "What relationship does the statement assert from source to target?",
			Criteria: typesafe.ChoiceCriteria{
				"duplicates": "Both issues describe the same underlying issue",
				"blocks":     "Source prevents progress on target",
				"caused_by":  "Source was caused by target",
				"related":    "Related without a stronger typed relationship",
				"none":       "No supported relationship, or ambiguous direction",
			},
		}},
	})
	if err != nil {
		log.Fatalf("leave endpoints unlinked for review: %v", err)
	}
	answer := resp.Choices()["relation"]
	if answer.Confidence < 0.85 { // Calibrate for the effect of each edge type.
		fmt.Printf("proposal needs review: confidence %.2f\n", answer.Confidence)
		return
	}
	switch answer.Choice {
	case "duplicates", "related":
		// Canonical order prevents both (A,B) and (B,A) for symmetric edges.
		if sourceID > targetID {
			sourceID, targetID = targetID, sourceID
		}
	case "blocks":
		if reaches(blocks, targetID, sourceID, map[int64]bool{}) {
			fmt.Println("block edge would create a cycle; queue for review")
			return
		}
	case "caused_by":
		// Preserve source -> target direction for the causal edge.
	case "none":
		fmt.Println("no supported edge; leave rows unlinked")
		return
	default:
		log.Fatal("unexpected relation label")
	}
	// Repeat scope checks in SQL; constraints/FKs remain the final gate for
	// endpoint integrity. Recheck the blocks graph inside the application's
	// serializable transaction before inserting, to prevent a concurrent cycle.
	fmt.Printf("SQL: INSERT INTO item_relations (tenant_id, source_id, target_id, relation_type) SELECT $1, s.id, t.id, $4 FROM items s JOIN items t ON t.tenant_id = s.tenant_id WHERE s.tenant_id = $1 AND s.id = $2 AND t.id = $3 ON CONFLICT DO NOTHING\nargs: %#v\n",
		[]any{tenantID, sourceID, targetID, answer.Choice})
}

func reaches(edges map[int64][]int64, from, goal int64, seen map[int64]bool) bool {
	if from == goal {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, next := range edges[from] {
		if reaches(edges, next, goal, seen) {
			return true
		}
	}
	return false
}
