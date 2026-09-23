// Command entity-alignment scores SQL-shortlisted import candidates and writes
// match proposals for curator review. It never merges entities from a model
// answer alone.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/entity-alignment
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type candidate struct {
	id           int64
	name         string
	manufacturer string
	productType  string
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

	// The application runs this indexed, tenant-scoped query first. Candidate
	// IDs below are example rows it returned, not IDs produced by Jev.
	fmt.Println("shortlist SQL: SELECT id, name, manufacturer, product_type FROM catalog_items WHERE tenant_id = $1 AND search_name % $2 ORDER BY similarity(search_name, $2) DESC LIMIT 5")
	fmt.Printf("shortlist args: %#v\n", []any{int64(42), "Acme Router AX-6"})
	candidates := []candidate{
		{id: 901, name: "Acme AX6 Wi-Fi Router", manufacturer: "Acme", productType: "router"},
		{id: 902, name: "Acme AX6 Range Extender", manufacturer: "Acme", productType: "range extender"},
	}
	const sourceSystem, sourceID = "partner-feed", "SKU-AX6-RTR"
	imported := map[string]any{"name": "Acme Router AX-6", "manufacturer": "Acme", "product_type": "router"}

	for _, c := range candidates {
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State: map[string]any{"imported": imported, "candidate": map[string]any{
				"name": c.name, "manufacturer": c.manufacturer, "product_type": c.productType,
			}},
			Questions: typesafe.Questions{
				"identity": typesafe.Score{
					Instructions: "Are these descriptions of the same catalog product, including product type?",
					Criteria:     typesafe.ScoreCriteria{"Clearly different products", "Uncertain identity", "Same product"},
				},
				"same_type": typesafe.Noul{Instructions: "Do both descriptions refer to the same product type?", Criteria: &typesafe.NoulCriteria{
					True: "Both are routers or both are extenders", False: "Their product types differ or are unclear",
				}},
				"same_brand": typesafe.Noul{Instructions: "Do both descriptions identify the same manufacturer?", Criteria: &typesafe.NoulCriteria{
					True: "Manufacturer matches", False: "Manufacturer differs or is unclear",
				}},
			},
		})
		if err != nil {
			fmt.Printf("candidate %d: keep unmerged; assessment failed: %v\n", c.id, err)
			continue
		}
		identity := resp.Scores()["identity"]
		state := "needs_review"
		if identity.Score >= 1.8 && identity.Confidence >= 0.90 && resp.Nouls()["same_type"].Noul >= 0.95 && resp.Nouls()["same_brand"].Noul >= 0.95 {
			state = "plausible_match" // A curator still checks identity and FK effects.
		}
		fmt.Printf("candidate %d: identity %.2f (confidence %.2f), proposal %s\n", c.id, identity.Score, identity.Confidence, state)
		fmt.Printf("SQL: INSERT INTO import_match_proposals (tenant_id, source_system, source_id, candidate_id, score, model_version, review_state) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (tenant_id, source_system, source_id, candidate_id) DO NOTHING\nargs: %#v\n",
			[]any{int64(42), sourceSystem, sourceID, c.id, identity.Score, resp.Model, state})
	}
}
