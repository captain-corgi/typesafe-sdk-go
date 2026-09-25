package typesafe

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestSuppliedClientRedirectBoundary(t *testing.T) {
	for _, target := range []string{"http://api.test/next", "https://sub.api.test/next", "https://api.test:444/next", "https://other.test/next", "https://user@api.test/next", "https://api.test/next", "https://API.test:443/next"} {
		for _, callback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/callback=%v", target, callback), func(t *testing.T) {
				hits := 0
				transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					hits++
					if req.Header.Get("Authorization") != "Bearer test" {
						t.Fatal("same-origin authorization lost")
					}
					if hits == 1 {
						return textResponseWithHeaders(req, 302, "", map[string]string{"Location": target}), nil
					}
					return textResponse(req, 200, `{"models":[]}`), nil
				})
				jar, _ := cookiejar.New(nil)
				supplied := &http.Client{Transport: transport, Jar: jar}
				if callback {
					supplied.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
				}
				client, err := NewClient(WithAPIKey("test"), WithBaseURL("https://api.test"), WithHTTPClient(supplied))
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				req, _ := http.NewRequestWithContext(t.Context(), "GET", "https://api.test/start", nil)
				req.Header.Set("Authorization", "Bearer test")
				resp, err := client.httpClient.Do(req)
				if resp != nil {
					resp.Body.Close()
				}
				safe := target == "https://api.test/next" || target == "https://API.test:443/next"
				if safe && (err != nil || hits != 2) {
					t.Fatalf("safe redirect: hits=%d err=%v", hits, err)
				}
				if !safe && (err == nil || hits != 1) {
					t.Fatalf("unsafe redirect sent: hits=%d err=%v", hits, err)
				}
				if client.httpClient.Jar != jar || (supplied.CheckRedirect != nil) != callback {
					t.Fatal("supplied client changed")
				}
			})
		}
	}
}

func TestRedirectCallbackAndLimit(t *testing.T) {
	for _, mode := range []string{"rewrite", "history", "loop", "stop", "local"} {
		t.Run(mode, func(t *testing.T) {
			hits := 0
			base := "https://api.test"
			if mode == "local" {
				base = "http://127.0.0.1"
			}
			supplied := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				hits++
				if mode == "local" && hits == 2 {
					return textResponse(req, 200, ""), nil
				}
				target := base + "/next"
				if mode == "history" && hits == 2 {
					target = "http://api.test/next"
				}
				return textResponseWithHeaders(req, 302, "", map[string]string{"Location": target}), nil
			})}
			if mode != "loop" {
				supplied.CheckRedirect = func(req *http.Request, via []*http.Request) error {
					switch mode {
					case "rewrite":
						req.URL.Scheme = "http"
					case "history":
						via[0].URL.Scheme = "http"
					case "stop":
						return http.ErrUseLastResponse
					}
					return nil
				}
			}
			client, err := NewClient(WithAPIKey("test"), WithBaseURL(base), WithAllowInsecureHTTP(), WithHTTPClient(supplied))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			req, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/start", nil)
			resp, err := client.httpClient.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			want := map[string]int{"rewrite": 1, "history": 2, "loop": 10, "stop": 1, "local": 2}[mode]
			if hits != want {
				t.Fatalf("hits=%d want=%d", hits, want)
			}
			if (mode == "stop" || mode == "local") != (err == nil) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	// Equivalent default ports are the same origin; nondefault ports are not.
	a, _ := url.Parse("https://api.test")
	b, _ := url.Parse("https://API.test:443")
	if !sameOrigin(a, b) {
		t.Fatal("default port normalization")
	}
}

func TestStrictWireLogging(t *testing.T) {
	var buf bytes.Buffer
	swapLogger(t, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	prior := logBodyMode()
	t.Cleanup(func() { _ = SetLogBodyMode(prior); SetSensitiveHeaders() })
	t.Setenv(EnvLogBody, "strict")
	setupLogBodyMode()
	names := []string{"X-Credential", "X-Per-Call"}
	SetSensitiveHeaders(names...)
	names[0] = "Changed"
	client, err := NewClient(WithAPIKey("auth-private"), WithHeaders(map[string]string{"X-Credential": "default-private", "X-ApiKey": "key-private"}), WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return textResponseWithHeaders(req, 200, `{"model":"m","usage":{},"answers":{},"sensitive-field":987654321}`, map[string]string{"X-APIKEY": "response-private", "X-Api-Key": "response-key-private", "X-Credential": "custom-private", "X-Diagnostic": "visible"}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.SystemOne(t.Context(), &SystemOneParams{State: map[string]any{"sensitive-field": 987654321}, Questions: Questions{"q": Noul{Instructions: "question-private"}}, ExtraHeaders: map[string]string{"X-Per-Call": "call-private"}})
	if err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	for _, secret := range []string{"auth-private", "default-private", "key-private", "response-private", "response-key-private", "custom-private", "call-private", "sensitive-field", "987654321", "question-private"} {
		if strings.Contains(logged, secret) {
			t.Errorf("leaked %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "visible") || !strings.Contains(logged, "dir=->") || !strings.Contains(logged, "dir=<-") {
		t.Fatalf("missing diagnostics: %s", logged)
	}
	for _, body := range []string{`{"sensitive-field":[987654321,true,null,"secret"]}`, `987654321`, `true`, `null`, `"secret"`, `malformed`} {
		if got, want := formatLoggedBody([]byte(body)), fmt.Sprintf("<redacted %d bytes>", len(body)); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	SetSensitiveHeaders()
	if isSecretHeader("X-Credential") || !isSecretHeader("X-APIKEY") {
		t.Fatal("reset weakened built-in rules")
	}
}

func TestSensitiveHeadersConcurrent(t *testing.T) {
	t.Cleanup(func() { SetSensitiveHeaders() })
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			SetSensitiveHeaders("X-Credential")
			SetSensitiveHeaders()
		}
	})
	for range 100 {
		_ = redactHeaders(http.Header{"X-Credential": {"value"}, "X-APIKEY": {"key"}})
	}
	wg.Wait()
}
