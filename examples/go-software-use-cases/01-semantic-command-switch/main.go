// Command semantic-command-switch routes a natural-language command to a fixed Go handler.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/01-semantic-command-switch
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
	command := "Could you show whether the API is healthy right now?"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: command,
		Questions: typesafe.Questions{"action": typesafe.Choice{
			Instructions: "Which supported command does the user request?",
			Criteria: typesafe.ChoiceCriteria{
				"search_docs":    "Search the product documentation",
				"show_status":    "Show current system status",
				"explain_config": "Explain a configuration setting",
				"other":          "No supported command clearly fits",
			},
		}},
	})
	if err != nil {
		fmt.Println("Please restate the command; routing is unavailable:", err)
		return
	}
	action := resp.Choices()["action"]
	if action.Confidence < 0.70 {
		fmt.Println("Please clarify which command you want.")
		return
	}
	switch action.Choice {
	case "search_docs":
		fmt.Println("dispatch: searchDocs")
	case "show_status":
		fmt.Println("dispatch: showStatus")
	case "explain_config":
		fmt.Println("dispatch: explainConfig")
	default:
		fmt.Println("Please choose search, status, or configuration help.")
	}
}
