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
	mailer, err := pigeon.New(&pigeon.Config{Provider: pigeon.ProviderResend, APIKey: "test-key", BaseURL: server.URL})
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
	mailer, err := pigeon.New(&pigeon.Config{Provider: pigeon.ProviderLocal, LocalCapacity: 2})
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
		if _, err := pigeon.New(&pigeon.Config{Provider: kind}); err == nil {
			t.Fatalf("provider %q silently accepted", kind)
		}
	}
}
