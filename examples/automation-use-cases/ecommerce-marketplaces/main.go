// Command ecommerce-marketplaces demonstrates listing moderation and
// normalization: a raw listing classified into category and condition,
// checked for prohibited items and counterfeit signals, and routed to
// publish, hold, or remove in plain Go.
//
// It mirrors the "E-commerce marketplaces" entry of the docs use-case
// map's example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/ecommerce-marketplaces
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

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

	listings := []map[string]any{
		{
			"title": "Sonicare Diamnd Clean electric toothbrush — brand new in box",
			"description": "Authentic Philips Sonicare, sealed. Retails $180, selling for $39 " +
				"because of an overflowing warehouse. Ships worldwide.",
			"price_usd": 39,
		},
		{
			"title": "Vintage mechanical kitchen scale, steel, works",
			"description": "1960s steel kitchen scale in working condition, some patina on the " +
				"base, original weighing pan included. 30 cm tall.",
			"price_usd": 45,
		},
	}

	for _, listing := range listings {
		fmt.Println("listing:", listing["title"])
		moderate(client, listing)
		fmt.Println()
	}
}

func moderate(client *typesafe.Client, listing map[string]any) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: listing,
		Questions: typesafe.Questions{
			"category": typesafe.Choice{
				Instructions: "Which marketplace category does this listing belong in?",
				Criteria: typesafe.ChoiceCriteria{
					"electronics":   "Consumer electronics and accessories",
					"home":          "Household goods, furniture, and appliances",
					"personal_care": "Health and personal care devices",
					"other":         "Anything else",
				},
			},
			"condition": typesafe.Choice{
				Instructions: "What condition does the listing describe?",
				Criteria: typesafe.ChoiceCriteria{
					"new":         "Unused, sealed, or brand new",
					"refurbished": "Restored to working order by a seller or manufacturer",
					"used":        "Previously owned, with or without wear",
				},
			},
			"prohibited_item": typesafe.Noul{
				Instructions: "Is this item banned or restricted on the marketplace?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Recalled, restricted, or policy-prohibited goods",
					False: "Ordinary permitted goods",
				},
			},
			"counterfeit_signal": typesafe.Noul{
				Instructions: "Does the listing show counterfeit signals?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Brand-name misspelling, implausible price, or stock-photo reuse",
					False: "Price, branding, and photos look genuine",
				},
			},
			"listing_quality": typesafe.Score{
				Instructions: "How complete and trustworthy is the listing?",
				Criteria:     typesafe.ScoreCriteria{"sparse", "adequate", "detailed"},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	category := resp.Choices()["category"]
	condition := resp.Choices()["condition"]
	prohibited := resp.Nouls()["prohibited_item"].Noul
	counterfeit := resp.Nouls()["counterfeit_signal"].Noul
	quality := resp.Scores()["listing_quality"]

	fmt.Printf("  normalized category: %s (confidence %.2f)\n", category.Choice, category.Confidence)
	fmt.Printf("  normalized condition: %s (confidence %.2f)\n", condition.Choice, condition.Confidence)
	fmt.Printf("  prohibited item: %.2f\n", prohibited)
	fmt.Printf("  counterfeit signal: %.2f\n", counterfeit)
	fmt.Printf("  listing quality: %.2f/2\n", quality.Score)

	switch {
	case prohibited >= 0.5:
		fmt.Println("  action: remove listing (prohibited item)")
	case counterfeit >= 0.5:
		fmt.Println("  action: hold for brand verification")
	case category.Confidence < 0.6:
		fmt.Println("  action: hold — request category clarification from seller")
	default:
		fmt.Println("  action: publish listing")
	}
}
