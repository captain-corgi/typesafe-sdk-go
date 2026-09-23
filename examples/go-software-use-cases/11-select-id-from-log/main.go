// Command select-id-from-log assigns a role to exact IDs extracted by regexp.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/11-select-id-from-log
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	logText := "build-731 completed; build-742 failed during tests; rollback target is build-731"
	if len(os.Args) > 1 {
		logText = os.Args[1]
	}
	pattern := regexp.MustCompile(`\bbuild-[0-9]+\b`)
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range pattern.FindAllString(logText, -1) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		fmt.Println("No build ID found; inspect the log.")
		return
	}
	if len(ids) > 8 {
		fmt.Println("Too many build IDs for a bounded choice; narrow the log excerpt.")
		return
	}
	criteria := typesafe.ChoiceCriteria{"none": "None of the extracted IDs is the failed build"}
	for i, id := range ids {
		criteria[fmt.Sprintf("candidate_%d", i)] = id
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"log": logText, "candidate_build_ids": ids},
		Questions: typesafe.Questions{"failed_build": typesafe.Choice{
			Instructions: "Which extracted candidate is the build that failed?",
			Criteria:     criteria,
		}},
	})
	if err != nil {
		fmt.Println("Could not select failed build; inspect candidates:", ids, err)
		return
	}
	answer := resp.Choices()["failed_build"]
	if answer.Confidence < 0.75 || answer.Choice == "none" {
		fmt.Println("Failed build uncertain; inspect candidates:", ids)
		return
	}
	var selected string
	for i, id := range ids {
		if answer.Choice == fmt.Sprintf("candidate_%d", i) {
			selected = id // Exact bytes come from regexp, never a generated value.
		}
	}
	buildDatabase := map[string]bool{"build-731": true, "build-742": true}
	if selected == "" || !buildDatabase[selected] {
		fmt.Println("Selected build is absent from the build database; inspect manually.")
		return
	}
	fmt.Println("failed build:", selected)
}
