package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
	"time"
)

// clientSettings accumulates functional options during NewClient.
type clientSettings struct {
	apiKey              string
	baseURL             string
	defaultModel        string
	timeout             time.Duration
	maxResponseBodySize int64
	allowInsecureHTTP   bool
	retry               *RetryPolicy
	headers             map[string]string
	httpClient          *http.Client
	httpClientSet       bool
}

// Option customizes a [Client] at construction; see the With* functions.
type Option func(*clientSettings) error

var reservedSystemOneBodyFields = map[string]struct{}{
	"model":     {},
	"questions": {},
	"state":     {},
}

// WithAPIKey sets the API key, overriding the TYPESAFE_API_KEY environment
// variable. An empty or whitespace-only value means "unset".
func WithAPIKey(apiKey string) Option {
	return func(s *clientSettings) error {
		s.apiKey = apiKey
		return nil
	}
}

// WithBaseURL sets the API root, overriding the TYPESAFE_BASE_URL environment
// variable; any trailing slashes are stripped.
func WithBaseURL(baseURL string) Option {
	return func(s *clientSettings) error {
		s.baseURL = baseURL
		return nil
	}
}

// WithModel sets the default model, overriding the TYPESAFE_DEFAULT_MODEL
// environment variable.
func WithModel(model string) Option {
	return func(s *clientSettings) error {
		s.defaultModel = model
		return nil
	}
}

// WithTimeout sets the per-attempt timeout, overriding the 10s default. Zero
// means "unset"; a negative value is rejected.
func WithTimeout(timeout time.Duration) Option {
	return func(s *clientSettings) error {
		if timeout < 0 {
			return newTypeSafeError("timeout must be a positive, finite number of seconds.")
		}
		if timeout > 0 {
			s.timeout = timeout
		}
		return nil
	}
}

// WithMaxResponseBodySize sets the maximum response body size buffered by
// this client. Zero uses DefaultMaxResponseBodySize.
func WithMaxResponseBodySize(size int64) Option {
	return func(s *clientSettings) error {
		if size < 0 {
			return newTypeSafeError("max response body size must be non-negative.")
		}
		s.maxResponseBodySize = size
		return nil
	}
}

// WithAllowInsecureHTTP permits http URLs only for loopback hosts. HTTPS
// remains the default and is required for remote hosts.
func WithAllowInsecureHTTP() Option {
	return func(s *clientSettings) error {
		s.allowInsecureHTTP = true
		return nil
	}
}

// WithRetry sets the client-level retry policy. The policy is validated at
// construction and snapshotted per call.
func WithRetry(policy RetryPolicy) Option {
	return func(s *clientSettings) error {
		if err := policy.Validate(); err != nil {
			return err
		}
		copied := policy.copy()
		s.retry = &copied
		return nil
	}
}

// WithHeaders sets additional default request headers; protected headers
// (Authorization, Accept, Content-Type, User-Agent, X-TypeSafe-*) can never
// be overridden this way.
func WithHeaders(headers map[string]string) Option {
	return func(s *clientSettings) error {
		s.headers = headers
		return nil
	}
}

// WithHTTPClient supplies the underlying [*http.Client]. When WithTimeout is
// not set, a positive Timeout on the supplied client is inherited as the
// per-attempt timeout (a zero Timeout — Go's default, meaning no client-level
// cap — is not inherited, and the 10s SDK default applies). The resolved
// timeout always governs the whole attempt through its context: the SDK
// sends on its own copy of the client with the outer Timeout disabled, so an
// explicit SDK timeout longer than the supplied client's Timeout is honored
// in full and the caller's client is never modified. The client's Transport,
// Jar, and CheckRedirect are shared, and its idle connections are closed by
// [Client.Close]. A nil client is rejected. Note that the SDK's default
// client does not follow redirects (3xx responses surface as errors, like
// the other TypeSafe SDKs); supply your own client here if you want
// different behavior.
func WithHTTPClient(client *http.Client) Option {
	return func(s *clientSettings) error {
		if client == nil {
			return newTypeSafeError("WithHTTPClient requires a non-nil *http.Client.")
		}
		s.httpClient = client
		s.httpClientSet = true
		return nil
	}
}

// Client is an HTTP client for the TypeSafe AI API. Create one with
// [NewClient]; it is safe for concurrent use, and per-call state (model,
// timeout, retry policy, headers) is fully isolated between calls. When
// finished, call [Client.Close] to release idle connections; using a closed
// client returns an error wrapping [ErrClientClosed].
type Client struct {
	config     *config
	httpClient *http.Client
	retry      RetryPolicy
	Models     *Models
	attached   attachedAssets

	closed atomic.Bool

	// rand supplies the backoff jitter fraction. It is a seam so tests can
	// pin jitter; the elapsed-time and sleep paths need no seam because
	// timing tests run under a testing/synctest fake clock.
	rand func() float64
}

// NewClient creates a client for the TypeSafe AI API. Explicit options take
// precedence over environment variables (TYPESAFE_API_KEY, TYPESAFE_BASE_URL,
// TYPESAFE_DEFAULT_MODEL), which take precedence over defaults. Blank environment
// values, empty explicit strings, and whitespace-only explicit API keys inherit;
// other explicit strings are preserved. It returns an error wrapping
// [ErrMissingAPIKey] when no API key resolves, and option values are
// validated before the client is built.
func NewClient(opts ...Option) (*Client, error) {
	settings := &clientSettings{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(settings); err != nil {
			return nil, err
		}
	}

	defaultHeaders := http.Header{}
	for _, name := range sortedHeaderNames(settings.headers) {
		setHeader(defaultHeaders, name, settings.headers[name])
	}

	timeout := settings.timeout
	if timeout == 0 && settings.httpClientSet && settings.httpClient.Timeout > 0 {
		// Inherit the supplied client's timeout, like the Python SDK.
		timeout = settings.httpClient.Timeout
	}
	resolved, err := resolveConfig(settings.apiKey, settings.baseURL, settings.defaultModel, timeout, settings.maxResponseBodySize, settings.allowInsecureHTTP, defaultHeaders)
	if err != nil {
		return nil, err
	}

	retry := DefaultRetryPolicy()
	if settings.retry != nil {
		retry = *settings.retry
	}

	var httpClient *http.Client
	if !settings.httpClientSet {
		// 3xx responses surface as errors instead of being followed,
		// matching the other TypeSafe SDKs. The transport is a private
		// clone of the process default so Close only evicts connections
		// the SDK made — a nil Transport would delegate CloseIdleConnections
		// to the shared http.DefaultTransport pool.
		httpClient = &http.Client{
			Transport: func() http.RoundTripper {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				// Keep-alive: Go's default transport pools only 2 idle
				// connections per host for 90s; the Python SDK keeps 100 for
				// 30s. MaxConnsPerHost stays unset: it caps connections by
				// queueing requests before dialing, which would silently
				// change latency semantics.
				transport.MaxIdleConnsPerHost = keepAliveConnsPerHost
				transport.IdleConnTimeout = keepAliveIdleTimeout
				return transport
			}(),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	} else {
		// Send on an SDK-owned copy with the outer Timeout disabled: the
		// resolved per-attempt timeout (which inherits the supplied client's
		// Timeout only when WithTimeout is unset) is enforced through the
		// attempt context, so a longer SDK timeout is never silently capped
		// by the injected client's own deadline. The caller's client is
		// never mutated; Transport, Jar, and CheckRedirect stay shared, so
		// CloseIdleConnections still reaches the supplied transport.
		copied := *settings.httpClient
		copied.Timeout = 0
		httpClient = &copied
	}

	client := &Client{
		config:     resolved,
		httpClient: httpClient,
		retry:      retry,
		Models:     nil,
		rand:       rand.Float64,
	}
	client.Models = &Models{client: client}
	return client, nil
}

// sleepWithContext waits for the duration or until the context ends.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// checkClosed reports whether the client is still usable.
func (c *Client) checkClosed() error {
	if c.closed.Load() {
		return &TypeSafeError{msg: "The client is closed.", wrapped: ErrClientClosed}
	}
	return nil
}

// Close releases network resources: it closes idle connections of the
// underlying *http.Client, including one supplied via WithHTTPClient. The
// SDK-owned default client runs on a private transport, so its Close never
// evicts connections pooled on the process-global http.DefaultTransport.
// The client must not be used afterwards; calls on a closed client return an
// error wrapping [ErrClientClosed]. Close is idempotent.
func (c *Client) Close() {
	if c.closed.Swap(true) {
		return
	}
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}

// SystemOneParams describes one POST /v1/systemone call. The zero values of
// the optional fields inherit the client settings.
type SystemOneParams struct {
	// State is the content all questions refer to: text, a JSON object, or
	// an array.
	State JSONContent
	// Questions is the non-empty mapping of names to questions.
	Questions Questions
	// Model overrides the client default model for this call.
	Model string
	// Timeout overrides the client per-attempt timeout for this call.
	Timeout time.Duration
	// Retry replaces the client retry policy for this call.
	Retry *RetryPolicy
	// ExtraHeaders are additional request headers for this call.
	ExtraHeaders map[string]string
	// ExtraBody holds additional top-level request-body fields. SDK-owned
	// fields (state, model, and questions) cannot be overridden; other fields
	// are shallow-merged and object values are replaced rather than deep-merged.
	// An ExtraBody "assets" key replaces the SDK asset machinery entirely:
	// Assets and placed images are neither read nor validated, and the body
	// stays plain JSON with that value verbatim.
	ExtraBody map[string]JSONValue
	// Assets names images for referencing them from request content: each
	// entry is uploaded as one asset part under its alias — even when the
	// body never names it — and every placed occurrence of the image
	// serializes as that alias. A nil or empty map sends no assets. Aliases
	// and image data are validated only when assets are actually sent.
	Assets map[string]*Image
}

// SystemOne answers named questions about text, structured state, or images
// in a single request. Questions are validated before any network I/O; placed
// and referenced images are collected and uploaded as multipart asset parts;
// the returned response carries answers keyed by question name, the model
// used, token usage, the request ID, and the buffered raw HTTP response.
func (c *Client) SystemOne(ctx context.Context, params *SystemOneParams) (*SystemOneResponse, error) {
	if params == nil {
		params = &SystemOneParams{}
	}
	if params.State == nil {
		return nil, newTypeSafeError("State is required.")
	}
	questions, err := normalizeQuestions(params.Questions)
	if err != nil {
		return nil, err
	}
	for _, key := range sortedKeys(params.ExtraBody) {
		if _, reserved := reservedSystemOneBodyFields[key]; reserved {
			return nil, newTypeSafeError("ExtraBody cannot override the reserved field %q.", key)
		}
	}
	body := map[string]any{
		"state":     params.State,
		"model":     c.config.defaultModel,
		"questions": questions,
	}
	if params.Model != "" {
		body["model"] = params.Model
	}
	for _, key := range sortedKeys(params.ExtraBody) {
		body[key] = params.ExtraBody[key]
	}
	assetsCtx := newAssetContext(params.Assets)
	walkedAny, err := rewriteContent(body, assetsCtx)
	if err != nil {
		if errors.Is(err, errRequestBodyCycle) {
			return nil, newTypeSafeError("The request body could not be encoded as JSON")
		}
		return nil, err
	}
	walked := walkedAny.(map[string]any)
	content, err := encodeBody(walked)
	if err != nil {
		return nil, err
	}
	if params.Timeout < 0 {
		return nil, newTypeSafeError("timeout must be a positive, finite number of seconds.")
	}
	contentType := ""
	if _, overridden := body["assets"]; !overridden {
		pending := collectPendingAssets(content, params.Assets, c.attached.snapshot(), assetsCtx.found)
		if len(pending) > 0 {
			// The API rejects an empty assets map, so the key is sent only
			// when at least one asset is pending.
			entries := make(map[string]json.RawMessage, len(pending))
			parts := make([]part, 0, len(pending)+1)
			for index := range pending {
				name := fmt.Sprintf(assetPartFormat, index)
				// Hand-encoded so "type" precedes "part"; the encoder would
				// sort a map's keys the other way.
				entries[pending[index].name] = json.RawMessage(`{"type":"image","part":"` + name + `"}`)
			}
			walked["assets"] = entries
			requestContent, err := encodeBody(walked)
			if err != nil {
				return nil, err
			}
			resolved, err := resolvePendingAssets(pending)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part{name: requestPartName, contentType: jsonContentType, content: requestContent})
			for index, image := range resolved {
				name := fmt.Sprintf(assetPartFormat, index)
				parts = append(parts, part{name: name, contentType: image.contentType, content: image.data, filename: name})
			}
			contentType, content = encodeMultipart(parts)
		}
	}
	req, err := prepareRequest(c.config, http.MethodPost, systemOnePath, content, contentType, params.Timeout, params.ExtraHeaders)
	if err != nil {
		return nil, err
	}
	return execute(ctx, c, req, params.Retry, parseSystemOneResponse)
}

// collectPendingAssets builds the ordered pending-asset list: every Assets
// entry in sorted-key order — each is uploaded even when the body never names
// it — then each reference found in the encoded body that names a known image
// (attached assets, Assets values by reference, and images placed in the
// content), in order of first appearance. A reference equal to an existing
// alias never overwrites the alias's entry.
func collectPendingAssets(content []byte, assets map[string]*Image, attached, found map[string]*Image) []pendingAsset {
	known := make(map[string]*Image, len(attached)+len(assets)+len(found))
	for reference, asset := range attached {
		known[reference] = asset
	}
	for _, name := range sortedKeys(assets) {
		if asset := assets[name]; asset != nil {
			known[asset.reference] = asset
		}
	}
	for reference, asset := range found {
		known[reference] = asset
	}

	pending := make([]pendingAsset, 0, len(assets))
	seen := make(map[string]struct{}, len(assets))
	for _, name := range sortedKeys(assets) {
		pending = append(pending, pendingAsset{name: name, image: assets[name]})
		seen[name] = struct{}{}
	}
	if len(known) == 0 {
		return pending
	}
	// References are JSON-safe ASCII, so one scan of the encoded body finds
	// them inside every string.
	for _, match := range referenceRegex.FindAll(content, -1) {
		reference := string(match)
		if _, exists := seen[reference]; exists {
			continue
		}
		asset, ok := known[reference]
		if !ok {
			continue
		}
		pending = append(pending, pendingAsset{name: reference, image: asset})
		seen[reference] = struct{}{}
	}
	return pending
}

// ModelsListParams describes one GET /v1/models call. The zero values of the
// optional fields inherit the client settings.
type ModelsListParams struct {
	// Timeout overrides the client per-attempt timeout for this call.
	Timeout time.Duration
	// Retry replaces the client retry policy for this call.
	Retry *RetryPolicy
	// ExtraHeaders are additional request headers for this call.
	ExtraHeaders map[string]string
}

// Models is the Models API resource, reached through [Client.Models].
type Models struct {
	client *Client
}

// List returns the models available to the account.
func (m *Models) List(ctx context.Context, params *ModelsListParams) (*ListModelsResponse, error) {
	if params == nil {
		params = &ModelsListParams{}
	}
	if params.Timeout < 0 {
		return nil, newTypeSafeError("timeout must be a positive, finite number of seconds.")
	}
	req, err := prepareRequest(m.client.config, http.MethodGet, modelsPath, nil, "", params.Timeout, params.ExtraHeaders)
	if err != nil {
		return nil, err
	}
	return execute(ctx, m.client, req, params.Retry, parseListModelsResponse)
}
