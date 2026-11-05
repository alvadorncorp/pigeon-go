package pigeon_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.alvadorn.org/pigeon"
)

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := pigeon.New(nil); err == nil {
		t.Fatal("nil configuration accepted")
	}
}

func TestNewSnapshotsConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer original-key" {
			t.Errorf("authorization = %q, want original credentials", got)
		}
		_, _ = io.WriteString(w, `{"id":"config-snapshot"}`)
	}))
	defer server.Close()
	config := &pigeon.Config{
		Provider: pigeon.ProviderResend,
		APIKey:   "original-key",
		BaseURL:  server.URL,
	}
	mailer, err := pigeon.New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.Provider = pigeon.ProviderLocal
	config.APIKey = "mutated-key"
	config.BaseURL = "https://unused.invalid"
	if mailer.Inbox() != nil {
		t.Fatal("configuration mutation changed backend")
	}
	email := &pigeon.Email{
		From:    pigeon.Address{Email: "sender@example.com"},
		To:      []pigeon.Address{{Email: "reader@example.com"}},
		Subject: "Config snapshot",
		Text:    "Credentials remain fixed after construction.",
	}
	receipt, sendErr := mailer.Send(t.Context(), email)
	if sendErr != nil || receipt.ID != "config-snapshot" {
		t.Fatalf("receipt = %+v, error = %v", receipt, sendErr)
	}
}
