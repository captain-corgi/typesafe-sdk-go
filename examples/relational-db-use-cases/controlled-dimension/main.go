// Command controlled-dimension maps an incoming description to an existing
// category code and shows the parameterized transactional insert. The IDs
// represent rows the application has already fetched from its categories table.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/controlled-dimension
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

	// In an application, SELECT id, code FROM categories WHERE tenant_id = $1
	// supplies this tenant-authorized code-to-ID mapping.
	categories := map[string]int64{"billing": 11, "access": 12, "reliability": 13}
	description := "The checkout page charged my card twice for one order."
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: description,
		Questions: typesafe.Questions{"category": typesafe.Choice{
			Instructions: "Which existing category best describes this record?",
			Criteria: typesafe.ChoiceCriteria{
				"billing":     "Charges, invoices, and payment errors",
				"access":      "Login, authorization, and account access",
				"reliability": "Outages, latency, and service failures",
				"other":       "None of the listed categories",
			},
		}},
	})
	if err != nil {
		log.Fatalf("save unclassified record for review: %v", err)
	}
	answer := resp.Choices()["category"]
	categoryID, known := categories[answer.Choice]
	if !known || answer.Confidence < 0.85 { // Calibrate before production use.
		fmt.Printf("pending classification: %q (choice %q, confidence %.2f)\n", description, answer.Choice, answer.Confidence)
		fmt.Printf("SQL: INSERT INTO records (tenant_id, description, category_id, classification_state) VALUES ($1, $2, NULL, 'pending')\nargs: %#v\n", []any{int64(42), description})
		return
	}

	// Execute this insert inside the application's transaction. The FK still
	// verifies that categoryID names a real category at commit time.
	fmt.Printf("BEGIN;\nSQL: INSERT INTO records (tenant_id, description, category_id, classification_state) VALUES ($1, $2, $3, 'classified')\nargs: %#v\nCOMMIT;\n", []any{int64(42), description, categoryID})
}
