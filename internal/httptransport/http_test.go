package httptransport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestHTTPOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
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
	if _, _, err := h.Do(req); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
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
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
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
