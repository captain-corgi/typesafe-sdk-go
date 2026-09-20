package typesafe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func fixedRand(value float64) func() float64 { return func() float64 { return value } }

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.MaxRetries != 2 || policy.BackoffInitial != 500*time.Millisecond || policy.BackoffMax != 5*time.Second || policy.BackoffJitter != 0.25 {
		t.Errorf("unexpected defaults: %+v", policy)
	}
	if !policy.RespectRetryAfter || !policy.APIConnectionError || !policy.APITimeoutError {
		t.Error("connection/timeout/retry-after defaults should be enabled")
	}
	if policy.Timeout != 30*time.Second {
		t.Errorf("budget = %v, want 30s", policy.Timeout)
	}
	for status := 500; status < 600; status++ {
		if _, ok := policy.HTTPStatuses[status]; !ok {
			t.Errorf("status %d should be retryable by default", status)
		}
	}
	if _, ok := policy.HTTPStatuses[408]; !ok {
		t.Error("408 should be retryable by default")
	}
	if _, ok := policy.HTTPStatuses[429]; !ok {
		t.Error("429 should be retryable by default")
	}
	for _, status := range []int{400, 401, 403, 404, 409, 422, 302, 599 + 1} {
		if _, ok := policy.HTTPStatuses[status]; ok {
			t.Errorf("status %d should not be retryable by default", status)
		}
	}
	if err := policy.Validate(); err != nil {
		t.Errorf("default policy should validate: %v", err)
	}
}

func TestRetryPolicyValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RetryPolicy)
		want   string
	}{
		{"negative max retries", func(p *RetryPolicy) { p.MaxRetries = -1 }, "max_retries must be a non-negative integer."},
		{"negative backoff initial", func(p *RetryPolicy) { p.BackoffInitial = -time.Second }, "backoff_initial must be a non-negative, finite number of seconds."},
		{"negative backoff max", func(p *RetryPolicy) { p.BackoffMax = -time.Second }, "backoff_max must be a non-negative, finite number of seconds."},
		{"jitter below zero", func(p *RetryPolicy) { p.BackoffJitter = -0.1 }, "backoff_jitter must be between zero and one."},
		{"jitter above one", func(p *RetryPolicy) { p.BackoffJitter = 1.1 }, "backoff_jitter must be between zero and one."},
		{"jitter NaN", func(p *RetryPolicy) { p.BackoffJitter = math.NaN() }, "backoff_jitter must be between zero and one."},
		{"jitter infinity", func(p *RetryPolicy) { p.BackoffJitter = math.Inf(1) }, "backoff_jitter must be between zero and one."},
		{"negative budget", func(p *RetryPolicy) { p.Timeout = -time.Second }, "timeout must be a positive, finite number of seconds."},
	}
	// The inclusive 0/1 jitter boundaries stay valid.
	for _, jitter := range []float64{0, 1} {
		policy := DefaultRetryPolicy()
		policy.BackoffJitter = jitter
		if err := policy.Validate(); err != nil {
			t.Errorf("jitter %v should validate: %v", jitter, err)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := DefaultRetryPolicy()
			tt.mutate(&policy)
			err := policy.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}

	t.Run("zero backoff and unlimited budget are valid", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.BackoffInitial = 0
		policy.BackoffMax = 0
		policy.Timeout = 0
		if err := policy.Validate(); err != nil {
			t.Errorf("zero backoff and budget should validate: %v", err)
		}
	})
}

func TestBackoffSequenceCapsAndJitter(t *testing.T) {
	// With rand pinned to 0 the sequence doubles up to the cap: 0.5 → 1 → 2 → 4 → 5 → 5…
	sequence := map[int]float64{1: 0.5, 2: 1, 3: 2, 4: 4, 5: 5, 20: 5}
	for attempt, want := range sequence {
		got := backoffSeconds(attempt, 0.5, 5.0, 0.25, fixedRand(0))
		if got != want {
			t.Errorf("backoffSeconds(%d) = %v, want %v", attempt, got, want)
		}
	}
	// Jitter floor at rand=1.0 with defaults: 0.5·(1−0.25) = 0.375.
	if got := backoffSeconds(1, 0.5, 5.0, 0.25, fixedRand(1)); got != 0.375 {
		t.Errorf("jitter floor = %v, want 0.375", got)
	}
	// Jitter never exceeds the un-jittered cap: 5·(1−0.25) = 3.75 ≤ 5.
	if got := backoffSeconds(5, 0.5, 5.0, 0.25, fixedRand(1)); got != 3.75 {
		t.Errorf("capped jitter = %v, want 3.75", got)
	}
	// The Duration wrapper keeps the same values.
	if got := backoffDelay(3, 500*time.Millisecond, 5*time.Second, 0.25, fixedRand(0)); got != 2*time.Second {
		t.Errorf("backoffDelay = %v, want 2s", got)
	}
}

func TestBackoffExtremeValues(t *testing.T) {
	tests := []struct {
		initial, maximum, want float64
		attempt                int
	}{
		{1e-300, 1e300, 0.0, 1},      // sub-millisecond values round to zero
		{1e-300, 1e300, 1e300, 2000}, // exponent guard picks the cap
		{1e308, 1e308, 1e308, 1},     // zero-step guard picks the cap immediately
		{0.5, 0.0006, 0.0006, 1},     // cap below initial wins on the first attempt
	}
	for _, tt := range tests {
		if got := backoffSeconds(tt.attempt, tt.initial, tt.maximum, 0.25, fixedRand(0)); got != tt.want {
			t.Errorf("backoffSeconds(%d, %v, %v) = %v, want %v", tt.attempt, tt.initial, tt.maximum, got, tt.want)
		}
	}
	// Zero backoff settings disable the delay entirely.
	for _, policy := range []RetryPolicy{
		{BackoffInitial: 0, BackoffMax: 5 * time.Second},
		{BackoffInitial: 500 * time.Millisecond, BackoffMax: 0},
	} {
		if got := backoffDelay(3, policy.BackoffInitial, policy.BackoffMax, 0.25, fixedRand(1)); got != 0 {
			t.Errorf("zero backoff settings should disable the delay, got %v", got)
		}
	}
}

func TestComputeDelayHonorsRetryAfter(t *testing.T) {
	rateLimited := apiErrorFor(429, `{}`, map[string]any{}, header("Retry-After", "5"), "")
	policy := DefaultRetryPolicy()
	if got := policy.computeDelay(1, rateLimited, fixedRand(0)); got != 5*time.Second {
		t.Errorf("retry-after should win: %v", got)
	}
	policy.RespectRetryAfter = false
	if got := policy.computeDelay(1, rateLimited, fixedRand(0)); got != 500*time.Millisecond {
		t.Errorf("backoff should apply when respect_retry_after is false: %v", got)
	}
	policy.BackoffInitial = 200 * time.Millisecond
	if got := policy.computeDelay(1, rateLimited, fixedRand(0)); got != 200*time.Millisecond {
		t.Errorf("custom initial backoff: %v", got)
	}
	// A 429 without parseable retry-after headers falls back to backoff.
	plain := apiErrorFor(429, `{}`, map[string]any{}, header("Retry-After", "bad"), "")
	if got := DefaultRetryPolicy().computeDelay(1, plain, fixedRand(1)); got != 375*time.Millisecond {
		t.Errorf("unparseable headers should fall back to backoff: %v", got)
	}
	// Server delays longer than backoff_max are honored in full — there is no
	// cap on the Retry-After path (unlike the older TS SDK's 60s clamp).
	for name, tt := range map[string]struct {
		headers http.Header
		want    time.Duration
	}{
		"seconds past the old clamp":  {header("Retry-After", "61"), 61 * time.Second},
		"milliseconds past the clamp": {header("Retry-After-Ms", "60001"), 60001 * time.Millisecond},
	} {
		err := apiErrorFor(429, `{}`, map[string]any{}, tt.headers, "")
		if got := DefaultRetryPolicy().computeDelay(1, err, fixedRand(0)); got != tt.want {
			t.Errorf("%s: delay = %v, want %v (uncapped)", name, got, tt.want)
		}
	}
}

func TestRetryableDispatch(t *testing.T) {
	timeoutErr := newTimeoutError(time.Second, errors.New("x"))
	connectionErr := newConnectionError(errors.New("y"))
	notFound := apiErrorFor(404, `{}`, map[string]any{}, http.Header{}, "")
	tooMany := apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, "")

	t.Run("built-in rules", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		if !policy.retryable(timeoutErr) || !policy.retryable(connectionErr) || !policy.retryable(tooMany) {
			t.Error("timeouts, connection errors, and retryable statuses should retry")
		}
		if policy.retryable(notFound) {
			t.Error("404 should not retry by default")
		}
		policy.APITimeoutError = false
		policy.APIConnectionError = false
		if policy.retryable(timeoutErr) || policy.retryable(connectionErr) {
			t.Error("flags should disable their retries")
		}
	})

	t.Run("custom statuses", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.HTTPStatuses = map[int]struct{}{409: {}}
		if !policy.retryable(apiErrorFor(409, `{}`, map[string]any{}, http.Header{}, "")) {
			t.Error("409 should retry with a custom status set")
		}
		if policy.retryable(tooMany) {
			t.Error("429 should not retry under a custom status set")
		}
	})

	t.Run("retry-on types", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.RetryOn = []error{&NotFoundError{}}
		if !policy.retryable(notFound) {
			t.Error("RetryOn should opt a 404 into retries")
		}
	})

	t.Run("retry-on sentinels", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.RetryOn = []error{ErrClientClosed}
		if !policy.retryable(&TypeSafeError{msg: "closed", wrapped: ErrClientClosed}) {
			t.Error("RetryOn should match sentinel values via errors.Is")
		}
	})

	t.Run("retry-on sentinels match strictly by identity", func(t *testing.T) {
		// A sentinel entry never falls back to matching every error of the
		// same concrete type (two errors.New values share *errors.errorString,
		// and custom sentinel types are just as ambiguous).
		sentinel := errors.New("flush the connection pool")
		disabled := RetryPolicy{MaxRetries: 2}
		disabled.RetryOn = []error{sentinel}

		if !disabled.retryable(newConnectionError(sentinel)) {
			t.Error("the exact sentinel should match")
		}
		if !disabled.retryable(&TypeSafeError{msg: "wrapped", wrapped: sentinel}) {
			t.Error("a wrapped sentinel should match via errors.Is")
		}
		if !disabled.retryable(newConnectionError(errors.Join(errors.New("outer"), sentinel))) {
			t.Error("a sentinel inside a joined error tree should match")
		}
		for name, other := range map[string]error{
			"different sentinel, same type":    errors.New("unrelated transport failure"),
			"same message, different identity": errors.New("flush the connection pool"),
		} {
			if disabled.retryable(newConnectionError(other)) {
				t.Errorf("%s should not match the sentinel", name)
			}
		}
	})

	t.Run("retry-on type selectors follow the error tree", func(t *testing.T) {
		// Type selectors (SDK error instances and typed-nil entries) match
		// via errors.As, so joined errors and custom As methods work.
		joined := newConnectionError(errors.Join(errors.New("outer"), apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, "")))
		byInstance := RetryPolicy{MaxRetries: 2}
		byInstance.RetryOn = []error{&RateLimitError{}}
		if !byInstance.retryable(joined) {
			t.Error("an SDK type selector should match through errors.Join")
		}

		byTypedNil := RetryPolicy{MaxRetries: 2}
		byTypedNil.RetryOn = []error{(*RateLimitError)(nil)}
		if !byTypedNil.retryable(joined) {
			t.Error("a typed-nil type selector should match through errors.Join")
		}

		wrapped := errors.Join(errors.New("outer"),
			errors.Join(errors.New("inner"), &NotFoundError{APIError: &APIError{Status: 404}}))
		byNestedNil := RetryPolicy{MaxRetries: 2}
		byNestedNil.RetryOn = []error{(*NotFoundError)(nil)}
		if !byNestedNil.retryable(wrapped) {
			t.Error("a type selector should match nested joined trees")
		}

		if byTypedNil.retryable(apiErrorFor(404, `{}`, map[string]any{}, http.Header{}, "")) {
			t.Error("a negative type match must not retry")
		}
	})

	t.Run("malformed SDK subclass with nil base does not panic", func(t *testing.T) {
		// A user-built tree can contain an SDK subclass whose embedded
		// *APIError is nil; the dispatch must treat it as statusless rather
		// than dereferencing the typed nil errors.As hands back.
		malformed := errors.Join(errors.New("outer"), &NotFoundError{})
		policy := DefaultRetryPolicy()
		if policy.retryable(malformed) {
			t.Error("a nil-base subclass carries no status and must not match the built-in rules")
		}
		byType := RetryPolicy{MaxRetries: 2}
		byType.RetryOn = []error{(*NotFoundError)(nil)}
		if !byType.retryable(malformed) {
			t.Error("the type selector should still match the malformed subclass")
		}
	})

	t.Run("retry-on custom selectors", func(t *testing.T) {
		// Arbitrary non-nil custom errors are sentinels (identity only); a
		// typed-nil entry selects the type instead.
		sentinel := &customRetryError{id: 7}
		policy := RetryPolicy{MaxRetries: 2}
		policy.RetryOn = []error{sentinel}
		if !policy.retryable(fmt.Errorf("wrapped: %w", sentinel)) {
			t.Error("the exact custom sentinel should match")
		}
		if policy.retryable(&customRetryError{id: 8}) {
			t.Error("a different instance of the same custom type must not match a sentinel entry")
		}

		byType := RetryPolicy{MaxRetries: 2}
		byType.RetryOn = []error{(*customRetryError)(nil)}
		if !byType.retryable(errors.Join(errors.New("outer"), &customRetryError{id: 9})) {
			t.Error("a typed-nil custom selector should match by type through the tree")
		}
	})

	t.Run("retry-on honors custom Is and As", func(t *testing.T) {
		bySentinelIs := RetryPolicy{MaxRetries: 2}
		bySentinelIs.RetryOn = []error{errFatal}
		if !bySentinelIs.retryable(isFatalError{fatal: true}) {
			t.Error("an error with a custom Is method should match through errors.Is")
		}
		if bySentinelIs.retryable(isFatalError{}) {
			t.Error("the custom Is method governs which selectors match")
		}

		byType := RetryPolicy{MaxRetries: 2}
		byType.RetryOn = []error{(*NotFoundError)(nil)}
		adapted := errors.Join(errors.New("outer"), asAdaptingError{})
		if !byType.retryable(adapted) {
			t.Error("a type selector should honor a custom As method via errors.As")
		}
	})

	t.Run("retry-on shared root selects every SDK error", func(t *testing.T) {
		// &TypeSafeError{} is a type selector for the SDK's error root, so it
		// opts API errors, connection errors, and timeouts into retries at
		// once — but never an error from outside the SDK.
		policy := RetryPolicy{MaxRetries: 2}
		policy.RetryOn = []error{&TypeSafeError{}}
		for name, err := range map[string]error{
			"api error":   apiErrorFor(404, `{}`, map[string]any{}, http.Header{}, ""),
			"connection":  newConnectionError(errors.New("x")),
			"timeout":     newTimeoutError(time.Second, errors.New("x")),
			"sdk-side":    newTypeSafeError("bad params"),
			"wrapped sdk": fmt.Errorf("app layer: %w", apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, "")),
			"joined sdk":  errors.Join(errors.New("outer"), newConnectionError(errors.New("y"))),
		} {
			if !policy.retryable(err) {
				t.Errorf("%s should match the shared SDK root", name)
			}
		}
		for name, err := range map[string]error{
			"plain error": errors.New("outside the SDK"),
			"canceled":    context.Canceled,
		} {
			if policy.retryable(err) {
				t.Errorf("%s must not acquire the SDK root", name)
			}
		}
	})

	t.Run("predicate", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.Predicate = func(err error) bool {
			var notFoundErr *NotFoundError
			return errors.As(err, &notFoundErr)
		}
		if !policy.retryable(notFound) {
			t.Error("predicate should opt a 404 into retries")
		}
		if policy.retryable(&TypeSafeError{msg: "unrelated"}) {
			t.Error("an error matching no rule should not retry")
		}
	})

	t.Run("disabled built-ins still consult retry-on and predicate", func(t *testing.T) {
		// The Python policy composes "builtin or exceptions or predicate", so a
		// timeout whose flag is off can still retry via RetryOn or Predicate.
		byType := DefaultRetryPolicy()
		byType.APITimeoutError = false
		byType.RetryOn = []error{&TimeoutError{}}
		if !byType.retryable(timeoutErr) {
			t.Error("RetryOn should retry a timeout even when APITimeoutError is off")
		}

		byPredicate := DefaultRetryPolicy()
		byPredicate.APITimeoutError = false
		byPredicate.Predicate = func(err error) bool {
			var timeoutErr *TimeoutError
			return errors.As(err, &timeoutErr)
		}
		if !byPredicate.retryable(timeoutErr) {
			t.Error("predicate should retry a timeout even when APITimeoutError is off")
		}

		byList := DefaultRetryPolicy()
		byList.APIConnectionError = false
		byList.RetryOn = []error{&ConnectionError{}}
		if !byList.retryable(connectionErr) {
			t.Error("RetryOn should retry a connection error even when APIConnectionError is off")
		}

		stillOff := DefaultRetryPolicy()
		stillOff.APITimeoutError = false
		stillOff.APIConnectionError = false
		if stillOff.retryable(timeoutErr) || stillOff.retryable(connectionErr) {
			t.Error("flags should disable their retries when no other rule matches")
		}
	})
}

func TestPolicyCopyIsIndependent(t *testing.T) {
	policy := DefaultRetryPolicy()
	copied := policy.copy()
	copied.HTTPStatuses[999] = struct{}{}
	if _, shared := policy.HTTPStatuses[999]; shared {
		t.Error("copied policy shares the status set with the original")
	}
	copied.RetryOn = append(copied.RetryOn, errors.New("x"))
	if len(policy.RetryOn) != 0 {
		t.Error("copied policy shares the RetryOn slice with the original")
	}
}

// retryingRoundTripper answers every request with a 429 carrying a
// Retry-After header, after spending a fixed duration on the attempt. Under a
// [synctest.Test] fake clock that sleep costs no real time but does count
// against the retry budget, which is what the budget tests measure.
func retryingRoundTripper(duration time.Duration, delaySeconds string, requests *int) roundTripperFunc {
	return func(req *http.Request) (*http.Response, error) {
		time.Sleep(duration)
		*requests++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Retry-After":           []string{delaySeconds},
				"X-Typesafe-Request-Id": []string{"request-" + strconv.Itoa(*requests)},
			},
			Body:    io.NopCloser(strings.NewReader(`{"message":"attempt ` + strconv.Itoa(*requests) + `"}`)),
			Request: req,
		}, nil
	}
}

func TestRetryBudgetStopsBeforeDelay(t *testing.T) {
	// (budget, per-attempt duration, retry-after delay, expected attempts)
	tests := []struct {
		budget   time.Duration
		duration time.Duration
		delay    string
		attempts int
	}{
		{0, 1 * time.Second, "0.5", 3}, // unlimited budget: max retries binds
		{30 * time.Second, 10 * time.Second, "5", 2},
		{2500 * time.Millisecond, 750 * time.Millisecond, "0.5", 2},
		{2 * time.Second, 1 * time.Second, "0", 2},
		{1 * time.Second, 0, "1", 1},
		{1 * time.Second, 0, "60", 1},
	}
	for _, tt := range tests {
		t.Run("budget="+tt.budget.String(), func(t *testing.T) {
			for round := range 2 {
				// Each SDK call gets a fresh budget.
				synctest.Test(t, func(t *testing.T) {
					requests := 0
					// A per-attempt timeout far above every simulated attempt
					// duration, so only the budget can end the run.
					dc := newDeterministicClient(t, nil, WithTimeout(time.Hour))
					policy := DefaultRetryPolicy()
					policy.Timeout = tt.budget
					dc.setTransport(retryingRoundTripper(tt.duration, tt.delay, &requests))

					_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
					var rateLimit *RateLimitError
					if !errors.As(err, &rateLimit) {
						t.Fatalf("round %d: expected RateLimitError, got %v", round, err)
					}
					if want := "attempt " + strconv.Itoa(tt.attempts); rateLimit.message != want {
						t.Errorf("round %d: final message = %q, want %q", round, rateLimit.message, want)
					}
					if requests != tt.attempts {
						t.Errorf("round %d: attempts = %d, want %d", round, requests, tt.attempts)
					}
					// The server-requested delay is honored exactly, however
					// long. The attempt's own duration falls outside the gap,
					// which spans one attempt returning to the next starting.
					wantDelay, _ := strconv.ParseFloat(tt.delay, 64)
					for _, got := range dc.recordedDelays() {
						if got != time.Duration(wantDelay*float64(time.Second)) {
							t.Errorf("round %d: slept %v, want exactly %s (from Retry-After)", round, got, tt.delay)
						}
					}
					if got := len(dc.recordedDelays()); got != tt.attempts-1 {
						t.Errorf("round %d: sleeps = %d, want %d", round, got, tt.attempts-1)
					}
				})
			}
		})
	}
}

func TestRetryBudgetOverride(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		requests := 0
		// A per-attempt timeout above the 20s simulated attempt cost, so only
		// the budget can end a run.
		dc := newDeterministicClient(t, nil, WithTimeout(time.Hour))
		inner := retryingRoundTripper(20*time.Second, "", &requests)
		// The wrapper converts the Retry-After header to retry-after-ms: 0.
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			resp, err := inner(req)
			if resp != nil {
				resp.Header.Del("Retry-After")
				resp.Header.Set("Retry-After-Ms", "0")
			}
			return resp, err
		})

		// Default 30s budget: each attempt costs 20s, so the second retry's cost
		// (elapsed 40s) reaches the budget and the loop stops after two attempts.
		requests = 0
		_, err := dc.Models.List(t.Context(), nil)
		if err == nil || requests != 2 {
			t.Errorf("default budget: attempts = %d (err %v), want 2", requests, err)
		}

		// Tight 1s budget: the first attempt already exceeds it.
		requests = 0
		tight := DefaultRetryPolicy()
		tight.Timeout = time.Second
		_, err = dc.Models.List(t.Context(), &ModelsListParams{Retry: &tight})
		if err == nil || requests != 1 {
			t.Errorf("tight budget: attempts = %d (err %v), want 1", requests, err)
		}

		// Unlimited budget: max retries binds at three attempts.
		requests = 0
		unlimited := DefaultRetryPolicy()
		unlimited.Timeout = 0
		_, err = dc.Models.List(t.Context(), &ModelsListParams{Retry: &unlimited})
		if err == nil || requests != 3 {
			t.Errorf("unlimited budget: attempts = %d (err %v), want 3", requests, err)
		}

		// Back to the client default.
		requests = 0
		_, err = dc.Models.List(t.Context(), nil)
		if err == nil || requests != 2 {
			t.Errorf("client default budget: attempts = %d (err %v), want 2", requests, err)
		}
	})
}

// TestBudgetOverflowSafeDelay: a server Retry-After so large that the delay
// clamps to the maximum duration must still stop at the budget instead of
// overflowing the elapsed+delay addition into a forever sleep.
func TestBudgetOverflowSafeDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			// A tiny delay first so the clock advances past zero, then a delay
			// large enough to clamp to the maximum duration: under the old
			// elapsed+delay arithmetic this wrapped negative and slept forever
			// instead of stopping at the budget.
			delay := "1"
			if attempts > 1 {
				delay = "9999999999999999"
			}
			return textResponseWithHeaders(req, http.StatusTooManyRequests, `{"message": "slow"}`,
				map[string]string{"Retry-After-Ms": delay}), nil
		})
		_, err := dc.Models.List(t.Context(), nil) // default 30s budget
		if err == nil {
			t.Fatal("expected the rate-limit error")
		}
		if attempts != 2 {
			t.Errorf("attempts = %d, want 2 (the budget must stop before the huge sleep)", attempts)
		}
		delays := dc.recordedDelays()
		if len(delays) != 1 || delays[0] != 1*time.Millisecond {
			t.Errorf("sleeps = %v, want only the initial 1ms", delays)
		}
		var rateLimit *RateLimitError
		if !errors.As(err, &rateLimit) {
			t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
		}
	})
}

// TestRetryConditionEvaluatedOnEveryFailedAttempt: the retry condition —
// RetryOn selectors and Predicate — runs once per failed attempt, including
// the final attempt that exceeds MaxRetries and is never retried. Python's
// tenacity evaluates the retry condition on every failure before consulting
// the stop strategy, so a counting or logging predicate observes all of them.
func TestRetryConditionEvaluatedOnEveryFailedAttempt(t *testing.T) {
	notFoundServer := func(t *testing.T) (*deterministicClient, *int) {
		t.Helper()
		requests := 0
		dc := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
			requests++
			return textResponse(req, http.StatusNotFound,
				`{"message": "attempt `+strconv.Itoa(requests)+`"}`), nil
		})
		return dc, &requests
	}

	t.Run("predicate sees the final failed attempt", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			dc, requests := notFoundServer(t)
			predicateCalls := 0
			policy := DefaultRetryPolicy()
			policy.MaxRetries = 1
			policy.Timeout = 0
			policy.Predicate = func(error) bool {
				predicateCalls++
				return true
			}
			_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
			if err == nil {
				t.Fatal("expected the final 404")
			}
			if *requests != 2 {
				t.Fatalf("attempts = %d, want 2", *requests)
			}
			if predicateCalls != 2 {
				t.Errorf("predicate calls = %d, want 2 (once per failed attempt, final included)", predicateCalls)
			}
			var notFoundErr *NotFoundError
			if !errors.As(err, &notFoundErr) || notFoundErr.message != "attempt 2" {
				t.Errorf("final error = %v, want the attempt-2 404", err)
			}
		})
	})

	t.Run("predicate runs once even with retries disabled", func(t *testing.T) {
		dc, requests := notFoundServer(t)
		predicateCalls := 0
		policy := DefaultRetryPolicy()
		policy.MaxRetries = 0
		policy.Predicate = func(error) bool {
			predicateCalls++
			return true
		}
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		if err == nil {
			t.Fatal("expected the 404")
		}
		if *requests != 1 {
			t.Fatalf("attempts = %d, want 1", *requests)
		}
		if predicateCalls != 1 {
			t.Errorf("predicate calls = %d, want 1", predicateCalls)
		}
	})

	t.Run("retry-on selectors also see the final failed attempt", func(t *testing.T) {
		// The selector evaluation is counted through a custom Is method on
		// the transport cause: errors.Is invokes it once per RetryOn check.
		// It matches the sentinel on the first consultation (triggering the
		// retry) and declines the second, so the run makes two attempts and
		// the selector must still be consulted on the un-retried final one.
		requests := 0
		sentinelConsultations := 0
		sentinel := errors.New("flush the connection pool")
		dc := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
			requests++
			return nil, &countingIsError{sentinel: sentinel, matches: &sentinelConsultations}
		})
		policy := RetryPolicy{MaxRetries: 1, Timeout: 0} // built-ins all off
		policy.RetryOn = []error{sentinel}
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
		if requests != 2 {
			t.Fatalf("attempts = %d, want 2", requests)
		}
		if sentinelConsultations != 2 {
			t.Errorf("selector checks = %d, want 2 (once per failed attempt, final included)", sentinelConsultations)
		}
	})
}

// countingIsError counts how often errors.Is consults it about one specific
// sentinel, matching only the first such consultation. Consultations about
// other targets (the transport-timeout probe walks the same chain) are
// ignored so the count tracks RetryOn checks alone.
type countingIsError struct {
	sentinel error
	matches  *int
}

func (e *countingIsError) Error() string { return "counting is error" }

func (e *countingIsError) Is(target error) bool {
	if target != e.sentinel {
		return false
	}
	*e.matches++
	return *e.matches == 1
}

// --- RetryOn selector fixtures ------------------------------------------------

// customRetryError is an application error type for selector tests.
type customRetryError struct{ id int }

func (e *customRetryError) Error() string { return fmt.Sprintf("custom retry error %d", e.id) }

// isFatalError is an application error that reports itself as matching the
// errFatal sentinel through a custom Is method (errors.Is calls Is on the
// error being matched, with the selector as its argument).
var errFatal = errors.New("fatal")

type isFatalError struct{ fatal bool }

func (isFatalError) Error() string { return "app failure" }

func (e isFatalError) Is(target error) bool { return e.fatal && target == errFatal }

// asAdaptingError claims to be a *NotFoundError through a custom As method.
type asAdaptingError struct{}

func (asAdaptingError) Error() string { return "adapted" }

func (asAdaptingError) As(target any) bool {
	_, ok := target.(**NotFoundError)
	return ok
}

// TestRetryOnSelectionDrivesAttempts: with every built-in rule disabled, only
// the failure selected by RetryOn triggers another attempt — an unrelated
// transport failure stops at one attempt.
func TestRetryOnSelectionDrivesAttempts(t *testing.T) {
	selectedOnly := func() RetryPolicy {
		policy := RetryPolicy{MaxRetries: 2, Timeout: 0} // built-ins all off
		policy.RetryOn = []error{&NotFoundError{}}
		return policy
	}

	t.Run("selected HTTP failure retries", func(t *testing.T) {
		requests := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			return textResponse(req, http.StatusNotFound, `{"message": "gone"}`), nil
		})
		policy := selectedOnly()
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		if err == nil {
			t.Fatal("expected the final 404")
		}
		if requests != 3 {
			t.Errorf("attempts = %d, want 3 (the selected 404 retries)", requests)
		}
	})

	t.Run("unrelated transport failure stops at one attempt", func(t *testing.T) {
		requests := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("dial tcp: connection refused")
		})
		policy := selectedOnly()
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
		if requests != 1 {
			t.Errorf("attempts = %d, want 1 (connection errors are not selected)", requests)
		}
	})

	t.Run("joined transport cause carrying the selected type retries", func(t *testing.T) {
		requests := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			return nil, errors.Join(
				errors.New("net hiccup"),
				apiErrorFor(http.StatusNotFound, `{}`, map[string]any{}, http.Header{}, ""))
		})
		policy := selectedOnly()
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
		if requests != 3 {
			t.Errorf("attempts = %d, want 3 (the selected type hides inside the joined cause)", requests)
		}
	})
}
