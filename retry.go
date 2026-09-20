package typesafe

import (
	"errors"
	"math"
	"reflect"
	"time"
)

// RetryPolicy configures SDK retry behavior. The zero value is valid but
// retries nothing; start from [DefaultRetryPolicy] and adjust fields.
//
//	policy := typesafe.DefaultRetryPolicy()
//	policy.MaxRetries = 4
//	client, err := typesafe.NewClient(typesafe.WithRetry(policy))
type RetryPolicy struct {
	// MaxRetries is the number of retries after the initial attempt; 0
	// disables retries (total attempts = MaxRetries + 1).
	MaxRetries int

	// BackoffInitial is the first backoff delay, doubled each attempt up to
	// BackoffMax; 0 disables backoff (immediate re-attempt).
	BackoffInitial time.Duration

	// BackoffMax caps the un-jittered exponential backoff; 0 disables backoff.
	BackoffMax time.Duration

	// BackoffJitter is the fraction of each backoff delay randomly
	// subtracted, between 0 and 1.
	BackoffJitter float64

	// HTTPStatuses are the *APIError statuses that are retried.
	HTTPStatuses map[int]struct{}

	// RespectRetryAfter makes the server-requested delay (Retry-After /
	// retry-after-ms) win over backoff.
	RespectRetryAfter bool

	// APIConnectionError retries *ConnectionError.
	APIConnectionError bool

	// APITimeoutError retries *TimeoutError.
	APITimeoutError bool

	// RetryOn names additional errors that trigger a retry. An entry of an
	// SDK error type (such as [&NotFoundError{}]) or a typed-nil pointer
	// (such as (*MyError)(nil)) selects by type: any matching error in the
	// tree — including wrapped, joined, and custom-As errors — retries, via
	// [errors.As]. Any other entry is a sentinel matched only by identity
	// through [errors.Is]: two distinct errors of the same concrete type
	// never match each other, so give a sentinel a custom Is method or use
	// Predicate for class-based matching of custom errors.
	RetryOn []error

	// Predicate, when non-nil, is called with the raised error; returning
	// true triggers a retry in addition to the other rules. Like the Python
	// policy, it runs once per failed attempt — including the final attempt
	// that exceeds MaxRetries and is never retried — so counting or logging
	// predicates observe every failure. (One deliberate difference from
	// Python: a caller-canceled context returns before the predicate runs,
	// where tenacity would call it with CancelledError.)
	Predicate func(error) bool

	// Timeout is the total budget per SDK call, including the initial
	// attempt and all delays; 0 means unlimited. The loop stops before a
	// retry whose delay would reach the budget, re-raising the last error.
	Timeout time.Duration
}

// DefaultRetryPolicy returns the default retry configuration: two retries,
// 0.5s→5s exponential backoff with 25% jitter, statuses {408, 429,
// 500–599}, Retry-After honored, connection and timeout errors retried, and a
// 30s per-call budget.
func DefaultRetryPolicy() RetryPolicy {
	statuses := make(map[int]struct{}, 102)
	statuses[408] = struct{}{}
	statuses[429] = struct{}{}
	for status := 500; status < 600; status++ {
		statuses[status] = struct{}{}
	}
	return RetryPolicy{
		MaxRetries:         2,
		BackoffInitial:     500 * time.Millisecond,
		BackoffMax:         5 * time.Second,
		BackoffJitter:      0.25,
		HTTPStatuses:       statuses,
		RespectRetryAfter:  true,
		APIConnectionError: true,
		APITimeoutError:    true,
		Timeout:            30 * time.Second,
	}
}

// Validate checks retry counts, delays, jitter, and the optional budget.
func (p RetryPolicy) Validate() error {
	if p.MaxRetries < 0 {
		return newTypeSafeError("max_retries must be a non-negative integer.")
	}
	for _, backoff := range []struct {
		name  string
		value time.Duration
	}{
		{"backoff_initial", p.BackoffInitial},
		{"backoff_max", p.BackoffMax},
	} {
		if backoff.value < 0 {
			return newTypeSafeError("%s must be a non-negative, finite number of seconds.", backoff.name)
		}
	}
	if p.BackoffJitter < 0 || p.BackoffJitter > 1 || math.IsNaN(p.BackoffJitter) || math.IsInf(p.BackoffJitter, 0) {
		return newTypeSafeError("backoff_jitter must be between zero and one.")
	}
	if p.Timeout < 0 {
		return newTypeSafeError("timeout must be a positive, finite number of seconds.")
	}
	return nil
}

// copy returns an independent snapshot so per-call state never shares maps or
// slices with the client-level policy.
func (p RetryPolicy) copy() RetryPolicy {
	snapshot := p
	if p.HTTPStatuses != nil {
		snapshot.HTTPStatuses = make(map[int]struct{}, len(p.HTTPStatuses))
		for status := range p.HTTPStatuses {
			snapshot.HTTPStatuses[status] = struct{}{}
		}
	}
	if p.RetryOn != nil {
		snapshot.RetryOn = append([]error(nil), p.RetryOn...)
	}
	return snapshot
}

// retryable reports whether an attempt error triggers a retry: the built-in
// rule for its error kind, the RetryOn list, or the Predicate — whichever
// matches first, mirroring the Python policy's "builtin or exceptions or
// predicate" composition.
func (p RetryPolicy) retryable(err error) bool {
	builtin := false
	var timeoutErr *TimeoutError
	var connErr *ConnectionError
	var apiErr *APIError
	switch {
	case errors.As(err, &timeoutErr) && timeoutErr != nil:
		builtin = p.APITimeoutError
	case errors.As(err, &connErr) && connErr != nil:
		builtin = p.APIConnectionError
	case errors.As(err, &apiErr) && apiErr != nil:
		// The nil guard: a subclass whose embedded *APIError is nil unwraps
		// to a typed nil that errors.As matches; it carries no status.
		_, builtin = p.HTTPStatuses[apiErr.Status]
	}
	if builtin {
		return true
	}
	for _, target := range p.RetryOn {
		if errorMatches(err, target) {
			return true
		}
	}
	return p.Predicate != nil && p.Predicate(err)
}

// errorMatches reports whether err matches one RetryOn entry. Entries of an
// SDK error type (such as &NotFoundError{}) and typed-nil pointer entries
// (such as (*MyError)(nil)) are type selectors, matched with [errors.As] so
// wrapping, joined trees, and custom As methods all work. Every other entry
// is a sentinel matched strictly by identity through [errors.Is] — there is
// deliberately no fallback to the entry's concrete type, because two
// distinct sentinel values routinely share one (errors.New values share
// *errors.errorString, and custom sentinel types are just as ambiguous).
func errorMatches(err, target error) bool {
	if target == nil {
		return false
	}
	// Sentinels always match by identity, whatever the entry's kind.
	if errors.Is(err, target) {
		return true
	}
	targetType := reflect.TypeOf(target)
	typeSelector := isSDKErrorSelector(target) ||
		(targetType.Kind() == reflect.Ptr && reflect.ValueOf(target).IsNil())
	if !typeSelector {
		return false
	}
	// errors.As with a synthesized **T target. RetryOn entries implement
	// error, and both selector forms are pointer types, so the target is
	// always a valid non-nil pointer to an error-implementing type.
	asTarget := reflect.New(targetType)
	return errors.As(err, asTarget.Interface())
}

// isSDKErrorSelector reports whether err is a non-nil instance of one of the
// SDK's error types, which RetryOn treats as a type selector — the documented
// "&NotFoundError{}" form — rather than a sentinel.
func isSDKErrorSelector(err error) bool {
	switch err.(type) {
	case *APIError, *BadRequestError, *AuthenticationError, *PermissionDeniedError,
		*NotFoundError, *UnprocessableEntityError, *RateLimitError, *InternalServerError,
		*ResponseValidationError, *ConnectionError, *TimeoutError, *TypeSafeError:
		return true
	}
	return false
}

// retryAfterDelayMs extracts the server-requested delay from an APIError's
// headers, in milliseconds.
func retryAfterDelayMs(err error) (float64, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr == nil || apiErr.Headers == nil {
		return 0, false
	}
	delay := parseRetryAfter(apiErr.Headers)
	if delay == nil {
		return 0, false
	}
	return *delay, true
}

// computeDelay resolves the wait before the next attempt: the
// server-requested delay when RespectRetryAfter and available, otherwise
// exponential backoff. attempt is the 1-based number of the attempt that just
// failed.
func (p RetryPolicy) computeDelay(attempt int, err error, rand func() float64) time.Duration {
	if p.RespectRetryAfter {
		if delayMs, ok := retryAfterDelayMs(err); ok {
			return maxDurationFromMs(delayMs)
		}
	}
	return backoffDelay(attempt, p.BackoffInitial, p.BackoffMax, p.BackoffJitter, rand)
}

// backoffSeconds implements the un-jittered exponential cap and jitter math in
// seconds, matching the Python SDK exactly (including the exponent guard that
// survives initial=1e-300 / max=1e300 extremes).
func backoffSeconds(attempt int, initial, maximum, jitter float64, rand func() float64) float64 {
	if initial == 0 || maximum == 0 {
		return 0
	}
	exponent := float64(attempt - 1)
	exponential := maximum
	if exponent < math.Log2(maximum)-math.Log2(initial) {
		exponential = math.Ldexp(initial, int(exponent))
	}
	delay := exponential * (1 - rand()*jitter)
	return math.Min(exponential, math.Round(delay*1000)/1000)
}

// backoffDelay is backoffSeconds over time.Duration values, clamped to the
// representable duration range.
func backoffDelay(attempt int, initial, maximum time.Duration, jitter float64, rand func() float64) time.Duration {
	seconds := backoffSeconds(attempt, initial.Seconds(), maximum.Seconds(), jitter, rand)
	return maxDurationFromMs(seconds * 1000)
}

// maxDurationFromMs converts milliseconds, clamping out-of-range values to
// the maximum duration instead of wrapping negative.
func maxDurationFromMs(ms float64) time.Duration {
	if ms <= 0 {
		return 0
	}
	ns := ms * float64(time.Millisecond)
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ns)
}
