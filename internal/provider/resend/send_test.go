package resend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func TestResendSendRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		for key, want := range map[string]string{
			"Authorization": "Bearer test-key",
			"Content-Type":  "application/json",
			"Accept":        "application/json",
		} {
			if got := r.Header.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		for key, want := range map[string]string{"subject": "Hello", "text": "Plain", "html": "<p>HTML</p>"} {
			if got := payload[key]; got != want {
				t.Errorf("%s = %v, want %q", key, got, want)
			}
		}
		resendCheckAddress(t, payload["from"], "Doe, Jane", "from@example.com")
		resendCheckAddress(t, payload["reply_to"], "Réponse, Élodie", "reply@example.com")
		for key, want := range map[string][2]string{
			"to":  {"José, Silva", "to@example.com"},
			"cc":  {"Copy", "cc@example.com"},
			"bcc": {"Blind", "bcc@example.com"},
		} {
			items, ok := payload[key].([]any)
			if !ok || len(items) != 1 {
				t.Errorf("%s = %#v, want one address", key, payload[key])
				continue
			}
			resendCheckAddress(t, items[0], want[0], want[1])
		}
		if len(payload) != 8 {
			t.Errorf("unexpected payload fields: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"accepted-123"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	sender, err := New(provider.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := sender.Send(t.Context(), &message.Email{
		From:    message.Address{Name: "Doe, Jane", Email: "from@example.com"},
		To:      []message.Address{{Name: "José, Silva", Email: "to@example.com"}},
		Cc:      []message.Address{{Name: "Copy", Email: "cc@example.com"}},
		Bcc:     []message.Address{{Name: "Blind", Email: "bcc@example.com"}},
		ReplyTo: message.Address{Name: "Réponse, Élodie", Email: "reply@example.com"},
		Subject: "Hello", Text: "Plain", HTML: "<p>HTML</p>",
	})
	if err != nil || receipt.ID != "accepted-123" {
		t.Fatalf("Send = %#v, %v", receipt, err)
	}
}

func resendCheckAddress(t *testing.T, value any, name, address string) {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Errorf("address = %#v, want string", value)
		return
	}
	parsed, err := mail.ParseAddress(text)
	if err != nil {
		t.Errorf("parse address %q: %v", text, err)
		return
	}
	if parsed.Name != name || parsed.Address != address {
		t.Errorf("address = %#v, want name %q and email %q", parsed, name, address)
	}
}

func TestResendOmitsOptionalFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		want := map[string]any{"from": "from@example.com", "to": []any{"to@example.com"}, "subject": "Subject", "text": "Body"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("payload = %#v, want %#v", got, want)
		}
		if _, err := w.Write([]byte(`{"id":"ok"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	sender, err := New(provider.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sender.Send(t.Context(), &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "Subject", Text: "Body"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResendErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"invalid json", http.StatusOK, "{"},
		{"missing id", http.StatusOK, `{}`},
		{"empty id", http.StatusOK, `{"id":""}`},
		{"rejected", http.StatusUnauthorized, `{"message":"invalid key"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			sender, err := New(provider.Config{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := sender.Send(t.Context(), &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "Subject", Text: "Body"})
			if err == nil || receipt.ID != "" {
				t.Fatalf("Send = %#v, %v, want error and empty ID", receipt, err)
			}
			if tc.status != http.StatusOK {
				var deliveryErr *message.DeliveryError
				if !errors.As(err, &deliveryErr) || deliveryErr.StatusCode != tc.status {
					t.Errorf("error = %v, want DeliveryError status %d", err, tc.status)
				}
			}
		})
	}
	if _, err := New(provider.Config{}); err == nil {
		t.Error("New with no API key succeeded")
	}
}

func TestResendCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("canceled request reached server")
	}))
	defer server.Close()
	sender, err := New(provider.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = sender.Send(ctx, &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "Subject", Text: "Body"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context canceled", err)
	}
}

func BenchmarkResendSend(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"id":"ok"}`)); err != nil {
			b.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	sender, err := New(provider.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		b.Fatal(err)
	}
	email := &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "Subject", Text: "Body"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sender.Send(context.Background(), email); err != nil {
			b.Fatal(err)
		}
	}
}
