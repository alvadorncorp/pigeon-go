package pigeon_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.alvadorn.org/pigeon"
)

func TestSendRejectsInvalidMessagesBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"id":"should-not-send"}`)
	}))
	defer server.Close()
	mailer, err := pigeon.New(pigeon.ProviderResend, pigeon.WithResend("test-key"), pigeon.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	email := &pigeon.Email{
		From: pigeon.Address{Email: "sender@example.com"}, To: []pigeon.Address{{Email: "reader@example.com"}},
		Subject: "Hello", Text: "Body",
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mailer.Send(cancelled, email); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	email.Subject = "Hello\r\nBcc: victim@example.com"
	if _, err := mailer.Send(t.Context(), email); err == nil {
		t.Fatal("header injection accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid or cancelled submission reached provider")
	}
}

func TestLocalInboxUsesSameMailerAndRetainsSnapshot(t *testing.T) {
	mailer, err := pigeon.New(pigeon.ProviderLocal, pigeon.WithLocal(pigeon.LocalLimits{Capacity: 2}))
	if err != nil {
		t.Fatal(err)
	}
	email := &pigeon.Email{
		From: pigeon.Address{Email: "sender@example.com"}, To: []pigeon.Address{{Email: "original@example.com"}},
		Subject: "A real local letter", Text: "Keep this body",
	}
	if _, err = mailer.Send(t.Context(), email); err != nil {
		t.Fatal(err)
	}
	email.Subject = "Mutated subject"
	email.To[0].Email = "mutated@example.com"
	mux := http.NewServeMux()
	mux.Handle("/mail/", http.StripPrefix("/mail", mailer.Inbox()))
	server := httptest.NewServer(mux)
	defer server.Close()
	resp, err := http.Get(server.URL + "/mail/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.Contains(string(body), "A real local letter") ||
		strings.Contains(string(body), "Mutated subject") {
		t.Fatalf("unexpected inbox: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	for _, kind := range []pigeon.Provider{"", "typo"} {
		if _, err := pigeon.New(kind); err == nil {
			t.Fatalf("provider %q silently accepted", kind)
		}
	}
}

// The public constants derive from the internal provider.Kind constants. This
// pins the published string values, which also appear in DeliveryError labels
// and in documentation, against accidental changes.
func TestProviderConstantsKeepTheirPublishedValues(t *testing.T) {
	for constant, want := range map[pigeon.Provider]string{
		pigeon.ProviderSES:      "ses",
		pigeon.ProviderResend:   "resend",
		pigeon.ProviderMailgun:  "mailgun",
		pigeon.ProviderSendGrid: "sendgrid",
		pigeon.ProviderLocal:    "local",
	} {
		if got := string(constant); got != want {
			t.Errorf("provider constant = %q, want %q", got, want)
		}
	}
}

func TestCloudInboxIsNotFoundHandler(t *testing.T) {
	for _, kind := range []pigeon.Provider{
		pigeon.ProviderSES, pigeon.ProviderResend, pigeon.ProviderMailgun, pigeon.ProviderSendGrid,
	} {
		t.Run(string(kind), func(t *testing.T) {
			mailer, err := pigeon.New(kind, providerOption(kind))
			if err != nil {
				t.Fatal(err)
			}
			handlers := []struct {
				name    string
				handler http.Handler
			}{
				{"constructed", mailer.Inbox()},
				{"zero value", (&pigeon.Mailer{}).Inbox()},
				{"nil value", (*pigeon.Mailer)(nil).Inbox()},
			}
			for _, h := range handlers {
				recorder := httptest.NewRecorder()
				h.handler.ServeHTTP(
					recorder,
					httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody),
				)
				if recorder.Code != http.StatusNotFound {
					t.Errorf("%s inbox status = %d, want 404", h.name, recorder.Code)
				}
			}
		})
	}
}

func TestSendRejectsUninitializedMailerAndNilArguments(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"id":"should-not-send"}`)
	}))
	defer server.Close()
	mailer, err := pigeon.New(pigeon.ProviderResend, pigeon.WithResend("test-key"), pigeon.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	email := validEmail("Body")
	tests := []struct {
		name   string
		mailer *pigeon.Mailer
		ctx    context.Context
		email  *pigeon.Email
	}{
		{"zero mailer", &pigeon.Mailer{}, t.Context(), email},
		{"nil mailer", nil, t.Context(), email},
		{"nil context", mailer, nil, email},
		{"nil email", mailer, t.Context(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.mailer.Send(tc.ctx, tc.email); err == nil {
				t.Fatal("invalid call was accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid calls reached the provider %d times", calls.Load())
	}
}

func TestSendRejectsOversizeMessageBeforeSubmission(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"id":"should-not-send"}`)
	}))
	defer server.Close()
	for _, kind := range []pigeon.Provider{pigeon.ProviderResend, pigeon.ProviderSES} {
		t.Run(string(kind), func(t *testing.T) {
			opts := []pigeon.Option{providerOption(kind), pigeon.WithBaseURL(server.URL)}
			mailer, err := pigeon.New(kind, opts...)
			if err != nil {
				t.Fatal(err)
			}
			const oneMiB = 1 << 20
			oversize := validEmail(strings.Repeat("a", oneMiB))
			if _, err := mailer.Send(t.Context(), oversize); err == nil {
				t.Fatal("an oversized message was accepted")
			}
			if calls.Load() != 0 {
				t.Fatalf("oversized submission reached the provider %d times", calls.Load())
			}
		})
	}
}
