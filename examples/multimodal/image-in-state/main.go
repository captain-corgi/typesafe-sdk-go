// Command image-in-state demonstrates multimodal state: a PNG generated
// with the standard library sits in State beside text, and one call asks a
// Noul and a Choice about the same pixels.
//
// Images are an experimental feature: every request must opt in with the
// TypeSafe-Danger-Zone-Images header, set here once on the client.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/multimodal/image-in-state
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// imageHeaders opts a client into the experimental images feature; the API
// rejects image requests without this header.
var imageHeaders = map[string]string{"TypeSafe-Danger-Zone-Images": "true"}

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

	pngBytes, err := solidPNG(512, 512, color.RGBA{R: 255, A: 255})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	photo := typesafe.NewImageFromBytes(pngBytes)
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		// State mixes text and images freely; the client uploads the image
		// bytes as a multipart asset part and leaves the image's reference
		// in the JSON.
		State: []any{"Here is a photo:", photo},
		Questions: typesafe.Questions{
			"is_red": typesafe.Noul{
				Instructions: "Is the photo a solid red square?",
			},
			"dominant": typesafe.Choice{
				Instructions: "Which color fills the photo?",
				Criteria: typesafe.ChoiceCriteria{
					"red":   "Every pixel is red",
					"blue":  "Every pixel is blue",
					"white": "Every pixel is white",
				},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	isRed := resp.Nouls()["is_red"].Noul
	dominant := resp.Choices()["dominant"]
	fmt.Printf("is_red: %.2f\n", isRed)
	fmt.Printf("dominant: %s (confidence %.2f)\n", dominant.Choice, dominant.Confidence)
	if isRed > 0.5 && dominant.Choice == "red" {
		fmt.Println("action: both answers agree; accept the solid-red-square verdict")
	} else {
		fmt.Println("action: the answers disagree; route the photo to a human")
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
