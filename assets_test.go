package typesafe

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var referenceShape = regexp.MustCompile(`^<image:[0-9a-f]{32}>$`)

// jsonResponse records an HTTP response for round-tripper stubs.
func jsonResponse(status int, body string) *http.Response {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, status, body)
	return recorder.Result()
}

func TestImageReference(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	if !referenceShape.MatchString(image.Reference()) {
		t.Errorf("reference %q does not match the reference shape", image.Reference())
	}
	if image.Reference() == NewImageFromBytes(pngFixture).Reference() {
		t.Error("references must be unique per instance")
	}
	if image.String() != image.Reference() {
		t.Errorf("String() = %q, Reference() = %q", image.String(), image.Reference())
	}
	if got := fmt.Sprintf("photo %s here", image); !strings.Contains(got, image.Reference()) {
		t.Errorf("Sprintf interpolation = %q", got)
	}
	if got := fmt.Sprintf("%v", image); got != image.Reference() {
		t.Errorf("%%v interpolation = %q", got)
	}
	encoded, err := image.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if want := `"` + image.Reference() + `"`; string(encoded) != want {
		t.Errorf("MarshalJSON = %s, want %s", encoded, want)
	}
}

func TestImageIdentity(t *testing.T) {
	first := NewImageFromBytes(pngFixture)
	second := NewImageFromBytes(pngFixture)
	if first == second {
		t.Error("images built from identical bytes must be distinct assets")
	}
	images := map[*Image]bool{first: true, second: true, NewImageFromBytes(bytes.Clone(pngFixture)): true}
	if len(images) != 3 {
		t.Errorf("images must be distinct map keys, got %d", len(images))
	}
}

func TestNewImageFromBase64(t *testing.T) {
	wrap := func(encoded string) string {
		var wrapped strings.Builder
		for index := 0; index < len(encoded); index += 16 {
			end := min(index+16, len(encoded))
			wrapped.WriteString(encoded[index:end])
			wrapped.WriteString("\r\n")
		}
		return wrapped.String()
	}
	for _, tt := range []struct {
		name    string
		data    []byte
		content string
	}{
		{"png", pngFixture, "image/png"},
		{"jpeg", jpegFixture, "image/jpeg"},
	} {
		for _, encode := range []struct {
			name string
			wrap func(string) string
		}{
			{"standard", func(s string) string { return s }},
			{"wrapped", wrap},
			{"crlf-terminated", func(s string) string { return s + "\r\n" }},
			{"ascii-separators", func(s string) string {
				var separated strings.Builder
				for index := 0; index < len(s); index += 8 {
					end := min(index+8, len(s))
					separated.WriteString(s[index:end])
					separated.WriteString("\x1c\x1e")
				}
				return separated.String()
			}},
		} {
			t.Run(tt.name+"/"+encode.name, func(t *testing.T) {
				// Whitespace, such as the line breaks of wrapped base64, is
				// ignored.
				image, err := NewImageFromBase64(encode.wrap(base64.StdEncoding.EncodeToString(tt.data)))
				if err != nil {
					t.Fatalf("NewImageFromBase64: %v", err)
				}
				client, transport := newAssetCaptureClient(t)
				if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "Is it damaged?"}}, map[string]*Image{"<photo>": image}); err != nil {
					t.Fatalf("SystemOne: %v", err)
				}
				request := transport.requests[0]
				entry := requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
				if !bytes.Equal(entry["data"].([]byte), tt.data) {
					t.Errorf("sent bytes = %q, want %q", entry["data"], tt.data)
				}
				if entry["content_type"] != tt.content {
					t.Errorf("content type = %v, want %v", entry["content_type"], tt.content)
				}
			})
		}
	}
}

func TestNewImageFromBase64Errors(t *testing.T) {
	tests := []struct {
		text    string
		message string
	}{
		{"", "Image data must contain non-empty ASCII base64 text."},
		{" \n", "Image data must contain non-empty ASCII base64 text."},
		{" \t\r\n", "Image data must contain non-empty ASCII base64 text."},
		{"iVBO\xa0Rw0K", "Image data must contain non-empty ASCII base64 text."},
		{"iVBORw0KGgoé", "Image data must contain non-empty ASCII base64 text."},
		{string(pngFixture), "Image data must contain non-empty ASCII base64 text."},
		{"!!!!" + base64.StdEncoding.EncodeToString(pngFixture), "Image data must contain valid base64 text."},
		{"iVBORw0", "Image data must contain valid base64 text."},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.text), func(t *testing.T) {
			// Base64 text is decoded immediately, so invalid text fails
			// before any request.
			_, err := NewImageFromBase64(tt.text)
			var sdkErr *TypeSafeError
			if !errors.As(err, &sdkErr) || err.Error() != tt.message {
				t.Fatalf("error = %v, want %q", err, tt.message)
			}
		})
	}
}

func TestSniffedContentTypes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"png", pngFixture, "image/png"},
		{"jpeg", jpegFixture, "image/jpeg"},
		{"gif", []byte("GIF89a...image data"), "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := newAssetCaptureClient(t)
			if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": NewImageFromBytes(tt.data)}); err != nil {
				t.Fatalf("SystemOne: %v", err)
			}
			request := transport.requests[0]
			entry := requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
			if entry["content_type"] != tt.want {
				t.Errorf("sniffed content type = %v, want %v", entry["content_type"], tt.want)
			}
			if !bytes.Equal(entry["data"].([]byte), tt.data) {
				t.Errorf("sent bytes = %q, want %q", entry["data"], tt.data)
			}
		})
	}
}

func TestNoAssets(t *testing.T) {
	for _, assets := range []map[string]*Image{nil, {}} {
		client, transport := newAssetCaptureClient(t)
		if _, err := systemOneWithAssets(t, client, "text", Questions{"q": Noul{Instructions: "Is it damaged?"}}, assets); err != nil {
			t.Fatalf("SystemOne: %v", err)
		}
		request := transport.requests[0]
		if got := request.request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body := requestBody(t, "application/json", request.body)
		if _, present := body["assets"]; present {
			// The API rejects an empty assets, so the key is left out.
			t.Errorf("assets key must be omitted, got %v", body["assets"])
		}
	}
}

func TestLargeImageUnchanged(t *testing.T) {
	data := bytes.Repeat(pngFixture, 1024) // 100 KiB
	client, transport := newAssetCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": NewImageFromBytes(data)}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	entry := requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
	if !bytes.Equal(entry["data"].([]byte), data) {
		t.Errorf("sent %d bytes, want %d unchanged bytes", len(entry["data"].([]byte)), len(data))
	}
}

func TestSubsliceImage(t *testing.T) {
	container := append(bytes.Repeat([]byte{0}, 64), pngFixture...)
	offset := 64
	image := NewImageFromBytes(container[offset : offset+len(pngFixture)])
	client, transport := newAssetCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	entry := requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
	if !bytes.Equal(entry["data"].([]byte), pngFixture) {
		t.Errorf("sent bytes = %q, want the subslice %q", entry["data"], pngFixture)
	}
}

func TestInvalidAssetsBeforeNetwork(t *testing.T) {
	tests := []struct {
		name    string
		assets  map[string]*Image
		message string
	}{
		{"empty alias", map[string]*Image{"": NewImageFromBytes(pngFixture)}, "Asset aliases must be non-empty strings."},
		{"whitespace alias", map[string]*Image{"  ": NewImageFromBytes(pngFixture)}, "Asset aliases must be non-empty strings."},
		{"nil asset", map[string]*Image{"<photo>": nil}, `Asset '<photo>' must be an asset.`},
		{"empty data", map[string]*Image{"<photo>": NewImageFromBytes(nil)}, `Asset '<photo>': Image data is empty.`},
		{"unreadable file", map[string]*Image{"<photo>": NewImageFromFile("missing.png")}, `Asset '<photo>': Image file 'missing.png' could not be read`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := newAssetCaptureClient(t)
			_, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "Is it damaged?"}}, tt.assets)
			var sdkErr *TypeSafeError
			if !errors.As(err, &sdkErr) {
				t.Fatalf("expected *TypeSafeError, got %T: %v", err, err)
			}
			if !strings.HasPrefix(err.Error(), tt.message) {
				t.Fatalf("error = %q, want prefix %q", err.Error(), tt.message)
			}
			if count := transport.requestCount(); count != 0 {
				t.Fatalf("invalid assets reached the network (%d requests)", count)
			}
		})
	}
}

func TestImagePaths(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{{"png", pngFixture, "image/png"}, {"jpeg", jpegFixture, "image/jpeg"}} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "photo")
			image := NewImageFromFile(path)
			// The file is read when the request is prepared, not when the
			// image is created.
			if err := os.WriteFile(path, tt.data, 0o600); err != nil {
				t.Fatalf("writing image file: %v", err)
			}
			client, transport := newAssetCaptureClient(t)
			if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
				t.Fatalf("SystemOne: %v", err)
			}
			request := transport.requests[0]
			entry := requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
			if entry["content_type"] != tt.want {
				t.Errorf("content type = %v, want %v", entry["content_type"], tt.want)
			}
			if !bytes.Equal(entry["data"].([]byte), tt.data) {
				t.Errorf("sent bytes = %q", entry["data"])
			}
		})
	}
}

func TestFirstFailingAlias(t *testing.T) {
	client, transport := newAssetCaptureClient(t)
	assets := map[string]*Image{
		"<empty>":   NewImageFromBytes(nil),
		"<missing>": NewImageFromFile("missing.png"),
	}
	_, err := systemOneWithAssets(t, client, "<empty> <missing>", Questions{"q": Noul{Instructions: "?"}}, assets)
	// The first failing alias in sorted-key order is named.
	want := `Asset '<empty>': Image data is empty.`
	var sdkErr *TypeSafeError
	if !errors.As(err, &sdkErr) || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if transport.requestCount() != 0 {
		t.Fatal("invalid assets reached the network")
	}
}

func TestImagesAreResolvedOnce(t *testing.T) {
	sentEntry := func(t *testing.T, transport *trackingTransport) map[string]any {
		t.Helper()
		request := transport.requests[len(transport.requests)-1]
		return requestBody(t, request.request.Header.Get("Content-Type"), request.body)["assets"].(map[string]any)["<photo>"].(map[string]any)
	}

	t.Run("bytes", func(t *testing.T) {
		source := bytes.Clone(pngFixture)
		image := NewImageFromBytes(source)
		client, transport := newAssetCaptureClient(t)
		if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
			t.Fatalf("first SystemOne: %v", err)
		}
		// Later requests reuse the first resolution, even after the data
		// changes.
		copy(source, jpegFixture)
		if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
			t.Fatalf("second SystemOne: %v", err)
		}
		entry := sentEntry(t, transport)
		if !bytes.Equal(entry["data"].([]byte), pngFixture) {
			t.Errorf("resolved payload changed after source mutation: %q", entry["data"])
		}
		if entry["content_type"] != "image/png" {
			t.Errorf("resolved content type changed: %v", entry["content_type"])
		}
	})

	t.Run("path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "photo.png")
		image := NewImageFromFile(path)
		if err := os.WriteFile(path, pngFixture, 0o600); err != nil {
			t.Fatalf("writing image file: %v", err)
		}
		client, transport := newAssetCaptureClient(t)
		if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
			t.Fatalf("first SystemOne: %v", err)
		}
		if err := os.WriteFile(path, jpegFixture, 0o600); err != nil {
			t.Fatalf("rewriting image file: %v", err)
		}
		if _, err := systemOneWithAssets(t, client, "<photo>", Questions{"q": Noul{Instructions: "?"}}, map[string]*Image{"<photo>": image}); err != nil {
			t.Fatalf("second SystemOne: %v", err)
		}
		entry := sentEntry(t, transport)
		if !bytes.Equal(entry["data"].([]byte), pngFixture) {
			t.Errorf("resolved payload changed after file rewrite: %q", entry["data"])
		}
		if entry["content_type"] != "image/png" {
			t.Errorf("resolved content type changed: %v", entry["content_type"])
		}
	})
}

func TestAssetPayloadReusedOnRetry(t *testing.T) {
	data := bytes.Clone(pngFixture)
	var bodies [][]byte
	var contentTypes []string
	attempts := 0
	client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, body)
		contentTypes = append(contentTypes, req.Header.Get("Content-Type"))
		if attempts == 1 {
			// Mutate the source between attempts; the retry must resend the
			// first resolution byte-identically.
			copy(data, jpegFixture)
			return jsonResponse(500, `{"message": "try again"}`), nil
		}
		return jsonResponse(200, assetOKBody), nil
	})
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 1
	policy.BackoffInitial = 0
	policy.BackoffMax = 0
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "<photo>",
		Questions: Questions{"q": Noul{Instructions: "Is it damaged?"}},
		Assets:    map[string]*Image{"<photo>": NewImageFromBytes(data)},
		Retry:     &policy,
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Error("retry payloads must be byte-identical")
	}
	entry := requestBody(t, contentTypes[0], bodies[0])["assets"].(map[string]any)["<photo>"].(map[string]any)
	if !bytes.Equal(entry["data"].([]byte), pngFixture) {
		t.Errorf("retry carried mutated bytes: %q", entry["data"])
	}
}

func TestMultipartUploadGolden(t *testing.T) {
	image := NewImageFromBytes(bytes.Clone(pngFixture))
	client, transport := newAssetCaptureClient(t)
	assets := map[string]*Image{
		"<a>": image,
		"<b>": image,
		"<c>": mustImageFromBase64(t, base64.StdEncoding.EncodeToString(jpegFixture)),
	}
	if _, err := systemOneWithAssets(t, client, "<a> <b> <c>", Questions{"q": Noul{Instructions: "Is it damaged?"}}, assets); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	boundary := strings.TrimPrefix(contentType, "multipart/form-data; boundary=")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(boundary) {
		t.Errorf("boundary %q is not 32 lowercase hex characters", boundary)
	}
	parts := multipartParts(t, contentType, request.body)
	if len(parts) != 4 {
		t.Fatalf("parts = %v, want [request asset-0 asset-1 asset-2]", keysOf(parts))
	}
	requestPart, ok := parts["request"]
	if !ok {
		t.Fatal("request part missing")
	}
	if !bytes.Contains(requestPart.head, []byte("Content-Type: application/json")) {
		t.Errorf("request part head = %q", requestPart.head)
	}
	if bytes.Contains(requestPart.head, []byte("filename=")) {
		t.Errorf("request part must carry no filename: %q", requestPart.head)
	}
	rawRequest := decodeJSONMap(t, requestPart.content)
	compareJSON(t, map[string]any{
		"<a>": map[string]any{"type": "image", "part": "asset-0"},
		"<b>": map[string]any{"type": "image", "part": "asset-1"},
		"<c>": map[string]any{"type": "image", "part": "asset-2"},
	}, rawRequest["assets"])
	body := requestBody(t, contentType, request.body)
	if body["state"] != "<a> <b> <c>" {
		t.Errorf("state = %v", body["state"])
	}
	// The API requires each part to be claimed by exactly one asset, so an
	// image under several keys has a part per key.
	for name, data := range map[string][]byte{"asset-0": pngFixture, "asset-1": pngFixture, "asset-2": jpegFixture} {
		part, ok := parts[name]
		if !ok {
			t.Fatalf("part %s missing", name)
		}
		wantType := "image/png"
		if name == "asset-2" {
			wantType = "image/jpeg"
		}
		wantHead := fmt.Sprintf("Content-Disposition: form-data; name=%q; filename=%q\r\nContent-Type: %s", name, name, wantType)
		if string(part.head) != wantHead {
			t.Errorf("part %s head = %q, want %q", name, part.head, wantHead)
		}
		if !bytes.Equal(part.content, data) {
			t.Errorf("part %s content = %q", name, part.content)
		}
	}
	if !bytes.HasSuffix(request.body, []byte("--"+boundary+"--\r\n")) {
		t.Error("body must end with the closing boundary marker")
	}
}

func TestUnreferencedAssetsStillSent(t *testing.T) {
	client, transport := newAssetCaptureClient(t)
	assets := map[string]*Image{
		"<used>":   NewImageFromBytes(pngFixture),
		"<unused>": NewImageFromBytes(jpegFixture),
	}
	if _, err := systemOneWithAssets(t, client, "Look at <used>", Questions{"q": Noul{Instructions: "?"}}, assets); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	// Every Assets entry is sent — unreferenced included — and sorted aliases
	// number their parts first.
	raw := decodeJSONMap(t, multipartParts(t, contentType, request.body)[requestPartName].content)
	compareJSON(t, map[string]any{
		"<unused>": map[string]any{"type": "image", "part": "asset-0"},
		"<used>":   map[string]any{"type": "image", "part": "asset-1"},
	}, raw["assets"])
	parts := multipartParts(t, contentType, request.body)
	if !bytes.Equal(parts["asset-0"].content, jpegFixture) {
		t.Error("the unreferenced asset's part is missing or wrong")
	}
}

func TestUnreadableUnreferencedAssetErrors(t *testing.T) {
	client, transport := newAssetCaptureClient(t)
	assets := map[string]*Image{
		"<ok>":      NewImageFromBytes(pngFixture),
		"<missing>": NewImageFromFile("missing.png"),
	}
	_, err := systemOneWithAssets(t, client, "Look at <ok>", Questions{"q": Noul{Instructions: "?"}}, assets)
	want := `Asset '<missing>': Image file 'missing.png' could not be read`
	var sdkErr *TypeSafeError
	if !errors.As(err, &sdkErr) || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("error = %v, want prefix %q", err, want)
	}
	if transport.requestCount() != 0 {
		t.Fatal("unreadable asset reached the network")
	}
}

func TestImageAsWholeState(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, transport := newAssetCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, image, Questions{"q": Noul{Instructions: "Is it damaged?"}}, nil); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	body := requestBody(t, contentType, request.body)
	if body["state"] != image.Reference() {
		t.Errorf("state = %v, want the image's reference", body["state"])
	}
	entry := body["assets"].(map[string]any)[image.Reference()].(map[string]any)
	if !bytes.Equal(entry["data"].([]byte), pngFixture) {
		t.Errorf("data = %q", entry["data"])
	}
}

func TestImageInCustomMapping(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, transport := newAssetCaptureClient(t)
	state := map[string]any{"photo": image, "note": "see photo"}
	if _, err := systemOneWithAssets(t, client, state, Questions{"q": Noul{Instructions: "Is it damaged?"}}, nil); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	body := requestBody(t, contentType, request.body)
	compareJSON(t, map[string]any{"photo": image.Reference(), "note": "see photo"}, body["state"])
	assets := body["assets"].(map[string]any)
	if len(assets) != 1 {
		t.Fatalf("assets = %v, want exactly the image's reference", keysOfAny(assets))
	}
	if entry, ok := assets[image.Reference()].(map[string]any); !ok || !bytes.Equal(entry["data"].([]byte), pngFixture) {
		t.Errorf("asset entry for the reference = %v", assets[image.Reference()])
	}
}

func TestAssetsInQuestionContent(t *testing.T) {
	pngImage, jpegImage := NewImageFromBytes(pngFixture), NewImageFromBytes(jpegFixture)
	questions := Questions{
		"n": Noul{Instructions: []any{"Describe", pngImage}, Criteria: &NoulCriteria{True: "like <jpeg>"}},
		"c": Choice{Criteria: ChoiceCriteria{"match": pngImage, "other": nil}},
		"s": Score{Criteria: ScoreCriteria{jpegImage, "other"}},
		"r": RawQuestion{"type": "noul", "instructions": jpegImage},
	}
	client, transport := newAssetCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, "Compare the images.", questions, map[string]*Image{"<png>": pngImage, "<jpeg>": jpegImage}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	body := requestBody(t, contentType, request.body)
	compareJSON(t, map[string]any{
		"n": map[string]any{"type": "noul", "instructions": []any{"Describe", "<png>"}, "criteria": map[string]any{"true": "like <jpeg>"}},
		"c": map[string]any{"type": "choice", "criteria": map[string]any{"match": "<png>", "other": nil}},
		"s": map[string]any{"type": "score", "criteria": []any{"<jpeg>", "other"}},
		"r": map[string]any{"type": "noul", "instructions": "<jpeg>"},
	}, body["questions"])
	assets := body["assets"].(map[string]any)
	if len(assets) != 2 {
		t.Fatalf("assets = %v, want one entry each for <png> and <jpeg>", keysOfAny(assets))
	}
	if !bytes.Equal(assets["<png>"].(map[string]any)["data"].([]byte), pngFixture) {
		t.Errorf("<png> data = %q", assets["<png>"].(map[string]any)["data"])
	}
	if !bytes.Equal(assets["<jpeg>"].(map[string]any)["data"].([]byte), jpegFixture) {
		t.Errorf("<jpeg> data = %q", assets["<jpeg>"].(map[string]any)["data"])
	}
}

func TestImagesInStateAndQuestions(t *testing.T) {
	returned, listing := NewImageFromBytes(pngFixture), NewImageFromBytes(jpegFixture)
	state := []any{
		map[string]any{"role": "user", "msg": []any{"Here is what came back:", returned}},
		[]any{"listing", listing},
	}
	questions := Questions{
		"same":      Noul{Instructions: []any{"Is it the item in this photo?", listing}},
		"condition": Choice{Criteria: ChoiceCriteria{"new": returned, "used": nil}},
		"damage":    Score{Criteria: ScoreCriteria{"none", map[string]any{"example": []any{returned}}}},
		"raw":       RawQuestion{"type": "noul", "instructions": returned, "criteria": map[string]any{"true": listing, "false": nil}},
	}
	client, transport := newAssetCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, state, questions, nil); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	contentType := request.request.Header.Get("Content-Type")
	body := requestBody(t, contentType, request.body)
	compareJSON(t, []any{
		map[string]any{"role": "user", "msg": []any{"Here is what came back:", returned.Reference()}},
		[]any{"listing", listing.Reference()},
	}, body["state"])
	compareJSON(t, map[string]any{
		"same":      map[string]any{"type": "noul", "instructions": []any{"Is it the item in this photo?", listing.Reference()}},
		"condition": map[string]any{"type": "choice", "criteria": map[string]any{"new": returned.Reference(), "used": nil}},
		"damage":    map[string]any{"type": "score", "criteria": []any{"none", map[string]any{"example": []any{returned.Reference()}}}},
		"raw":       map[string]any{"type": "noul", "instructions": returned.Reference(), "criteria": map[string]any{"true": listing.Reference(), "false": nil}},
	}, body["questions"])
	assets := body["assets"].(map[string]any)
	if len(assets) != 2 {
		t.Fatalf("assets = %v, want one entry per reference", keysOfAny(assets))
	}
	if !bytes.Equal(assets[returned.Reference()].(map[string]any)["data"].([]byte), pngFixture) {
		t.Errorf("returned image data = %q", assets[returned.Reference()].(map[string]any)["data"])
	}
	if !bytes.Equal(assets[listing.Reference()].(map[string]any)["data"].([]byte), jpegFixture) {
		t.Errorf("listing image data = %q", assets[listing.Reference()].(map[string]any)["data"])
	}
}

func TestContentWithoutImagesUnchanged(t *testing.T) {
	for _, state := range []JSONContent{"text", map[string]any{"a": []any{1.0, "x", nil, true}}} {
		client, transport := newAssetCaptureClient(t)
		if _, err := systemOneWithAssets(t, client, state, Questions{"q": Noul{Instructions: map[string]any{"text": []any{"a", "b"}}}}, nil); err != nil {
			t.Fatalf("SystemOne: %v", err)
		}
		request := transport.requests[0]
		body := requestBody(t, "application/json", request.body)
		if _, present := body["assets"]; present {
			t.Errorf("assets key must be absent, got %v", body["assets"])
		}
		compareJSON(t, map[string]any{"type": "noul", "instructions": map[string]any{"text": []any{"a", "b"}}}, body["questions"].(map[string]any)["q"])
	}
}

// mustImageFromBase64 builds an image from valid base64 text or fails.
func mustImageFromBase64(t *testing.T, text string) *Image {
	t.Helper()
	image, err := NewImageFromBase64(text)
	if err != nil {
		t.Fatalf("NewImageFromBase64: %v", err)
	}
	return image
}

func keysOf(parts map[string]multipartPart) []string {
	keys := make([]string, 0, len(parts))
	for key := range parts {
		keys = append(keys, key)
	}
	return keys
}

func keysOfAny(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
