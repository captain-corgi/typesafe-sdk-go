// Command semantic-code-linting demonstrates team-convention lints that
// grep cannot express: a battery of Noul violation checks plus a worst-case
// severity Score over one code snippet, with CI semantics — a nonzero exit
// when any convention is violated.
//
// It mirrors the "Semantic code linting" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/semantic-code-linting
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

const snippet = `def CalcPrice(qty, unitCost):
	# TODO: apply volume discounts for large orders
	try:
		return qty * unitCost
	except:
		pass`

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

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"file":     "pricing.py",
			"language": "python",
			"code":     snippet,
			"conventions": map[string]string{
				"naming": "snake_case for functions and arguments",
				"errors": "never catch an exception without logging or handling it",
				"todo":   "no TODO/FIXME markers on main",
			},
		},
		Questions: typesafe.Questions{
			"violates_naming": typesafe.Noul{
				Instructions: "Does this code violate the naming convention?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Names use camelCase or abbreviations where snake_case is required",
					False: "All names follow the convention",
				},
			},
			"swallows_errors": typesafe.Noul{
				Instructions: "Does this code catch an exception and continue without handling or logging it?",
			},
			"left_todo": typesafe.Noul{
				Instructions: "Does this code contain TODO/FIXME markers or stubbed behavior?",
			},
			"severity": typesafe.Score{
				Instructions: "If any convention is violated, how severe is the worst violation?",
				Criteria:     typesafe.ScoreCriteria{"style", "minor", "major"},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	violations := map[string]float64{
		"violates_naming": resp.Nouls()["violates_naming"].Noul,
		"swallows_errors": resp.Nouls()["swallows_errors"].Noul,
		"left_todo":       resp.Nouls()["left_todo"].Noul,
	}
	severity := resp.Scores()["severity"]

	failed := false
	for _, name := range sortedNames(violations) {
		value := violations[name]
		if value >= 0.5 {
			fmt.Printf("%s: FAIL (%.2f)\n", name, value)
			failed = true
		} else {
			fmt.Printf("%s: ok (%.2f)\n", name, value)
		}
	}
	fmt.Printf("worst severity: %.2f/2 (confidence %.2f)\n", severity.Score, severity.Confidence)

	if failed {
		fmt.Println("lint result: violations found")
		// CI semantics: a lint that never fails cannot gate a merge.
		os.Exit(1)
	}
	fmt.Println("lint result: clean")
}

// sortedNames keeps the printed findings deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
