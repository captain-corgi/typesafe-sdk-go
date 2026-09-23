// Command question-regression-evaluation records per-class errors and
// abstentions for a versioned Noul question on labeled fixtures. Compare its
// artifact with a reviewed matching baseline before declaring a regression.
// This live run is opt-in; set TYPESAFE_API_KEY and a pinned TYPESAFE_EVAL_MODEL.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

const questionVersion = "retry-failure-v2"
const instructions = "Does this report describe a failed retry attempt?"
const positiveAt, negativeAt = 0.90, 0.10 // Calibrate on labeled local examples.

var criteria = &typesafe.NoulCriteria{
	True:  "An operation was retried and the retry failed",
	False: "No retry failure is described",
}

type fixture struct{ ID, Text, Label string }
type classMetrics struct {
	Total, Errors, Abstentions int
}
type result struct {
	Fixture      fixture                `json:"fixture"`
	Model        string                 `json:"model"`
	Question     string                 `json:"question_version"`
	Instructions string                 `json:"instructions"`
	Criteria     *typesafe.NoulCriteria `json:"criteria"`
	PositiveAt   float64                `json:"positive_at"`
	NegativeAt   float64                `json:"negative_at"`
	Probability  *float64               `json:"probability,omitempty"`
	Prediction   string                 `json:"prediction,omitempty"`
	Outcome      string                 `json:"outcome"`
	Error        string                 `json:"error,omitempty"`
}
type report struct {
	RequestedModel  string                  `json:"requested_model"`
	QuestionVersion string                  `json:"question_version"`
	Instructions    string                  `json:"instructions"`
	Criteria        *typesafe.NoulCriteria  `json:"criteria"`
	PositiveAt      float64                 `json:"positive_at"`
	NegativeAt      float64                 `json:"negative_at"`
	Fixtures        []fixture               `json:"fixtures"`
	ReviewedBy      string                  `json:"reviewed_by,omitempty"`
	ReviewedAt      string                  `json:"reviewed_at,omitempty"`
	Current         map[string]classMetrics `json:"current"`
	Results         []result                `json:"results"`
}

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in evaluation command")
	}
	model := os.Getenv("TYPESAFE_EVAL_MODEL")
	if model == "" || strings.Contains(model, "latest") {
		log.Fatal("set TYPESAFE_EVAL_MODEL to a pinned model identifier")
	}
	client, err := typesafe.NewClient(typesafe.WithModel(model))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	fixtures := []fixture{
		{"F01", "The second attempt also returned HTTP 503.", "retry_failure"},
		{"F02", "After retrying, the request timed out again.", "retry_failure"},
		{"F03", "Three retries each failed with connection reset.", "retry_failure"},
		{"F04", "A retry was attempted, but it still failed.", "retry_failure"},
		{"F05", "The backoff elapsed and the next attempt failed too.", "retry_failure"},
		{"F06", "The initial request failed; no retry was attempted.", "not_retry_failure"},
		{"F07", "The retry succeeded on the next attempt.", "not_retry_failure"},
		{"F08", "Retries are configured but this call was successful.", "not_retry_failure"},
		{"F09", "The user manually sent a new unrelated request.", "not_retry_failure"},
		{"F10", "The operation failed once and stopped.", "not_retry_failure"},
	}
	r := report{
		RequestedModel: model, QuestionVersion: questionVersion, Instructions: instructions,
		Criteria: criteria, PositiveAt: positiveAt, NegativeAt: negativeAt, Fixtures: fixtures,
		Current: map[string]classMetrics{}, Results: make([]result, 0, len(fixtures)),
	}
	var baseline *report
	if len(os.Args) > 2 {
		log.Fatal("usage: go run ./examples/testing-use-cases/question-regression-evaluation [reviewed-baseline.json]")
	}
	if len(os.Args) == 2 {
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
		var previous report
		if err := json.Unmarshal(data, &previous); err != nil {
			log.Fatal("invalid baseline JSON: ", err)
		}
		if err := validateBaseline(previous, r); err != nil {
			log.Fatal("incompatible baseline: ", err)
		}
		baseline = &previous
	}
	for _, f := range fixtures {
		row := result{Fixture: f, Model: model, Question: questionVersion, Instructions: instructions, Criteria: criteria, PositiveAt: positiveAt, NegativeAt: negativeAt}
		metrics := r.Current[f.Label]
		metrics.Total++
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State:     f.Text,
			Questions: typesafe.Questions{"failed_retry": typesafe.Noul{Instructions: instructions, Criteria: criteria}},
		})
		if err != nil {
			row.Outcome, row.Error = "inconclusive", err.Error()
		} else if a, ok := resp.Nouls()["failed_retry"]; !ok {
			row.Outcome, row.Error = "inconclusive", "missing typed Noul answer"
		} else {
			row.Model = resp.Model
			p := a.Noul
			row.Probability = &p
			switch {
			case p >= positiveAt:
				row.Prediction = "retry_failure"
			case p <= negativeAt:
				row.Prediction = "not_retry_failure"
			default:
				row.Outcome = "inconclusive"
			}
			if row.Prediction != "" {
				if row.Prediction == f.Label {
					row.Outcome = "agreement_for_review"
				} else {
					row.Outcome = "disagreement_for_review"
					metrics.Errors++
				}
			}
		}
		if row.Outcome == "inconclusive" {
			metrics.Abstentions++
		}
		r.Current[f.Label] = metrics
		r.Results = append(r.Results, row)
		fmt.Printf("%s expected=%s prediction=%s outcome=%s\n", f.ID, f.Label, row.Prediction, row.Outcome)
	}
	path := fmt.Sprintf("question-regression-evaluation-%d.json", time.Now().UTC().UnixNano())
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(r); err != nil {
		file.Close()
		log.Fatal(err)
	}
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}
	for _, label := range []string{"retry_failure", "not_retry_failure"} {
		current := r.Current[label]
		errorRate := float64(current.Errors) / float64(current.Total)
		abstainRate := float64(current.Abstentions) / float64(current.Total)
		fmt.Printf("%s: %d/%d errors (%.2f), %d/%d abstentions (%.2f)\n",
			label, current.Errors, current.Total, errorRate, current.Abstentions, current.Total, abstainRate)
		if baseline != nil {
			prior := baseline.Current[label]
			priorError := float64(prior.Errors) / float64(prior.Total)
			priorAbstain := float64(prior.Abstentions) / float64(prior.Total)
			fmt.Printf("  reviewed baseline: error %.2f, abstain %.2f; increased error=%t, increased abstention=%t\n",
				priorError, priorAbstain, errorRate > priorError, abstainRate > priorAbstain)
		}
	}
	if baseline == nil {
		fmt.Println("comparison unavailable: add reviewed_by and RFC3339 reviewed_at to a human-reviewed prior artifact, then pass its path")
	} else {
		oldCriteria, _ := json.Marshal(baseline.Criteria)
		newCriteria, _ := json.Marshal(r.Criteria)
		fmt.Printf("baseline model=%s question=%s; current model=%s question=%s; inspect changed cases before release\n",
			baseline.RequestedModel, baseline.QuestionVersion, r.RequestedModel, r.QuestionVersion)
		fmt.Printf("question changes: version=%t instructions=%t criteria=%t\n",
			baseline.QuestionVersion != r.QuestionVersion, baseline.Instructions != r.Instructions, string(oldCriteria) != string(newCriteria))
	}
	fmt.Println("artifact:", path)
}

func validateBaseline(previous, current report) error {
	if previous.ReviewedBy == "" || previous.ReviewedAt == "" {
		return fmt.Errorf("baseline needs reviewed_by and reviewed_at")
	}
	if _, err := time.Parse(time.RFC3339, previous.ReviewedAt); err != nil {
		return fmt.Errorf("reviewed_at must be RFC3339: %w", err)
	}
	if previous.RequestedModel == "" || strings.Contains(previous.RequestedModel, "latest") || previous.QuestionVersion == "" || previous.Instructions == "" || previous.Criteria == nil {
		return fmt.Errorf("baseline is missing model or question identity")
	}
	if !reflect.DeepEqual(previous.Fixtures, current.Fixtures) {
		return fmt.Errorf("fixture IDs, texts, labels, or order changed")
	}
	if previous.PositiveAt != current.PositiveAt || previous.NegativeAt != current.NegativeAt {
		return fmt.Errorf("noul thresholds changed; error and abstention rates are not comparable")
	}
	oldCriteria, err := json.Marshal(previous.Criteria)
	if err != nil {
		return err
	}
	newCriteria, err := json.Marshal(current.Criteria)
	if err != nil {
		return err
	}
	if previous.QuestionVersion == current.QuestionVersion && (previous.Instructions != current.Instructions || string(oldCriteria) != string(newCriteria)) {
		return fmt.Errorf("instructions or criteria changed without a question version change")
	}
	if len(previous.Results) != len(previous.Fixtures) {
		return fmt.Errorf("baseline has incomplete fixture results")
	}
	recount := map[string]classMetrics{}
	for i, row := range previous.Results {
		if row.Fixture != previous.Fixtures[i] || row.Question != previous.QuestionVersion || row.Instructions != previous.Instructions || row.PositiveAt != previous.PositiveAt || row.NegativeAt != previous.NegativeAt || row.Model == "" {
			return fmt.Errorf("baseline result %d does not match its fixture or setup", i)
		}
		rowCriteria, err := json.Marshal(row.Criteria)
		if err != nil || string(rowCriteria) != string(oldCriteria) {
			return fmt.Errorf("baseline result %d has mismatched criteria", i)
		}
		m := recount[row.Fixture.Label]
		m.Total++
		switch row.Outcome {
		case "inconclusive":
			m.Abstentions++
		case "agreement_for_review":
			if row.Prediction != row.Fixture.Label {
				return fmt.Errorf("baseline result %d has inconsistent agreement", i)
			}
		case "disagreement_for_review":
			if row.Prediction == row.Fixture.Label || row.Prediction == "" {
				return fmt.Errorf("baseline result %d has inconsistent disagreement", i)
			}
			m.Errors++
		default:
			return fmt.Errorf("baseline result %d has unknown outcome", i)
		}
		recount[row.Fixture.Label] = m
	}
	if !reflect.DeepEqual(recount, previous.Current) {
		return fmt.Errorf("baseline metrics do not match fixture results")
	}
	return nil
}
