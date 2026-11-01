// Command attached-assets demonstrates Client.AttachAssets, the attach_assets
// equivalent: attached images are uploaded by every call whose text names
// their reference, so several calls share one attachment without placing the
// image in each request. Holds are refcounted per reference and the returned
// detach function is single-shot; the idiom is "defer detach()".
//
// Images are an experimental feature: every request must opt in with the
// TypeSafe-Danger-Zone-Images header, set here once on the client.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/multimodal/attached-assets
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

	// Two nested scopes over the same image: each holds one reference, and
	// the asset stays attached until the last holder detaches.
	outerDetach, err := client.AttachAssets(photo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer outerDetach()

	innerDetach, err := client.AttachAssets(photo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// %s interpolates the image's reference, which the attachment resolves.
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: fmt.Sprintf("Here is a photo: %s", photo),
		Questions: typesafe.Questions{
			"is_red": typesafe.Noul{Instructions: "Is the photo a solid red square?"},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("call 1 (two holds): is_red=%.2f\n", resp.Nouls()["is_red"].Noul)

	innerDetach()
	innerDetach() // single-shot: a second call is a no-op, not a double release

	// The outer hold keeps the asset attached, so this call still uploads it.
	resp, err = client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: fmt.Sprintf("The same photo again: %s", photo),
		Questions: typesafe.Questions{
			"uniformity": typesafe.Score{
				Instructions: "How uniform is the color across the photo?",
				Criteria:     typesafe.ScoreCriteria{"patchy", "mostly uniform", "entirely uniform"},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	uniformity := resp.Scores()["uniformity"]
	fmt.Printf("call 2 (one hold): uniformity=%.0f (confidence %.2f)\n",
		uniformity.Score, uniformity.Confidence)
	if uniformity.Score >= 2 {
		fmt.Println("action: attachment survived the inner detach; verdict accepted")
	} else {
		fmt.Println("action: color reads patchy; inspect with TYPESAFE_LOG_LEVEL=debug")
	}
	// outerDetach runs via defer above, ending the attachment for later calls.
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
