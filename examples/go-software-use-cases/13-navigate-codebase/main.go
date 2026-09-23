// Command navigate-codebase chooses only among known repository paths at each level.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/13-navigate-codebase
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	description := "Where is the HTTP retry loop implemented?"
	if len(os.Args) > 1 {
		description = os.Args[1]
	}
	tree := map[string]map[string]string{
		"sdk":        {"client.go": "Client construction and API methods", "transport.go": "HTTP request and retry loop", "questions.go": "Typed question wire forms"},
		"examples":   {"quickstart/main.go": "Minimal SDK example", "retries-errors/main.go": "Retry and error example"},
		"governance": {"SECURITY.md": "Security reports", "CONTRIBUTING.md": "Contribution process"},
	}
	rootCriteria := typesafe.ChoiceCriteria{"other": "No known area fits"}
	for _, pair := range [][2]string{{"sdk", "Core SDK implementation"}, {"examples", "Runnable examples"}, {"governance", "Project policies"}} {
		rootCriteria[pair[0]] = pair[1]
	}
	rootResp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State:     description,
		Questions: typesafe.Questions{"area": typesafe.Choice{Instructions: "Which repository area is most likely?", Criteria: rootCriteria}},
	})
	if err != nil {
		fmt.Println("Search manually; area selection unavailable:", err)
		return
	}
	area := rootResp.Choices()["area"]
	if area.Confidence < 0.70 {
		type ranked struct {
			name string
			p    float64
		}
		options := []ranked{}
		for name := range tree {
			options = append(options, ranked{name, area.Probabilities[name]})
		}
		sort.SliceStable(options, func(i, j int) bool { return options[i].p > options[j].p })
		fmt.Printf("Area uncertain; inspect both %s and %s.\n", options[0].name, options[1].name)
		return
	}
	children, ok := tree[area.Choice]
	if !ok {
		fmt.Println("No known area selected; search manually.")
		return
	}
	childCriteria := typesafe.ChoiceCriteria{"other": "No listed file fits"}
	for path, detail := range children {
		childCriteria[path] = detail
	}
	childResp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State:     map[string]any{"description": description, "area": area.Choice},
		Questions: typesafe.Questions{"file": typesafe.Choice{Instructions: "Which existing file is most relevant?", Criteria: childCriteria}},
	})
	if err != nil {
		fmt.Println("File choice unavailable; inspect area:", area.Choice, err)
		return
	}
	file := childResp.Choices()["file"]
	if file.Confidence < 0.70 || file.Choice == "other" {
		fmt.Println("File uncertain; inspect area:", area.Choice)
		return
	}
	if _, ok := children[file.Choice]; !ok {
		fmt.Println("Unknown file selected; inspect manually.")
		return
	}
	path := file.Choice
	if area.Choice == "examples" {
		path = filepath.Join("examples", path)
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Println("Selected path is missing; inspect repository:", err)
		return
	}
	fmt.Println("candidate path:", path)
}
