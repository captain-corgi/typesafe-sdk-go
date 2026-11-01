// Command image-sources demonstrates the three Image constructors: raw
// bytes, base64 text (decoded immediately, whitespace tolerated), and a file
// path (read lazily, when the request is prepared). However an image is
// constructed, the client sends it the same way: one multipart asset part
// per image, with the image's reference standing in inside the JSON body.
//
// Images are an experimental feature: every request must opt in with the
// TypeSafe-Danger-Zone-Images header, set here once on the client.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/multimodal/image-sources
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"

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

	// Invalid input is rejected here, before any network I/O.
	if _, err := typesafe.NewImageFromBase64("not base64!"); err != nil {
		fmt.Println("invalid base64 rejected up front:", err)
	}

	fromBytes := typesafe.NewImageFromBytes(pngBytes)
	fromBase64, err := typesafe.NewImageFromBase64(wrappedBase64(base64.StdEncoding.EncodeToString(pngBytes)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	file, err := os.CreateTemp("", "image-sources-*.png")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if _, err := file.Write(pngBytes); err != nil {
		file.Close()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	file.Close()
	fromFile := typesafe.NewImageFromFile(file.Name())

	// Images compare by identity, not content: identical bytes through three
	// constructors are three assets with three references, so the request
	// below carries three asset parts.
	fmt.Println("bytes:  ", fromBytes.Reference())
	fmt.Println("base64: ", fromBase64.Reference())
	fmt.Println("file:   ", fromFile.Reference())

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: []any{"Three photos of the same red square, built three ways:",
			fromBytes, fromBase64, fromFile},
		Questions: typesafe.Questions{
			"identical": typesafe.Noul{
				Instructions: "Are all three photos identical solid red squares?",
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	identical := resp.Nouls()["identical"].Noul
	fmt.Printf("identical: %.2f\n", identical)
	if identical > 0.5 {
		fmt.Println("action: the three constructions are interchangeable on the wire")
	} else {
		fmt.Println("action: the photos were not seen as identical; inspect with TYPESAFE_LOG_LEVEL=debug")
	}
	fmt.Println("request id:", resp.RequestID)
}

// wrappedBase64 inserts a line break every 76 characters, the MIME wrapping
// some producers emit; NewImageFromBase64 ignores it.
func wrappedBase64(text string) string {
	var wrapped strings.Builder
	for len(text) > 76 {
		wrapped.WriteString(text[:76])
		wrapped.WriteByte('\n')
		text = text[76:]
	}
	wrapped.WriteString(text)
	return wrapped.String()
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
