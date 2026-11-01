// Command box-locate-and-crop demonstrates the Box question: the model
// locates what the instructions describe and answers with the box's edges as
// fractions of the image (top/bottom of the height, left/right of the
// width, each 0 to 1). This program turns the fractions back into pixels and
// crops the source image with the standard library image, image/png, and
// image/jpeg packages, writing the crop to disk as PNG and JPEG.
//
// Images are an experimental feature: every request must opt in with the
// TypeSafe-Danger-Zone-Images header, set here once on the client.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/multimodal/box-locate-and-crop
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// imageHeaders opts a client into the experimental images feature; the API
// rejects image requests without this header.
var imageHeaders = map[string]string{"TypeSafe-Danger-Zone-Images": "true"}

// size stays comfortably above the API's minimum image size.
const size = 512

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

	source := markerImage()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	photo := typesafe.NewImageFromBytes(encoded.Bytes())
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: photo,
		Questions: typesafe.Questions{
			"red_square": typesafe.Box{Instructions: "The red square in the center"},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	answer := resp.Boxes()["red_square"]
	edges := answer.Box
	fmt.Printf("box: top %.3f bottom %.3f left %.3f right %.3f\n",
		edges.Top, edges.Bottom, edges.Left, edges.Right)
	if answer.Confidence != nil {
		fmt.Printf("confidence: %.2f\n", *answer.Confidence)
	} else {
		fmt.Println("confidence: (model did not provide one)")
	}

	// Fractions become pixels against the source's own bounds; Intersect
	// clamps a box that drifts past an edge.
	bounds := source.Bounds()
	cropRect := image.Rect(
		int(edges.Left*float64(bounds.Dx())),
		int(edges.Top*float64(bounds.Dy())),
		int(edges.Right*float64(bounds.Dx())),
		int(edges.Bottom*float64(bounds.Dy())),
	).Intersect(bounds)
	crop := source.SubImage(cropRect)
	fmt.Printf("crop: %d x %d pixels (%.0f%% of the image)\n",
		crop.Bounds().Dx(), crop.Bounds().Dy(),
		100*float64(crop.Bounds().Dx()*crop.Bounds().Dy())/float64(bounds.Dx()*bounds.Dy()))

	pngPath, err := writeCrop("box-crop-*.png", func(w *os.File) error {
		return png.Encode(w, crop)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	jpegPath, err := writeCrop("box-crop-*.jpg", func(w *os.File) error {
		return jpeg.Encode(w, crop, &jpeg.Options{Quality: 90})
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("cropped PNG:", pngPath)
	fmt.Println("cropped JPEG:", jpegPath)

	// The drawn marker fills the center quarter, centered at (0.5, 0.5); a
	// box far from that is a review flag, not a crop instruction.
	centerX := (edges.Left + edges.Right) / 2
	centerY := (edges.Top + edges.Bottom) / 2
	if math.Abs(centerX-0.5) < 0.1 && math.Abs(centerY-0.5) < 0.1 {
		fmt.Println("action: box centers on the marker; keep the crop")
	} else {
		fmt.Println("action: box drifted from the marker; flag the crop for review")
	}
}

// markerImage draws a white canvas with a red square filling the center
// half, from size/4 to 3*size/4 on both axes.
func markerImage() *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := size / 4; y < 3*size/4; y++ {
		for x := size / 4; x < 3*size/4; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	return canvas
}

// writeCrop creates a temporary file and encodes the crop into it, returning
// the path.
func writeCrop(pattern string, encode func(*os.File) error) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := encode(file); err != nil {
		return "", err
	}
	return file.Name(), nil
}
