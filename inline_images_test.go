package typesafe

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

var inlineQuestions = Questions{"q": Noul{Instructions: "Is it damaged?"}}

func assetData(t *testing.T, body map[string]any, alias string) []byte {
	t.Helper()
	entry, ok := body["assets"].(map[string]any)[alias].(map[string]any)
	if !ok {
		t.Fatalf("no asset entry %q in %v", alias, body["assets"])
	}
	return entry["data"].([]byte)
}

func assetKeys(body map[string]any) []string {
	assets, ok := body["assets"].(map[string]any)
	if !ok {
		return nil
	}
	return keysOfAny(assets)
}

func sendInline(t *testing.T, client *Client, state JSONContent, questions Questions) {
	t.Helper()
	if _, err := client.SystemOne(t.Context(), &SystemOneParams{State: state, Questions: questions}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
}

func TestAliasedAndInlineCombine(t *testing.T) {
	inline := NewImageFromBytes(jpegFixture)
	client, captured := newBodyCaptureClient(t)
	if _, err := systemOneWithAssets(t, client, []any{"Compare with <listing>.", inline}, inlineQuestions, map[string]*Image{"<listing>": NewImageFromBytes(pngFixture)}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := (*captured)[0]
	// Go's deterministic enumeration: sorted Assets aliases number their
	// parts before scan-found references (the accepted sorted-keys deviation
	// from Python's insertion order; the encoder also sorts the JSON keys).
	parts := multipartParts(t, request.contentType, request.body)
	raw := decodeJSONMap(t, parts[requestPartName].content)
	compareJSON(t, map[string]any{
		"<listing>":        map[string]any{"type": "image", "part": "asset-0"},
		inline.Reference(): map[string]any{"type": "image", "part": "asset-1"},
	}, raw["assets"])
	if !bytes.Equal(assetData(t, request.decoded, "<listing>"), pngFixture) {
		t.Error("<listing> bytes wrong")
	}
	if !bytes.Equal(assetData(t, request.decoded, inline.Reference()), jpegFixture) {
		t.Error("inline reference bytes wrong")
	}
}

func TestPlacedMemberAliasAndLiteralReference(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	state := []any{"Compare <photo> with", image}
	if _, err := systemOneWithAssets(t, client, state, inlineQuestions, map[string]*Image{"<photo>": image}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := (*captured)[0]
	compareJSON(t, []any{"Compare <photo> with", "<photo>"}, request.decoded["state"])
	assets := request.decoded["assets"].(map[string]any)
	if len(assets) != 1 {
		t.Fatalf("assets = %v, want only the alias — a placed member maps to a single entry", keysOfAny(assets))
	}
	if _, ok := assets["<photo>"]; !ok {
		t.Fatalf("alias entry missing: %v", keysOfAny(assets))
	}
}

func TestFormattedImageFoundInContent(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	// An image placed in the content needs no attachment for other strings to
	// reference it.
	state := []any{image, fmt.Sprintf("Compare %s with the listing.", image)}
	if _, err := systemOneWithAssets(t, client, state, inlineQuestions, nil); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := (*captured)[0]
	assets := request.decoded["assets"].(map[string]any)
	if len(assets) != 1 {
		t.Fatalf("assets = %v, want only the image's reference", keysOfAny(assets))
	}
	if !bytes.Equal(assetData(t, request.decoded, image.Reference()), pngFixture) {
		t.Error("found image bytes wrong")
	}
}

func TestUnknownReferencesAreText(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	// Only known references are matched: text that merely looks like one is
	// left alone.
	state := fmt.Sprintf("Look at %s and <image:%s>", image, strings.Repeat("0", 32))
	if _, err := systemOneWithAssets(t, client, state, inlineQuestions, map[string]*Image{"<photo>": NewImageFromBytes(pngFixture)}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := (*captured)[0]
	if request.decoded["state"] != state {
		t.Errorf("state = %v, want the unknown reference left as text", request.decoded["state"])
	}
	if keys := assetKeys(request.decoded); len(keys) != 1 || keys[0] != "<photo>" {
		t.Errorf("assets = %v, want only <photo>", keys)
	}
}

func TestReferenceShapeMatching(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	tests := []struct {
		name      string
		lookalike string
	}{
		{"31 hex digits", "<image:" + strings.Repeat("0", 31) + ">"},
		{"33 hex digits", "<image:" + strings.Repeat("0", 33) + ">"},
		{"uppercase hex", strings.ToUpper(image.Reference())},
		{"truncated", image.Reference()[:20] + ">"},
		{"no brackets", strings.Trim(image.Reference(), "<>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, captured := newBodyCaptureClient(t)
			sendInline(t, client, "Look at "+tt.lookalike, inlineQuestions)
			request := (*captured)[0]
			if request.decoded["state"] != "Look at "+tt.lookalike {
				t.Errorf("state = %v", request.decoded["state"])
			}
			if _, present := request.decoded["assets"]; present {
				t.Errorf("lookalike %q must never match: %v", tt.lookalike, request.decoded["assets"])
			}
		})
	}
}

func TestInlineImageErrorsNameReference(t *testing.T) {
	image := NewImageFromFile("missing.png")
	client, transport := newAssetCaptureClient(t)
	_, err := systemOneWithAssets(t, client, map[string]any{"photos": []any{image}}, inlineQuestions, nil)
	want := fmt.Sprintf("Asset '%s': Image file 'missing.png' could not be read", image.Reference())
	var sdkErr *TypeSafeError
	if !errors.As(err, &sdkErr) || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("error = %v, want prefix %q", err, want)
	}
	if transport.requestCount() != 0 {
		t.Fatal("invalid image reached the network")
	}
}

func TestExtraBodyOverridesAssets(t *testing.T) {
	image := NewImageFromFile("missing.png")
	client, transport := newAssetCaptureClient(t)
	// Overridden assets are not read, so the missing file raises no error.
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     image,
		Questions: inlineQuestions,
		ExtraBody: map[string]JSONValue{"assets": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := transport.requests[0]
	if got := request.request.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want plain JSON", got)
	}
	body := requestBody(t, "application/json", request.body)
	compareJSON(t, map[string]any{}, body["assets"])
	if body["state"] != image.Reference() {
		t.Errorf("state = %v, want the image's reference", body["state"])
	}
}

func TestOverrideSkipsAliasValidation(t *testing.T) {
	client, transport := newAssetCaptureClient(t)
	// Asset validation lives only in the resolve step, which an ExtraBody
	// assets override skips entirely.
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "text",
		Questions: inlineQuestions,
		Assets:    map[string]*Image{"  ": NewImageFromBytes(pngFixture)},
		ExtraBody: map[string]JSONValue{"assets": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("alias validation must be skipped under an override: %v", err)
	}
	request := transport.requests[0]
	if got := request.request.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want plain JSON", got)
	}
	compareJSON(t, map[string]any{}, requestBody(t, "application/json", request.body)["assets"])
}

func TestImagesInFormattedStrings(t *testing.T) {
	returned, listing := NewImageFromBytes(pngFixture), NewImageFromBytes(jpegFixture)
	client, captured := newBodyCaptureClient(t)
	detach, err := client.AttachAssets(returned, listing)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	state := fmt.Sprintf("The customer says %s arrived scratched.", returned)
	instructions := fmt.Sprintf("Is %s the item in %s?", returned, listing)
	if _, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     state,
		Questions: Questions{"same": Noul{Instructions: instructions}},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	detach()
	request := (*captured)[0]
	if request.decoded["state"] != state {
		t.Errorf("state = %v", request.decoded["state"])
	}
	if request.decoded["questions"].(map[string]any)["same"].(map[string]any)["instructions"] != instructions {
		t.Errorf("instructions = %v", request.decoded["questions"])
	}
	if keys := assetKeys(request.decoded); len(keys) != 2 {
		t.Fatalf("assets = %v, want both attached references", keys)
	}
	if !bytes.Equal(assetData(t, request.decoded, listing.Reference()), jpegFixture) {
		t.Error("listing bytes wrong")
	}
}

func TestOnlyReferencedAttachedSent(t *testing.T) {
	first, second := NewImageFromBytes(pngFixture), NewImageFromBytes(jpegFixture)
	client, captured := newBodyCaptureClient(t)
	detach, err := client.AttachAssets(first, second)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	defer detach()
	sendInline(t, client, fmt.Sprintf("Look at %s", first), inlineQuestions)
	if keys := assetKeys((*captured)[0].decoded); len(keys) != 1 || keys[0] != first.Reference() {
		t.Fatalf("assets = %v, want only the referenced attachment", keys)
	}
	sendInline(t, client, "No images here.", inlineQuestions)
	if _, present := (*captured)[1].decoded["assets"]; present {
		t.Errorf("no assets key expected: %v", (*captured)[1].decoded["assets"])
	}
}

func TestAttachmentsNestAndDetach(t *testing.T) {
	outer, inner := NewImageFromBytes(pngFixture), NewImageFromBytes(jpegFixture)
	client, captured := newBodyCaptureClient(t)
	state := func() string { return fmt.Sprintf("%s %s", outer, inner) }
	send := func() { sendInline(t, client, state(), inlineQuestions) }
	detachOuter, err := client.AttachAssets(outer)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	detachInner, err := client.AttachAssets(inner)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	send()
	detachInner()
	send()
	detachOuter()
	send()
	var orders [][]string
	for _, request := range *captured {
		orders = append(orders, assetKeys(request.decoded))
	}
	if len(orders[0]) != 2 || len(orders[1]) != 1 || orders[2] != nil {
		t.Fatalf("attachment visibility = %v, want [both, outer-only, none]", orders)
	}
	if keys := assetKeys((*captured)[1].decoded); keys[0] != outer.Reference() {
		t.Error("outer asset must stay attached after the inner detach")
	}
}

func TestOverlappingHolds(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	first, err := client.AttachAssets(image)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	second, err := client.AttachAssets(image)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	send := func() { sendInline(t, client, image.Reference(), inlineQuestions) }
	first()
	send()
	if keys := assetKeys((*captured)[0].decoded); len(keys) != 1 {
		t.Fatalf("assets = %v; the asset must stay attached until the last holder detaches", keys)
	}
	second()
	send()
	if keys := assetKeys((*captured)[1].decoded); keys != nil {
		t.Fatalf("assets = %v; the asset must be detached after the last holder", keys)
	}
	client.attached.mu.Lock()
	remaining := len(client.attached.assets)
	client.attached.mu.Unlock()
	if remaining != 0 {
		t.Errorf("released assets must no longer be referenced by the client: %d left", remaining)
	}
}

func TestDetachIsSingleShot(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	first, err := client.AttachAssets(image)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	second, err := client.AttachAssets(image)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	send := func() { sendInline(t, client, image.Reference(), inlineQuestions) }
	first()
	first() // Single-shot: the second call must not release the other hold.
	send()
	if keys := assetKeys((*captured)[0].decoded); len(keys) != 1 || keys[0] != image.Reference() {
		t.Fatalf("assets = %v; the remaining hold must keep the asset attached", keys)
	}
	second()
	send()
	if keys := assetKeys((*captured)[1].decoded); keys != nil {
		t.Fatalf("assets = %v; the asset must detach once its last hold ends", keys)
	}
}

func TestAttachmentsScopedToClient(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	attaching, _ := newBodyCaptureClient(t)
	other, otherCaptured := newBodyCaptureClient(t)
	detach, err := attaching.AttachAssets(image)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	defer detach()
	sendInline(t, other, image.String(), inlineQuestions)
	if keys := assetKeys((*otherCaptured)[0].decoded); keys != nil {
		t.Errorf("attachments must not leak to another client: %v", keys)
	}
}

func TestAttachmentsReachGoroutines(t *testing.T) {
	images := make([]*Image, 8)
	for i := range images {
		images[i] = NewImageFromBytes(pngFixture)
	}
	client, transport := newAssetCaptureClient(t)
	detach, err := client.AttachAssets(images...)
	if err != nil {
		t.Fatalf("AttachAssets: %v", err)
	}
	var wg sync.WaitGroup
	for _, image := range images {
		wg.Add(1)
		go func(image *Image) {
			defer wg.Done()
			sendInline(t, client, image.Reference(), inlineQuestions)
		}(image)
	}
	wg.Wait()
	detach()
	if count := transport.requestCount(); count != len(images) {
		t.Fatalf("requests = %d, want %d", count, len(images))
	}
	seen := map[string]bool{}
	transport.mu.Lock()
	for _, request := range transport.requests {
		for _, alias := range assetKeys(requestBody(t, request.request.Header.Get("Content-Type"), request.body)) {
			seen[alias] = true
		}
	}
	transport.mu.Unlock()
	if len(seen) != len(images) {
		t.Errorf("distinct attached assets seen = %d, want %d", len(seen), len(images))
	}
}

func TestAttachAssetsRejectsNil(t *testing.T) {
	client, _ := newAssetCaptureClient(t)
	want := "Attached value <nil> must be an asset."
	var sdkErr *TypeSafeError
	_, err := client.AttachAssets(nil)
	if !errors.As(err, &sdkErr) || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	_, err = client.AttachAssets(NewImageFromBytes(pngFixture), nil)
	if !errors.As(err, &sdkErr) || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestReferenceShapedAliasKey(t *testing.T) {
	image := NewImageFromBytes(pngFixture)
	client, captured := newBodyCaptureClient(t)
	alias := "<image:" + strings.Repeat("0", 32) + ">"
	// The state text holds the same reference-shaped alias; the scan hit must
	// not overwrite the alias entry.
	if _, err := systemOneWithAssets(t, client, alias, inlineQuestions, map[string]*Image{alias: image}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	request := (*captured)[0]
	if keys := assetKeys(request.decoded); len(keys) != 1 || keys[0] != alias {
		t.Fatalf("assets = %v, want the single alias key", keys)
	}
	if !bytes.Equal(assetData(t, request.decoded, alias), pngFixture) {
		t.Error("alias bytes wrong")
	}
}
