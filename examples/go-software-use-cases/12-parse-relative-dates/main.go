// Command parse-relative-dates resolves bounded date parts against an explicit clock.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/12-parse-relative-dates
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	command := "Rerun the flaky job next Thursday."
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	location := time.FixedZone("Asia/Saigon", 7*60*60)
	clock := time.Date(2026, time.September, 23, 9, 0, 0, 0, location)
	if exact := regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).FindString(command); exact != "" {
		date, err := time.ParseInLocation("2006-01-02", exact, location)
		if err != nil || date.Before(clock) {
			fmt.Println("Invalid or past ISO date; clarify the schedule.")
			return
		}
		fmt.Println("scheduled:", date.Add(9*time.Hour).Format(time.RFC3339))
		return
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: command,
		Questions: typesafe.Questions{
			"kind": typesafe.Choice{Instructions: "What date form does this command contain?", Criteria: typesafe.ChoiceCriteria{
				"relative_weekday": "A weekday relative to this or another week", "none": "No date", "other": "Another or unclear date form",
			}},
			"weekday": typesafe.Choice{Instructions: "Which weekday is specified?", Criteria: typesafe.ChoiceCriteria{
				"mon": "Monday", "tue": "Tuesday", "wed": "Wednesday", "thu": "Thursday", "fri": "Friday", "sat": "Saturday", "sun": "Sunday", "none": "No clear weekday",
			}},
			"week": typesafe.Choice{Instructions: "Which week is requested relative to the explicit clock?", Criteria: typesafe.ChoiceCriteria{
				"this": "The current Monday-to-Sunday week", "next": "The following Monday-to-Sunday week", "after_next": "The week after next", "other": "Unclear week",
			}},
		},
	})
	if err != nil {
		fmt.Println("Could not parse relative date; ask for ISO date:", err)
		return
	}
	choices := resp.Choices()
	if choices["kind"].Choice != "relative_weekday" || choices["kind"].Confidence < 0.75 || choices["weekday"].Confidence < 0.75 || choices["week"].Confidence < 0.75 {
		fmt.Println("Date parts uncertain; ask for an ISO date.")
		return
	}
	weekdays := map[string]int{"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6}
	weeks := map[string]int{"this": 0, "next": 1, "after_next": 2}
	day, validDay := weekdays[choices["weekday"].Choice]
	week, validWeek := weeks[choices["week"].Choice]
	if !validDay || !validWeek {
		fmt.Println("Unsupported date parts; ask for an ISO date.")
		return
	}
	daysSinceMonday := (int(clock.Weekday()) + 6) % 7
	monday := time.Date(clock.Year(), clock.Month(), clock.Day()-daysSinceMonday, 9, 0, 0, 0, location)
	scheduled := monday.AddDate(0, 0, 7*week+day)
	if !scheduled.After(clock) {
		fmt.Println("Resolved date is past; ask for clarification.")
		return
	}
	fmt.Println("scheduled:", scheduled.Format(time.RFC3339))
}
