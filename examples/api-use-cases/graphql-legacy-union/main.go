// Command graphql-legacy-union models abstract type resolution for a union
// with Incident, MaintenanceNotice, and UnknownRecord members.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/graphql-legacy-union
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type legacyRecord struct {
	ID, Description, ReliableType string
}

var unionMembers = map[string]string{
	"incident":           "Incident",
	"maintenance_notice": "MaintenanceNotice",
	"unknown":            "UnknownRecord",
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

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	r := legacyRecord{ID: "legacy-94", Description: "Planned database maintenance Sunday from 01:00 to 02:00 UTC."}
	typeName, err := resolveType(ctx, client, r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolver used UnknownRecord:", err)
	}
	// The GraphQL executor then calls field resolvers for this declared type.
	fmt.Printf("legacyRecord(id: %q) { __typename: %q }\n", r.ID, typeName)
}

func resolveType(ctx context.Context, client *typesafe.Client, r legacyRecord) (string, error) {
	if declared, ok := unionMembers[r.ReliableType]; ok {
		return declared, nil
	}
	if len(r.Description) == 0 || len(r.Description) > 1000 {
		return unionMembers["unknown"], fmt.Errorf("description missing or too long")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: r.Description,
		Questions: typesafe.Questions{
			"record_type": typesafe.Choice{
				Instructions: "Which declared LegacyRecord object type does this description support?",
				Criteria: typesafe.ChoiceCriteria{
					"incident":           "An unplanned outage, fault, or security incident",
					"maintenance_notice": "A planned maintenance window or change notice",
					"unknown":            "Neither declared type is supported",
				},
			},
		},
	})
	if err != nil {
		return unionMembers["unknown"], err
	}
	answer := resp.Choices()["record_type"]
	declared, ok := unionMembers[answer.Choice]
	if !ok || answer.Confidence < 0.80 {
		return unionMembers["unknown"], nil
	}
	return declared, nil
}
