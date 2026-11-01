// Command image-in-choice-criteria demonstrates images as Choice criteria:
// each label's description is itself an image, so the model answers by
// picking the label whose sample matches the image in State. State and
// criteria images are all uploaded as multipart asset parts of one request.
//
// Images are an experimental feature: every request must opt in with the
// TypeSafe-Danger-Zone-Images header, set here once on the client.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/multimodal/image-in-choice-criteria
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// imageHeaders opts a client into the experimental images feature; the API
// rejects image requests without this header.
var imageHeaders = map[string]string{"TypeSafe-Danger-Zone-Images": "true"}

// autoConfirmBelow sends low-confidence matches to a human.
const autoConfirmBelow = 0.75

func main() {
	client, err := typesafe.NewClient(typesafe.WithHeaders(imageHeaders))
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	redPNG, err := solidPNG(512, 512, color.RGBA{R: 255, A: 255})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	bluePNG, err := solidPNG(512, 512, color.RGBA{B: 255, A: 255})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: typesafe.NewImageFromBytes(redPNG),
		Questions: typesafe.Questions{
			"sample": typesafe.Choice{
				Instructions: "Which sample has the same color as the state image?",
				Criteria: typesafe.ChoiceCriteria{
					"red":  typesafe.NewImageFromBytes(redPNG),
					"blue": typesafe.NewImageFromBytes(bluePNG),
				},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	sample := resp.Choices()["sample"]
	fmt.Printf("sample: %s (confidence %.2f)\n", sample.Choice, sample.Confidence)
	for _, label := range slices.Sorted(maps.Keys(sample.Probabilities)) {
		fmt.Printf("p(%s): %.2f\n", label, sample.Probabilities[label])
	}
	if sample.Choice == "red" && sample.Confidence >= autoConfirmBelow {
		fmt.Println("action: confident match; auto-confirm the sample pairing")
	} else {
		fmt.Printf("action: match below %.2f or wrong label; queue for human review\n", autoConfirmBelow)
	}
	fmt.Println("request id:", resp.RequestID)
}

// solidPNG encodes a width x height single-color PNG. The API enforces a
// minimum image size, so examples stay at 512 x 512.
func solidPNG(width, height int, fill color.RGBA) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.SetRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
