package typesafe_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// integrationKey returns the API key for live tests, skipping when absent.
func integrationKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY not set; skipping live API test")
	}
	return key
}

func TestIntegrationSystemOne(t *testing.T) {
	key := integrationKey(t)
	client, err := typesafe.NewClient(typesafe.WithAPIKey(key))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: "I was charged twice. Please fix this ASAP.",
		Questions: typesafe.Questions{
			"category": typesafe.Choice{
				Instructions: "What is this ticket about?",
				Criteria:     typesafe.ChoiceCriteria{"billing": nil, "technical": nil, "other": nil},
			},
			"urgent": typesafe.Noul{Instructions: "Is this urgent?"},
			"tone": typesafe.Score{
				Instructions: "How polite is the tone?",
				Criteria:     typesafe.ScoreCriteria{"rude", "neutral", "polite"},
			},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if resp.Model == "" || resp.RequestID == "" {
		t.Errorf("missing metadata: %+v", resp)
	}
	if category, ok := resp.Choices()["category"]; !ok {
		t.Error("no choice answer for 'category'")
	} else if category.Choice == "" {
		t.Error("empty choice label")
	}
	if urgent, ok := resp.Nouls()["urgent"]; !ok {
		t.Error("no noul answer for 'urgent'")
	} else if urgent.Noul < 0 || urgent.Noul > 1 {
		t.Errorf("noul = %v, want within [0, 1]", urgent.Noul)
	}
	if tone, ok := resp.Scores()["tone"]; !ok {
		t.Error("no score answer for 'tone'")
	} else if len(tone.Legend) != 3 {
		t.Errorf("legend = %v, want three rubric levels", tone.Legend)
	}
}

func TestIntegrationModelsList(t *testing.T) {
	key := integrationKey(t)
	client, err := typesafe.NewClient(typesafe.WithAPIKey(key))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	models, err := client.Models.List(ctx, nil)
	if err != nil {
		t.Fatalf("Models.List: %v", err)
	}
	if len(models.Models) == 0 {
		t.Fatal("expected at least one model")
	}
	for _, model := range models.Models {
		if model.Name == "" || model.ReleaseDate == "" {
			t.Errorf("incomplete model card: %+v", model)
		}
	}
}
