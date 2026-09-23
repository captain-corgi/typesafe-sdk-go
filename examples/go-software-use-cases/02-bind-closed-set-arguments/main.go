// Command bind-closed-set-arguments maps a request to allowlisted functions and enum arguments.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/02-bind-closed-set-arguments
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	command := "Show hourly errors for last week, including deploy markers."
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: command,
		Questions: typesafe.Questions{
			"function": typesafe.Choice{Instructions: "Which report is requested?", Criteria: typesafe.ChoiceCriteria{
				"show_errors": "Show error counts", "show_latency": "Show request latency", "other": "Neither report is requested",
			}},
			"resolution": typesafe.Choice{Instructions: "What time resolution is explicitly requested?", Criteria: typesafe.ChoiceCriteria{
				"hour": "Hourly buckets", "day": "Daily buckets", "unspecified": "No resolution stated",
			}},
			"window": typesafe.Choice{Instructions: "What time window is explicitly requested?", Criteria: typesafe.ChoiceCriteria{
				"day": "Last day", "week": "Last week", "month": "Last month", "unspecified": "No window stated",
			}},
			"deploys": typesafe.Noul{Instructions: "Does the user explicitly ask for deployment markers?"},
		},
	})
	if err != nil {
		fmt.Println("Report request needs manual handling:", err)
		return
	}
	choices := resp.Choices()
	fn, resolution, window := choices["function"], choices["resolution"], choices["window"]
	if fn.Confidence < 0.75 || resolution.Confidence < 0.65 || window.Confidence < 0.65 {
		fmt.Println("Please clarify the report, resolution, and time window.")
		return
	}
	if fn.Choice != "show_errors" && fn.Choice != "show_latency" {
		fmt.Println("No supported report selected.")
		return
	}
	if resolution.Choice != "hour" && resolution.Choice != "day" && resolution.Choice != "unspecified" {
		fmt.Println("Invalid resolution.")
		return
	}
	if window.Choice != "day" && window.Choice != "week" && window.Choice != "month" && window.Choice != "unspecified" {
		fmt.Println("Invalid time window.")
		return
	}
	if resolution.Choice == "unspecified" {
		resolution.Choice = "day"
	}
	if window.Choice == "unspecified" {
		window.Choice = "day"
	}
	includeDeploys := resp.Nouls()["deploys"].Noul >= 0.80
	switch fn.Choice {
	case "show_errors":
		showErrors(resolution.Choice, window.Choice, includeDeploys)
	case "show_latency":
		showLatency(resolution.Choice, window.Choice, includeDeploys)
	}
}

func showErrors(resolution, window string, deploys bool) {
	fmt.Printf("showErrors(resolution=%q, window=%q, deploys=%t)\n", resolution, window, deploys)
}

func showLatency(resolution, window string, deploys bool) {
	fmt.Printf("showLatency(resolution=%q, window=%q, deploys=%t)\n", resolution, window, deploys)
}
