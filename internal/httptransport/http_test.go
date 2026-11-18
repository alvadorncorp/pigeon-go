package httptransport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func TestHTTPRejectsRedirectWithoutLeakingMessage(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	original := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		t.Error("caller redirect policy was used")
		return nil
	}}
	h, err := New(&provider.Config{Provider: provider.Resend, BaseURL: origin.URL, HTTPClient: original}, "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin.URL, strings.NewReader("private email"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	_, _, err = h.Do(req)
	delivery, ok := errors.AsType[*message.DeliveryError](err)
	if !ok || delivery.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v", err)
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect received the message")
	}
}

func TestHTTPErrorDiagnosticsDoNotLeakThroughError(t *testing.T) {
	for _, body := range []string{
		`{"message":"private@example.com","name":"secret-token"}`,
		`{"errors":[{"message":"private@example.com","field":"from"}]}`,
		`<html>upstream failed</html>`,
	} {
		t.Run(body, func(t *testing.T) {
			e := responseError(provider.SendGrid, &http.Response{StatusCode: http.StatusTooManyRequests}, []byte(body))
			if e.StatusCode != http.StatusTooManyRequests || strings.Contains(e.Error(), "private") ||
				strings.Contains(e.Error(), "secret") {
				t.Fatalf("unsafe error: %v", e)
			}
			if strings.HasPrefix(body, "{") && e.Message != "private@example.com" {
				t.Fatalf("diagnostic lost: %q", e.Message)
			}
		})
	}
}

func TestResponseErrorCodePrecedence(t *testing.T) {
	for _, tc := range []struct{ header, body, want string }{
		{"MessageRejected:http://internal.amazon.com/coral/", `{"message":"x"}`, "MessageRejected"},
		{"aws.ses#NotFoundException", `{}`, "NotFoundException"},
		{"", `{"code":"A","name":"B","__type":"C"}`, "A"},
		{"", `{"name":"B","__type":"C"}`, "B"},
		{"", `{"__type":"C"}`, "C"},
	} {
		resp := &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}}
		if tc.header != "" {
			resp.Header.Set("X-Amzn-Errortype", tc.header)
		}
		if got := responseError(provider.SES, resp, []byte(tc.body)).Code; got != tc.want {
			t.Errorf("header %q body %s: code = %q, want %q", tc.header, tc.body, got, tc.want)
		}
	}
}

// A field of the wrong JSON type must not discard the decoded diagnostics.
func TestResponseErrorKeepsFieldsAroundTypeMismatch(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusUnprocessableEntity, Header: http.Header{}}
	e := responseError(provider.Resend, resp, []byte(`{"message":"bad","name":["x"]}`))
	if e.Message != "bad" {
		t.Fatalf("message = %q, want %q kept despite the wrong JSON type of name", e.Message, "bad")
	}
	if e.Code != "" {
		t.Fatalf("code = %q, want empty from the mismatched name field", e.Code)
	}
}

func TestHTTPOversizedErrorKeepsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 2*maxResponseBytes))
	}))
	defer server.Close()
	h, err := New(&provider.Config{Provider: provider.Resend, BaseURL: server.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = h.Do(req)
	if delivery, ok := errors.AsType[*message.DeliveryError](err); !ok || delivery.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %v, want DeliveryError with 502", err)
	}
}

// Named waits keep a hung server from stalling the run without magic numbers.
const (
	enterWait        = 5 * time.Second
	preservedTimeout = 5 * time.Second
)

func TestHTTPClientTimeoutDefaults(t *testing.T) {
	for name, client := range map[string]*http.Client{"nil client": nil, "zero timeout": {}} {
		h, err := New(&provider.Config{Provider: provider.Resend, HTTPClient: client}, "https://api.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if h.client.Timeout != defaultTimeout || defaultTimeout != 30*time.Second {
			t.Errorf("%s: timeout = %v, want 30s", name, h.client.Timeout)
		}
	}
}

func TestHTTPClientTimeoutPreserved(t *testing.T) {
	h, err := New(&provider.Config{
		Provider:   provider.Resend,
		HTTPClient: &http.Client{Timeout: preservedTimeout},
	}, "https://api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if h.client.Timeout != preservedTimeout {
		t.Fatalf("timeout = %v, want preserved %v", h.client.Timeout, preservedTimeout)
	}
}

// The provider accepted the delivery, so an oversized 2xx body costs only the
// receipt: Do returns nil body with nil error.
func TestHTTPOversized2xxBodyIsAcceptedWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
	}))
	defer server.Close()
	h, req := newTestClient(t, server.URL)
	body, headers, err := h.Do(req)
	if err != nil {
		t.Fatalf("oversized 2xx body must not fail an accepted delivery: %v", err)
	}
	if body != nil {
		t.Fatalf("body = %d bytes, want nil", len(body))
	}
	if headers == nil {
		t.Fatal("headers = nil, want response headers")
	}
}

func TestHTTP2xxBodyCapBoundary(t *testing.T) {
	for _, size := range []int{maxResponseBytes - 1, maxResponseBytes} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, strings.Repeat("x", size))
			}))
			defer server.Close()
			h, req := newTestClient(t, server.URL)
			body, _, err := h.Do(req)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(body) != size {
				t.Fatalf("body = %d bytes, want %d", len(body), size)
			}
		})
	}
}

// A declared Content-Length with fewer bytes written closes the connection
// mid-body: a 2xx read failure must not become a failed delivery either.
func TestHTTP2xxTruncatedBodyIsAcceptedWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(2*maxResponseBytes))
		_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes))
	}))
	defer server.Close()
	h, req := newTestClient(t, server.URL)
	body, _, err := h.Do(req)
	if err != nil {
		t.Fatalf("truncated 2xx body must not fail an accepted delivery: %v", err)
	}
	if body != nil {
		t.Fatalf("body = %d bytes, want nil", len(body))
	}
}

func TestHTTPInFlightCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	h, err := New(&provider.Config{Provider: provider.Resend, BaseURL: server.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := h.Do(req); done <- err }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("request never reached the server before the test deadline")
	case <-time.After(enterWait):
		t.Fatal("request never reached the server in time")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

// newTestClient returns a Client for the given server URL and a POST request to it.
func newTestClient(t *testing.T, serverURL string) (*Client, *http.Request) {
	t.Helper()
	h, err := New(&provider.Config{Provider: provider.Resend, BaseURL: serverURL}, "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, serverURL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	return &h, req
}

func TestHTTPClientValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	original := &http.Client{Timeout: -1}
	if _, err := New(
		&provider.Config{Provider: provider.Resend, BaseURL: server.URL, HTTPClient: original},
		"",
	); err == nil {
		t.Fatal("negative timeout accepted")
	}
	h, err := New(&provider.Config{Provider: provider.Resend, BaseURL: server.URL + "/"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.BaseURL() != server.URL {
		t.Fatalf("base URL = %q, want trailing slash removed", h.BaseURL())
	}
}

func TestHTTPRejectsUnsafeOrigins(t *testing.T) {
	for _, origin := range []string{
		"http://example.com", "http://localhost", "https://key@example.com",
		"https://example.com/path", "https://example.com?key=secret", "https://example.com#fragment",
		"file:///tmp/mail", "https:///missing", "http://192.0.2.1", "https://example.com#", "https://example.com?",
	} {
		if _, err := New(&provider.Config{BaseURL: origin}, ""); err == nil {
			t.Errorf("accepted unsafe origin %q", origin)
		}
	}
}

func TestRequireAPIKeyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		key      string
		accepted bool
	}{
		{"", false},
		{"\x1f", false},
		{"\x20", false},
		{"\x21", true},
		{"\x7e", true},
		{"\x7f", false},
	} {
		if accepted := RequireAPIKey(tc.key) == nil; accepted != tc.accepted {
			t.Errorf("key %#v: accepted = %v, want %v", tc.key, accepted, tc.accepted)
		}
	}
}

func TestEncodeJSONKeepsRawHTMLOnOneLine(t *testing.T) {
	type payload struct {
		HTML string `json:"html"`
	}
	body, err := EncodeJSON(payload{HTML: "<p>a & b > c</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"html":"<p>a & b > c</p>"}`; string(body) != want {
		t.Fatalf("body = %q, want %q with raw < > & and no trailing newline", body, want)
	}
}
