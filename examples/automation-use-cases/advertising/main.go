// Command advertising demonstrates creative review: an ad evaluated for
// brand safety, claim substantiation, and ad-to-landing-page alignment,
// plus a creative-quality Score and audience fit, deciding between
// approve, revise, and reject in plain Go.
//
// It mirrors the "Advertising" entry of the docs use-case map's example
// automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/advertising
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

	creatives := []map[string]any{
		{
			"headline":     "Sleep deeper in 7 nights — or your money back",
			"body":         "Clinically tested frequency shifts. 30-day guarantee.",
			"landing_page": "Soundscape app subscription page; guarantee terms in the footer.",
			"placement":    "podcast ads, wellness channels",
		},
		{
			"headline":     "Beat insomnia forever with sound therapy doctors trust",
			"body":         "Miracle results in 3 days. No diet or lifestyle change needed.",
			"landing_page": "One-page checkout with no clinical references or refund policy.",
			"placement":    "auto-placed across open exchange inventory",
		},
	}

	for _, creative := range creatives {
		fmt.Println("creative:", creative["headline"])
		review(client, creative)
		fmt.Println()
	}
}

func review(client *typesafe.Client, creative map[string]any) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: creative,
		Questions: typesafe.Questions{
			"brand_safety": typesafe.Noul{
				Instructions: "Is the creative or its placement context unsafe for the brand?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Adjacency risk (hate, tragedy, misinformation) or out-of-brand tone",
					False: "Context and tone are brand-appropriate",
				},
			},
			"unsubstantiated_claim": typesafe.Noul{
				Instructions: "Does the creative make a claim the landing page does not substantiate?",
			},
			"ad_page_alignment": typesafe.Noul{
				Instructions: "Does the landing page deliver exactly what the ad promises?",
				Criteria: &typesafe.NoulCriteria{
					True:  "The page matches the ad's offer, price, and expectations",
					False: "Bait-and-switch, missing offer, or mismatched expectations",
				},
			},
			"creative_quality": typesafe.Score{
				Instructions: "How strong is the creative (hook, clarity, call to action)?",
				Criteria:     typesafe.ScoreCriteria{"weak", "solid", "exceptional"},
			},
			"audience_fit": typesafe.Choice{
				Instructions: "How does the creative fit the placement audience?",
				Criteria: typesafe.ChoiceCriteria{
					"broad":      "Relevant to most of the audience",
					"niche":      "Relevant to a segment of the audience",
					"mismatched": "Wrong audience for this message",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	brandSafety := resp.Nouls()["brand_safety"].Noul
	unsubstantiated := resp.Nouls()["unsubstantiated_claim"].Noul
	alignment := resp.Nouls()["ad_page_alignment"].Noul
	quality := resp.Scores()["creative_quality"]
	fit := resp.Choices()["audience_fit"]

	fmt.Printf("  brand safety risk: %.2f\n", brandSafety)
	fmt.Printf("  unsubstantiated claim: %.2f\n", unsubstantiated)
	fmt.Printf("  ad-page alignment: %.2f\n", alignment)
	fmt.Printf("  creative quality: %.2f/2\n", quality.Score)
	fmt.Printf("  audience fit: %s (confidence %.2f)\n", fit.Choice, fit.Confidence)

	switch {
	case brandSafety >= 0.5:
		fmt.Println("  decision: reject (brand safety)")
	case unsubstantiated >= 0.5 || alignment < 0.5 || fit.Choice == "mismatched":
		fmt.Println("  decision: revise (claims, alignment, or audience fit)")
	case quality.Score >= 1.5:
		fmt.Println("  decision: approve and scale spend")
	default:
		fmt.Println("  decision: approve at baseline spend")
	}
}
