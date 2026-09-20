// Command financial-crime demonstrates alert triage for anti-money-
// laundering workflows: a transaction narrative matched against typologies,
// entity-name variants resolved across inconsistent records, and the alert
// prioritized into a plain Go routing decision.
//
// It mirrors the "Financial crime" entry of the docs use-case map's example
// automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/financial-crime
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type alert struct {
	narrative string
	records   []string
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	alerts := []alert{
		{
			narrative: "Customer received 4 inbound wires of $9,400 each within 48 hours and " +
				"forwarded the combined amount to a personal account at an offshore bank.",
			records: []string{"ACME TRADING LTD", "Acme Trading Limited", "ACME Trading"},
		},
		{
			narrative: "Monthly payroll transfer to a single known supplier, amount consistent " +
				"with the last 12 months of invoices.",
			records: []string{"Northwind Bakery LLC", "Northwind Bakery, L.L.C."},
		},
	}

	for _, a := range alerts {
		fmt.Println("narrative:", a.narrative)
		triage(client, a)
		fmt.Println()
	}
}

func triage(client *typesafe.Client, a alert) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"narrative":            a.narrative,
			"entity_name_variants": a.records,
		},
		Questions: typesafe.Questions{
			"typology_match": typesafe.Noul{
				Instructions: "Does the transaction pattern match a known money-laundering typology?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Structuring, layering, or rapid movement of just-under-threshold funds",
					False: "Ordinary business payments consistent with history",
				},
			},
			"same_entity": typesafe.Noul{
				Instructions: "Do all the listed name variants refer to the same legal entity?",
			},
			"alert_class": typesafe.Choice{
				Instructions: "How should this alert be classified?",
				Criteria: typesafe.ChoiceCriteria{
					"false positive": "Explained by legitimate business context",
					"suspicious":     "Warrants investigation",
					"urgent":         "Suspicious with escalation-critical features",
				},
			},
			"risk": typesafe.Score{
				Instructions: "Rate the money-laundering risk of this activity.",
				Criteria:     typesafe.ScoreCriteria{"low", "medium", "high"},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	typology := resp.Nouls()["typology_match"].Noul
	sameEntity := resp.Nouls()["same_entity"].Noul
	class := resp.Choices()["alert_class"]
	risk := resp.Scores()["risk"].Score

	fmt.Printf("  typology match: %.2f\n", typology)
	fmt.Printf("  same entity across records: %.2f\n", sameEntity)
	fmt.Printf("  alert class: %s (confidence %.2f)\n", class.Choice, class.Confidence)
	fmt.Printf("  risk: %.2f/2\n", risk)

	switch {
	case class.Choice == "urgent" || risk >= 1.5:
		fmt.Println("  route: senior investigator, same day")
	case class.Choice == "suspicious" || typology >= 0.5:
		fmt.Println("  route: investigation queue")
	case sameEntity < 0.5:
		fmt.Println("  route: entity-resolution desk (records may not match)")
	default:
		fmt.Println("  route: auto-close (documented false positive)")
	}
}
