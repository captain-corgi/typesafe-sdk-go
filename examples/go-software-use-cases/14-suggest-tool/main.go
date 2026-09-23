// Command suggest-tool proposes one installed capability from a fixed registry.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/14-suggest-tool
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type capability struct {
	description string
	installed   bool
	allowed     bool
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	request := "Find every use of RetryPolicy in this repository."
	if len(os.Args) > 1 {
		request = os.Args[1]
	}
	registry := map[string]capability{
		"rg":      {"Search repository file contents", true, true},
		"gofmt":   {"Format Go source files", true, true},
		"go_test": {"Run Go unit tests", true, true},
	}
	criteria := typesafe.ChoiceCriteria{"none": "No listed tool is needed"}
	for name, tool := range registry {
		criteria[name] = tool.description
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: request,
		Questions: typesafe.Questions{
			"needed": typesafe.Noul{Instructions: "Would a repository tool help complete this request?"},
			"tool":   typesafe.Choice{Instructions: "Which listed tool best fits the request?", Criteria: criteria},
		},
	})
	if err != nil {
		fmt.Println("No tool proposed; inspect request manually:", err)
		return
	}
	choice := resp.Choices()["tool"]
	tool, known := registry[choice.Choice]
	if resp.Nouls()["needed"].Noul < 0.80 || choice.Confidence < 0.70 || !known || !tool.installed || !tool.allowed {
		fmt.Println("No tool proposed; inspect request manually.")
		return
	}
	fit, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State:     map[string]any{"request": request, "selected_tool": choice.Choice, "tool_description": tool.description},
		Questions: typesafe.Questions{"fits": typesafe.Noul{Instructions: "Does this selected tool directly help the request?"}},
	})
	if err != nil || fit.Nouls()["fits"].Noul < 0.80 {
		fmt.Println("Tool fit uncertain; inspect request manually.")
		return
	}
	fmt.Println("proposed installed tool:", choice.Choice)
}
