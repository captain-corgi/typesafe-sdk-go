// Command allowlisted-select maps prose to a tenant-scoped, parameterized
// PostgreSQL SELECT. It prints the query and arguments for an application to
// execute with database/sql and its own PostgreSQL driver.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/allowlisted-select
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	request := "Show unresolved regressions from last week"
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: request,
		Questions: typesafe.Questions{
			"status": typesafe.Choice{Instructions: "Which issue status is requested?", Criteria: typesafe.ChoiceCriteria{
				"open": "Unresolved or open", "closed": "Resolved or closed", "unspecified": "No status filter requested",
			}},
			"kind": typesafe.Choice{Instructions: "Which issue kind is requested?", Criteria: typesafe.ChoiceCriteria{
				"regression": "A previously working behavior broke", "feature": "A requested new capability", "other": "Another explicit issue kind", "unspecified": "No kind filter requested",
			}},
			"window": typesafe.Choice{Instructions: "Which time window is requested?", Criteria: typesafe.ChoiceCriteria{
				"last_week": "Previous complete calendar week", "last_month": "Previous complete calendar month", "unspecified": "No time window requested",
			}},
		},
	})
	if err != nil {
		log.Fatalf("ask for clarification; classification failed: %v", err)
	}

	choices := resp.Choices()
	for _, name := range []string{"status", "kind", "window"} {
		if choices[name].Confidence < 0.80 { // Illustrative; calibrate on application examples.
			log.Fatalf("ask for clarification: %s confidence %.2f", name, choices[name].Confidence)
		}
	}

	// All SQL fragments and identifiers are fixed in Go. Only values become arguments.
	clauses := []string{"tenant_id = $1"}
	args := []any{int64(42)} // Authorized tenant ID comes from the application session.
	add := func(column, value string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	switch choices["status"].Choice {
	case "open", "closed":
		add("status", choices["status"].Choice)
	case "unspecified":
	default:
		log.Fatal("ask for clarification: unexpected status")
	}
	switch choices["kind"].Choice {
	case "regression", "feature":
		add("kind", choices["kind"].Choice)
	case "unspecified":
	case "other":
		log.Fatal("ask for clarification: requested kind is outside the allowed vocabulary")
	default:
		log.Fatal("ask for clarification: unexpected kind")
	}

	// Calendar windows use UTC and half-open boundaries, calculated by Go.
	now := time.Now().UTC()
	var start, end time.Time
	switch choices["window"].Choice {
	case "last_week":
		monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
		end = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
		start = end.AddDate(0, 0, -7)
	case "last_month":
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		start = end.AddDate(0, -1, 0)
	case "unspecified":
	default:
		log.Fatal("ask for clarification: unexpected window")
	}
	if !start.IsZero() {
		args = append(args, start)
		clauses = append(clauses, fmt.Sprintf("opened_at >= $%d", len(args)))
		args = append(args, end)
		clauses = append(clauses, fmt.Sprintf("opened_at < $%d", len(args)))
	}
	query := "SELECT id, status, kind, opened_at FROM issues WHERE " + strings.Join(clauses, " AND ") + " ORDER BY opened_at DESC"
	fmt.Printf("request: %s\nSQL: %s\nargs: %#v\n", request, query, args)
}
