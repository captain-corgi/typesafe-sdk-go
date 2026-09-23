// Command upstream-problem-types maps opaque upstream prose to a reviewed
// public RFC 9457 problem type while preserving a trusted HTTP status.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/upstream-problem-types
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
}

var publicProblems = map[string]problem{
	"quota_exhausted":       {Type: "https://api.example.com/problems/quota-exhausted", Title: "Quota exhausted"},
	"temporary_unavailable": {Type: "https://api.example.com/problems/upstream-unavailable", Title: "Upstream unavailable"},
	"invalid_payload":       {Type: "https://api.example.com/problems/upstream-rejected", Title: "Upstream rejected request"},
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
	// The upstream transport gives us the status. The message is sanitized
	// before this function sees it; never forward stack traces or secrets.
	p, err := classifyFailure(ctx, client, http.StatusTooManyRequests, "Monthly request quota exhausted")
	if err != nil {
		fmt.Fprintln(os.Stderr, "classification:", err)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encoding:", err)
		os.Exit(1)
	}
	fmt.Printf("HTTP %d\nContent-Type: application/problem+json\n%s\n", p.Status, encoded)
}

func classifyFailure(ctx context.Context, client *typesafe.Client, upstreamStatus int, message string) (problem, error) {
	status := http.StatusBadGateway
	switch upstreamStatus {
	case http.StatusTooManyRequests:
		status = http.StatusTooManyRequests
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		status = http.StatusServiceUnavailable
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		status = http.StatusBadGateway // upstream rejection is not caller fault by default
	}
	fallback := problem{Type: "https://api.example.com/problems/upstream-failure", Title: "Upstream failure", Status: status}
	if len(message) == 0 || len(message) > 500 {
		return fallback, fmt.Errorf("sanitized upstream message missing or too long")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: message,
		Questions: typesafe.Questions{
			"failure": typesafe.Choice{
				Instructions: "Which known upstream failure does this sanitized message describe?",
				Criteria: typesafe.ChoiceCriteria{
					"quota_exhausted":       "An account or request quota has been used up",
					"temporary_unavailable": "A temporary outage or overload",
					"invalid_payload":       "Upstream rejected an invalid payload",
					"other":                 "None of these meanings is clear",
				},
			},
		},
	})
	if err != nil {
		return fallback, err
	}
	answer := resp.Choices()["failure"]
	known, ok := publicProblems[answer.Choice]
	if !ok || answer.Confidence < 0.80 {
		return fallback, nil
	}
	known.Status = status // Jev never chooses the HTTP status.
	return known, nil
}
