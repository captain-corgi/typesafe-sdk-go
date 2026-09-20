// Command demand-forecasting demonstrates enriching a forecast with
// semantic signals: each sales note or review is mapped to demand signals
// (intent, urgency, supply worries, competitive pressure) in its own call,
// then reduced into an aggregate signal line a forecasting model can join
// on.
//
// It mirrors the "Demand forecasting" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/demand-forecasting
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type noteSignals struct {
	intent     float64
	urgency    float64
	supply     float64
	competitor float64
	interest   float64
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

	notes := []string{
		"Enterprise call: they want 300 seats for the analytics tier before their fiscal " +
			"year closes in 6 weeks.",
		"Channel partner says the latest shipment of the sensor kit is delayed again; " +
			"their backlog is growing.",
		"Renewal check-in: customer is piloting a rival's cheaper plan for one team.",
	}

	var total noteSignals
	for i, note := range notes {
		fmt.Printf("note %d: %s\n", i+1, note)
		s, err := signals(client, note)
		if err != nil {
			fmt.Println("  error:", err)
			fmt.Println()
			continue
		}
		fmt.Printf("  intent %.2f | urgency %.2f | supply concern %.2f | competitor %.2f | interest %.2f\n",
			s.intent, s.urgency, s.supply, s.competitor, s.interest)
		total.intent += s.intent
		total.urgency += s.urgency
		total.supply += s.supply
		total.competitor += s.competitor
		total.interest += s.interest
	}
	n := float64(len(notes))

	fmt.Println()
	fmt.Println("aggregate demand signal:")
	fmt.Printf("  mean intent: %.2f\n", total.intent/n)
	fmt.Printf("  mean interest: %.2f (0=cool, 1=interested, 2=must-have)\n", total.interest/n)
	fmt.Printf("  notes flagging supply: %.0f%%\n", 100*total.supply/n)
	fmt.Printf("  notes flagging competitors: %.0f%%\n", 100*total.competitor/n)

	meanInterest := total.interest / n
	meanIntent := total.intent / n
	switch {
	case meanInterest >= 1.0 && meanIntent >= 0.4:
		fmt.Println("  forecast adjustment: revise demand UP for next period")
	case meanInterest < 0.5:
		fmt.Println("  forecast adjustment: revise demand DOWN for next period")
	default:
		fmt.Println("  forecast adjustment: hold forecast, keep watching")
	}
	if total.supply/n >= 0.5 {
		fmt.Println("  risk flag: supply constraints may cap achievable revenue")
	}
	if total.competitor/n >= 0.5 {
		fmt.Println("  risk flag: competitive pressure in the pipeline")
	}
}

func signals(client *typesafe.Client, note string) (noteSignals, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: note,
		Questions: typesafe.Questions{
			"purchase_intent": typesafe.Noul{
				Instructions: "Does this note signal concrete intent to buy or expand?",
			},
			"urgency": typesafe.Noul{
				Instructions: "Does this note attach a deadline or urgency to the purchase?",
			},
			"supply_concern": typesafe.Noul{
				Instructions: "Does this note describe supply, inventory, or delivery problems?",
			},
			"competitive_mention": typesafe.Noul{
				Instructions: "Does this note mention a competitor being evaluated or adopted?",
			},
			"interest": typesafe.Score{
				Instructions: "How strong is the demand signal in this note?",
				Criteria:     typesafe.ScoreCriteria{"cool", "interested", "must have"},
			},
		},
	})
	if err != nil {
		return noteSignals{}, err
	}
	return noteSignals{
		intent:     resp.Nouls()["purchase_intent"].Noul,
		urgency:    resp.Nouls()["urgency"].Noul,
		supply:     resp.Nouls()["supply_concern"].Noul,
		competitor: resp.Nouls()["competitive_mention"].Noul,
		interest:   resp.Scores()["interest"].Score,
	}, nil
}
