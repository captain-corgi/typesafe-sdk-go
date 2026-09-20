package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// ExampleClient_SystemOne answers a mixed set of typed questions about a
// support ticket in a single request.
func ExampleClient_SystemOne() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
		  "model": "jev-latest",
		  "usage": {"input_tokens": 12, "output_tokens": 3},
		  "answers": {
		    "department": {"type": "choice", "choice": "billing", "confidence": 0.93,
		                   "probabilities": {"billing": 0.93, "technical": 0.05, "other": 0.02}},
		    "frustration": {"type": "score", "score": 2.0, "confidence": 0.88,
		                    "legend": {"0": "calm", "1": "annoyed", "2": "upset"},
		                    "probabilities": {"0": 0.05, "1": 0.07, "2": 0.88}},
		    "is_urgent": {"type": "noul", "noul": 0.91}
		  }
		}`)
	}))
	defer server.Close()

	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("demo-key"),
		typesafe.WithBaseURL(server.URL), typesafe.WithAllowInsecureHTTP(),
		typesafe.WithAllowInsecureHTTP(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"document": "I was charged twice. Please fix this ASAP."},
		Questions: typesafe.Questions{
			"department": typesafe.Choice{
				Instructions: "What is this ticket about?",
				Criteria:     typesafe.ChoiceCriteria{"billing": nil, "technical": nil, "other": nil},
			},
			"frustration": typesafe.Score{
				Instructions: "How frustrated is the customer?",
				Criteria:     typesafe.ScoreCriteria{"calm", "annoyed", "upset"},
			},
			"is_urgent": typesafe.Noul{Instructions: "Is this urgent?"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("department:", resp.Choices()["department"].Choice)
	fmt.Println("urgency:", resp.Nouls()["is_urgent"].Noul)
	fmt.Println("frustration:", resp.Scores()["frustration"].Score)
	fmt.Println("input tokens:", *resp.Usage.InputTokens)
	// Output:
	// department: billing
	// urgency: 0.91
	// frustration: 2
	// input tokens: 12
}

// ExampleRawQuestion shows the raw dictionary passthrough
// for question fields this SDK does not model.
func ExampleRawQuestion() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model": "jev-latest", "usage": {},
		  "answers": {"spam": {"type": "noul", "noul": 0.02}}}`)
	}))
	defer server.Close()

	client, _ := typesafe.NewClient(typesafe.WithAPIKey("demo-key"), typesafe.WithBaseURL(server.URL), typesafe.WithAllowInsecureHTTP())
	defer client.Close()

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: "You have won a free prize, click here!",
		Questions: typesafe.Questions{
			"spam": typesafe.RawQuestion{
				"type":         "noul",
				"instructions": "Is this message spam?",
				"weight":       3,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("spam probability:", resp.Nouls()["spam"].Noul)
	// Output:
	// spam probability: 0.02
}

// ExampleModels lists the models available to the account.
func ExampleModels() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models": [
		  {"name": "jev-latest", "description": "General-purpose system one model.", "release_date": "2026-09-15"}
		]}`)
	}))
	defer server.Close()

	client, _ := typesafe.NewClient(typesafe.WithAPIKey("demo-key"), typesafe.WithBaseURL(server.URL), typesafe.WithAllowInsecureHTTP())
	defer client.Close()

	models, err := client.Models.List(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, model := range models.Models {
		fmt.Printf("%s (released %s): %s\n", model.Name, model.ReleaseDate, model.Description)
	}
	// Output:
	// jev-latest (released 2026-09-15): General-purpose system one model.
}

// ExampleRateLimitError shows matching typed errors with errors.As, including the
// server-requested retry delay on a rate-limit response.
func ExampleRateLimitError() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "125")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"message": "slow down"}`)
	}))
	defer server.Close()

	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, _ := typesafe.NewClient(
		typesafe.WithAPIKey("demo-key"),
		typesafe.WithBaseURL(server.URL), typesafe.WithAllowInsecureHTTP(),
		typesafe.WithAllowInsecureHTTP(),
		typesafe.WithRetry(policy),
	)
	defer client.Close()

	_, err := client.Models.List(context.Background(), nil)

	var rateLimit *typesafe.RateLimitError
	if errors.As(err, &rateLimit) {
		fmt.Println("rate limited; retry after ms:", *rateLimit.RetryAfterMs)
		return
	}
	fmt.Println("unexpected error:", err)
	// Output:
	// rate limited; retry after ms: 125
}

// ExampleRetryPolicy configures a client with a custom retry policy and
// per-call overrides.
func ExampleRetryPolicy() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models": []}`)
	}))
	defer server.Close()

	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 4
	policy.BackoffInitial = 200 * time.Millisecond
	policy.HTTPStatuses = map[int]struct{}{429: {}, 503: {}}

	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("demo-key"),
		typesafe.WithBaseURL(server.URL), typesafe.WithAllowInsecureHTTP(),
		typesafe.WithAllowInsecureHTTP(),
		typesafe.WithRetry(policy),
		typesafe.WithTimeout(5*time.Second),
		typesafe.WithHeaders(map[string]string{"X-Team": "search"}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// A single call can loosen the timeout and extend the retry budget.
	extended := typesafe.DefaultRetryPolicy()
	extended.Timeout = 2 * time.Minute
	if _, err := client.Models.List(context.Background(), &typesafe.ModelsListParams{
		Timeout: 30 * time.Second,
		Retry:   &extended,
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("done")
	// Output:
	// done
}
