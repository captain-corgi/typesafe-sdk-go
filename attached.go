package typesafe

import (
	"sync"
)

// attachedAssets holds a client's attached assets, keyed by reference and
// shared by its calls from any goroutine.
type attachedAssets struct {
	mu     sync.Mutex
	assets map[string]*Image
	// holds counts overlapping attachment scopes; an asset stays attached
	// until the last of them detaches.
	holds map[string]int
}

// attach registers pre-validated assets, one hold each.
func (a *attachedAssets) attach(assets []*Image) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.assets == nil {
		a.assets = make(map[string]*Image)
		a.holds = make(map[string]int)
	}
	for _, asset := range assets {
		a.assets[asset.reference] = asset
		a.holds[asset.reference]++
	}
}

// detach releases one hold per asset, removing each asset whose last hold
// ended.
func (a *attachedAssets) detach(assets []*Image) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, asset := range assets {
		reference := asset.reference
		a.holds[reference]--
		if a.holds[reference] <= 0 {
			delete(a.holds, reference)
			delete(a.assets, reference)
		}
	}
}

// snapshot copies the currently attached assets, keyed by reference, so
// concurrent calls see a stable view.
func (a *attachedAssets) snapshot() map[string]*Image {
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot := make(map[string]*Image, len(a.assets))
	for reference, asset := range a.assets {
		snapshot[reference] = asset
	}
	return snapshot
}

// AttachAssets attaches images to every call made on this client until the
// returned detach function runs, for referencing them inside strings: until
// then, calls send each attached image whose [Image.Reference] appears in the
// request. Attachment scopes nest and are refcounted, per-client, and safe
// for concurrent use; a second client never sees this client's attachments.
// It returns an error when any value is nil, before anything is attached.
//
// The returned function is single-shot: calling it twice is a no-op, because
// holds are counted per reference and an unguarded second call would release
// another holder's asset. The idiom is `defer detach()`.
func (c *Client) AttachAssets(assets ...*Image) (func(), error) {
	for _, asset := range assets {
		if asset == nil {
			return nil, newTypeSafeError("Attached value %v must be an asset.", asset)
		}
	}
	c.attached.attach(assets)
	var once sync.Once
	return func() {
		once.Do(func() { c.attached.detach(assets) })
	}, nil
}
