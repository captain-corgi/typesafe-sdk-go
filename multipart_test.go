package typesafe

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestEncodeMultipartFraming(t *testing.T) {
	tests := []struct {
		name  string
		parts []part
		want  string
	}{
		{
			name: "single part without filename",
			parts: []part{
				{name: "request", contentType: "application/json", content: []byte(`{"a":1}`)},
			},
			want: "--%[1]s\r\n" +
				"Content-Disposition: form-data; name=\"request\"\r\n" +
				"Content-Type: application/json\r\n" +
				"\r\n" +
				"{\"a\":1}\r\n" +
				"--%[1]s--\r\n",
		},
		{
			name: "part with filename",
			parts: []part{
				{name: "request", contentType: "application/json", content: []byte(`{}`)},
				{name: "asset-0", contentType: "image/png", content: pngFixture, filename: "asset-0"},
			},
			want: "--%[1]s\r\n" +
				"Content-Disposition: form-data; name=\"request\"\r\n" +
				"Content-Type: application/json\r\n" +
				"\r\n" +
				"{}\r\n" +
				"--%[1]s\r\n" +
				"Content-Disposition: form-data; name=\"asset-0\"; filename=\"asset-0\"\r\n" +
				"Content-Type: image/png\r\n" +
				"\r\n" +
				"\x89PNG\r\n\x1a\nimage data\r\n" +
				"--%[1]s--\r\n",
		},
		{
			name: "empty part content keeps its framing",
			parts: []part{
				{name: "request", contentType: "application/json"},
				{name: "asset-0", contentType: "application/octet-stream", content: nil, filename: "asset-0"},
			},
			want: "--%[1]s\r\n" +
				"Content-Disposition: form-data; name=\"request\"\r\n" +
				"Content-Type: application/json\r\n" +
				"\r\n" +
				"\r\n" +
				"--%[1]s\r\n" +
				"Content-Disposition: form-data; name=\"asset-0\"; filename=\"asset-0\"\r\n" +
				"Content-Type: application/octet-stream\r\n" +
				"\r\n" +
				"\r\n" +
				"--%[1]s--\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentType, body := encodeMultipart(tt.parts)
			if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
				t.Fatalf("content type = %q", contentType)
			}
			boundary := strings.TrimPrefix(contentType, "multipart/form-data; boundary=")
			if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(boundary) {
				t.Errorf("boundary %q is not 32 lowercase hex characters", boundary)
			}
			want := fmt.Sprintf(tt.want, boundary)
			if !bytes.Equal(body, []byte(want)) {
				t.Errorf("body mismatch:\n want %q\n  got %q", want, body)
			}
		})
	}
}

func TestEncodeMultipartBoundaryUnique(t *testing.T) {
	parts := []part{{name: "request", contentType: "application/json", content: []byte("{}")}}
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		contentType, _ := encodeMultipart(parts)
		boundary := strings.TrimPrefix(contentType, "multipart/form-data; boundary=")
		if seen[boundary] {
			t.Fatalf("boundary %q repeated", boundary)
		}
		seen[boundary] = true
	}
}
