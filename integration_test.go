package typesafe_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
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

// TestIntegrationImagesAndBox sends a live image with a Box question on
// jev-preview, using the documented TypeSafe-Danger-Zone-Images header.
func TestIntegrationImagesAndBox(t *testing.T) {
	key := integrationKey(t)
	client, err := typesafe.NewClient(typesafe.WithAPIKey(key))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// A real 512x512 PNG with a red centered square, mirroring the Python
	// live suite: the API rejects images below a minimum size (the SDK has
	// no client-side limits).
	const size = 512
	pixel := image.NewRGBA(image.Rect(0, 0, size, size))
	for x := size / 4; x < 3*size/4; x++ {
		for y := size / 4; y < 3*size/4; y++ {
			pixel.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, pixel); err != nil {
		t.Fatalf("encoding test PNG: %v", err)
	}
	photo, err := typesafe.NewImageFromBase64(base64.StdEncoding.EncodeToString(pngBuf.Bytes()))
	if err != nil {
		t.Fatalf("NewImageFromBase64: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: photo,
		Questions: typesafe.Questions{
			"red_square": typesafe.Box{Instructions: "The red square in the center"},
		},
		Model:        "jev-preview",
		ExtraHeaders: map[string]string{"TypeSafe-Danger-Zone-Images": "true"},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	box, ok := resp.Boxes()["red_square"]
	if !ok {
		t.Fatalf("no box answer for 'red_square': %v", resp.Answers)
	}
	for name, edge := range map[string]float64{
		"top": box.Box.Top, "bottom": box.Box.Bottom, "left": box.Box.Left, "right": box.Box.Right,
	} {
		if edge < 0 || edge > 1 {
			t.Errorf("box %s edge = %v, want a fraction in [0, 1]", name, edge)
		}
	}
	if box.Confidence != nil && (*box.Confidence < 0 || *box.Confidence > 1) {
		t.Errorf("confidence = %v, want nil or a fraction in [0, 1]", *box.Confidence)
	}
}
