package sendgrid

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func TestSendGridPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/mail/send" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept = %q", got)
		}
		var got any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var want any
		if err := json.Unmarshal([]byte(`{"personalizations":[{"to":[{"name":"To Name","email":"to@example.com"}],"cc":[{"email":"cc@example.com"}],"bcc":[{"email":"bcc@example.com"}]}],"from":{"name":"Sender","email":"from@example.com"},"reply_to":{"name":"Reply","email":"reply@example.com"},"subject":"Hello <world>","content":[{"type":"text/plain","value":"plain & text"},{"type":"text/html","value":"<b>html</b>"}]}`), &want); err != nil {
			t.Fatalf("decode expected payload: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("payload = %#v; want %#v", got, want)
		}
		w.Header().Set("X-Message-Id", "accepted-id")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender, err := New(provider.Config{Provider: provider.SendGrid, APIKey: "secret-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	email := &message.Email{
		From:    message.Address{Name: "Sender", Email: "from@example.com"},
		To:      []message.Address{{Name: "To Name", Email: "to@example.com"}},
		Cc:      []message.Address{{Email: "cc@example.com"}},
		Bcc:     []message.Address{{Email: "bcc@example.com"}},
		ReplyTo: message.Address{Name: "Reply", Email: "reply@example.com"},
		Subject: "Hello <world>", Text: "plain & text", HTML: "<b>html</b>",
	}
	receipt, err := sender.Send(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ID != "accepted-id" {
		t.Errorf("receipt ID = %q", receipt.ID)
	}
}

func TestSendGridOptionalFieldsAndEmptyReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := got["reply_to"]; ok {
			t.Error("reply_to must be omitted")
		}
		var personalizations []map[string]json.RawMessage
		if err := json.Unmarshal(got["personalizations"], &personalizations); err != nil || len(personalizations) != 1 {
			t.Errorf("personalizations = %s, error = %v", got["personalizations"], err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, field := range []string{"cc", "bcc"} {
			if _, ok := personalizations[0][field]; ok {
				t.Errorf("%s must be omitted", field)
			}
		}
		if string(got["content"]) != `[{"type":"text/html","value":"only html"}]` {
			t.Errorf("content = %s", got["content"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sender, err := New(provider.Config{Provider: provider.SendGrid, APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := sender.Send(t.Context(), &message.Email{
		From:    message.Address{Email: "from@example.com"},
		To:      []message.Address{{Email: "to@example.com"}},
		Subject: "subject", HTML: "only html",
	})
	if err != nil || receipt.ID != "" {
		t.Fatalf("receipt = %#v, error = %v", receipt, err)
	}
}

func TestSendGridErrors(t *testing.T) {
	if _, err := New(provider.Config{}); err == nil {
		t.Error("missing API key accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if _, err := io.WriteString(w, `{"errors":[{"message":"sensitive provider detail","field":"from"}]}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	sender, err := New(provider.Config{Provider: provider.SendGrid, APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	email := &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "s", Text: "body"}
	_, err = sender.Send(t.Context(), email)
	deliveryErr, ok := errors.AsType[*message.DeliveryError](err)
	if !ok || deliveryErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 delivery error, got %v", err)
	}
	if strings.Contains(err.Error(), "sensitive provider detail") {
		t.Errorf("error exposes provider response: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = sender.Send(ctx, email)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled send: %v", err)
	}
}

func BenchmarkSendGrid(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			b.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sender, err := New(provider.Config{Provider: provider.SendGrid, APIKey: "key", BaseURL: server.URL})
	if err != nil {
		b.Fatal(err)
	}
	email := &message.Email{From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}}, Subject: "subject", Text: "body"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sender.Send(b.Context(), email); err != nil {
			b.Fatal(err)
		}
	}
}
