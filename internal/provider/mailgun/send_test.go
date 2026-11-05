package mailgun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func mailgunTestEmail() *message.Email {
	return &message.Email{
		From:    message.Address{Name: "Sender", Email: "sender@example.com"},
		To:      []message.Address{{Name: "Doe, Jane", Email: "jane@example.com"}, {Email: "other@example.com"}},
		Cc:      []message.Address{{Email: "cc@example.com"}},
		Bcc:     []message.Address{{Email: "bcc@example.com"}},
		ReplyTo: message.Address{Email: "reply@example.com"},
		Subject: "Hello", Text: "plain body", HTML: "<p>html body</p>",
	}
}

func TestMailgunMultipartRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/mg.example.com/messages" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "api" || password != "secret:key" {
			t.Error("incorrect basic authentication")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.MultipartForm.RemoveAll(); err != nil {
			t.Errorf("remove multipart temporary files: %v", err)
		}
		want := map[string][]string{
			"from": {"\"Sender\" <sender@example.com>"},
			"to":   {"\"Doe, Jane\" <jane@example.com>", "other@example.com"},
			"cc":   {"cc@example.com"}, "bcc": {"bcc@example.com"},
			"h:Reply-To": {"reply@example.com"}, "subject": {"Olá 世界"},
			"text": {"Olá\r\n世界\r\n"}, "html": {"<p>Olá\r\n世界</p>"},
		}
		if !reflect.DeepEqual(r.MultipartForm.Value, want) {
			t.Errorf("multipart fields = %#v, want %#v", r.MultipartForm.Value, want)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"id":"<mailgun-id@example.com>","message":"Queued"}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	sender, err := New(provider.Config{APIKey: "secret:key", Domain: "mg.example.com", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	email := mailgunTestEmail()
	email.Subject = "Olá 世界"
	email.Text = "Olá\r\n世界\r\n"
	email.HTML = "<p>Olá\r\n世界</p>"
	receipt, err := sender.Send(context.Background(), email)
	if err != nil || receipt.ID != "<mailgun-id@example.com>" {
		t.Fatalf("Send = %#v, %v", receipt, err)
	}
}

func TestMailgunInvalidConfiguration(t *testing.T) {
	for _, domain := range []string{"", ".example.com", "example.com.", "example..com", "-bad.example", "bad-.example", "example.com/other", "example.com?x=y", "example.com:443", "é.example", strings.Repeat("a", 64) + ".example"} {
		t.Run(domain, func(t *testing.T) {
			if _, err := New(provider.Config{APIKey: "key", Domain: domain}); err == nil {
				t.Fatalf("accepted invalid domain %q", domain)
			}
		})
	}
	if _, err := New(provider.Config{Domain: "example.com"}); err == nil {
		t.Error("accepted empty API key")
	}
}

func TestMailgunResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		delivery bool
	}{
		{"bad request", 400, `{"message":"invalid"}`, true},
		{"unauthorized", 401, "unauthorized", true},
		{"rate limit", 429, `{"message":"rate limited"}`, true},
		{"server error", 500, "broken", true},
		{"invalid json", 200, "{", false},
		{"missing id", 200, `{"message":"queued"}`, false},
		{"empty id", 200, `{"id":""}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := io.WriteString(w, tc.body); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			sender, err := New(provider.Config{APIKey: "key", Domain: "example.com", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sender.Send(context.Background(), mailgunTestEmail())
			if err == nil {
				t.Fatal("expected error")
			}
			delivery, ok := errors.AsType[*message.DeliveryError](err)
			if ok != tc.delivery {
				t.Fatalf("delivery error = %v, want %v: %v", ok, tc.delivery, err)
			}
			if ok && delivery.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", delivery.StatusCode, tc.status)
			}
		})
	}
}

func TestMailgunCancellation(t *testing.T) {
	sender, err := New(provider.Config{APIKey: "key", Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sender.Send(ctx, mailgunTestEmail())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func BenchmarkMailgunSend(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			b.Errorf("read request: %v", err)
			return
		}
		if _, err := fmt.Fprint(w, `{"id":"benchmark"}`); err != nil {
			b.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	sender, err := New(provider.Config{APIKey: "key", Domain: "example.com", BaseURL: server.URL})
	if err != nil {
		b.Fatal(err)
	}
	email := mailgunTestEmail()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sender.Send(context.Background(), email); err != nil {
			b.Fatal(err)
		}
	}
}
