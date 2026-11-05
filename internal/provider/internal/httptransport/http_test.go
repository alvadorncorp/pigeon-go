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
			e := responseError(provider.SendGrid, 429, []byte(body))
			if e.StatusCode != 429 || strings.Contains(e.Error(), "private") || strings.Contains(e.Error(), "secret") {
				t.Fatalf("unsafe error: %v", e)
			}
			if strings.HasPrefix(body, "{") && e.Message != "private@example.com" {
				t.Fatalf("diagnostic lost: %q", e.Message)
			}
		})
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

func TestHTTPRejectsUnsafeOrigins(t *testing.T) {
	for _, origin := range []string{
		"http://example.com", "http://localhost", "https://key@example.com",
		"https://example.com/path", "https://example.com?key=secret", "https://example.com#fragment",
		"file:///tmp/mail", "https:///missing",
	} {
		if _, err := New(&provider.Config{BaseURL: origin}, ""); err == nil {
			t.Errorf("accepted unsafe origin %q", origin)
		}
	}
}
