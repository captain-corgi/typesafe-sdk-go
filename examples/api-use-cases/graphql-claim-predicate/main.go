// Command graphql-claim-predicate models a nullable
// Document.supportsClaim(claim: String!): Boolean resolver.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/graphql-claim-predicate
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type document struct {
	ID, Excerpt string
}

type graphQLResult struct {
	Data struct {
		Document struct {
			SupportsClaim *bool `json:"supportsClaim"`
		} `json:"document"`
	} `json:"data"`
	Errors []graphQLError `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string   `json:"message"`
	Path    []string `json:"path"`
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

	// Retrieval and document authorization have already selected this record.
	doc := document{ID: "doc-27", Excerpt: "The warranty covers parts and labor for 24 months after purchase."}
	claim := "The warranty lasts two years."
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	value, err := resolveSupportsClaim(ctx, client, doc, claim)
	var result graphQLResult
	result.Data.Document.SupportsClaim = value
	if err != nil {
		result.Errors = []graphQLError{{Message: "Claim support could not be determined", Path: []string{"document", "supportsClaim"}}}
		fmt.Fprintln(os.Stderr, "resolver:", err)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encoding:", err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}

func resolveSupportsClaim(ctx context.Context, client *typesafe.Client, doc document, claim string) (*bool, error) {
	if doc.ID == "" || len(doc.Excerpt) == 0 || len(doc.Excerpt) > 1500 || len(claim) == 0 || len(claim) > 300 {
		return nil, fmt.Errorf("invalid document excerpt or claim")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: map[string]any{"excerpt": doc.Excerpt, "claim": claim},
		Questions: typesafe.Questions{
			"supports": typesafe.Noul{
				Instructions: "Does the document excerpt directly support the claim, rather than merely mention similar terms?",
				Criteria: &typesafe.NoulCriteria{
					True:  "The excerpt entails the claim",
					False: "The claim is contradicted or not established by the excerpt",
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	p := resp.Nouls()["supports"].Noul
	switch {
	case p >= 0.80:
		answer := true
		return &answer, nil
	case p <= 0.20:
		answer := false
		return &answer, nil
	default:
		return nil, fmt.Errorf("ambiguous claim support probability %.2f", p)
	}
}
