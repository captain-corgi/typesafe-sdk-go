// Command detection demonstrates the Detection task category: the
// probability that one property is present. Each inbox message runs
// through a Noul battery (spam, phishing, promotional) and per-signal
// thresholds pick the folder in plain Go.
//
// It mirrors the "Detection" row of the docs use-case map's example task
// categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/detection
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// thresholds per signal; phishing acts at a lower bar than the others.
const (
	spamThreshold        = 0.50
	phishingThreshold    = 0.30
	promotionalThreshold = 0.50
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

	messages := []string{
		"Hi team, attached is the Q3 board deck for tomorrow's review — same agenda as " +
			"last quarter. Cheers, Dana",
		"URGENT: your account will be suspended in 24h. Verify your password and card " +
			"details at secure-login.example-net.ru immediately.",
		"Flash sale! 40% off everything this weekend only. Unsubscribe anytime.",
	}

	for _, message := range messages {
		fmt.Println("message:", message)
		detect(client, message)
		fmt.Println()
	}
}

func detect(client *typesafe.Client, message string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: message,
		Questions: typesafe.Questions{
			"spam": typesafe.Noul{
				Instructions: "Is this message unsolicited bulk mail?",
			},
			"phishing": typesafe.Noul{
				Instructions: "Is this message a phishing attempt?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Urgency plus a credential or payment request impersonating a trusted sender",
					False: "Legitimate mail, or bulk mail without credential theft",
				},
			},
			"promotional": typesafe.Noul{
				Instructions: "Is this a marketing or promotional message?",
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	signals := map[string]float64{
		"spam":        resp.Nouls()["spam"].Noul,
		"phishing":    resp.Nouls()["phishing"].Noul,
		"promotional": resp.Nouls()["promotional"].Noul,
	}
	for _, name := range sortedNames(signals) {
		fmt.Printf("  p(%s): %.2f\n", name, signals[name])
	}

	switch {
	case signals["phishing"] >= phishingThreshold:
		fmt.Println("  action: quarantine and warn the recipient")
	case signals["spam"] >= spamThreshold:
		fmt.Println("  action: move to junk")
	case signals["promotional"] >= promotionalThreshold:
		fmt.Println("  action: move to promotions")
	default:
		fmt.Println("  action: deliver to inbox")
	}
}

// sortedNames keeps the printed signals deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
