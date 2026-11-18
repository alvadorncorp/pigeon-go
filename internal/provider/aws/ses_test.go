package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func sesTestConfig(url string) *provider.Config {
	return &provider.Config{
		Provider: provider.SES, BaseURL: url, Region: "us-east-1",
		AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "test-secret", SessionToken: "session-token",
	}
}

func verifySESProtocol(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v2/email/outbound-emails" ||
		r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
	}
	if err := sesVerifyRequest(r, body, "test-secret", "us-east-1", "session-token"); err != nil {
		t.Error(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Error(err)
		return
	}
	want := map[string]any{
		// From carries a display name to exercise the header formatting path.
		"FromEmailAddress": `"Sender Name" <from@example.com>`,
		"Destination": map[string]any{
			"ToAddresses": []any{"to@example.com"}, "CcAddresses": []any{"cc@example.com"},
			"BccAddresses": []any{"bcc@example.com"},
		},
		"ReplyToAddresses": []any{"reply@example.com"},
		"Content": map[string]any{"Simple": map[string]any{
			"Subject": map[string]any{"Data": "Olá 世界", "Charset": "UTF-8"},
			"Body": map[string]any{
				"Text": map[string]any{"Data": "Olá <world>", "Charset": "UTF-8"},
				"Html": map[string]any{"Data": "<b>世界</b>", "Charset": "UTF-8"},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %#v, want %#v", got, want)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprint(w, `{"MessageId":"ses-id"}`); err != nil {
		t.Error(err)
	}
}

func TestSESSendProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifySESProtocol(t, w, r)
	}))
	defer server.Close()
	s, err := New(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	email := &message.Email{
		From: message.Address{Name: "Sender Name", Email: "from@example.com"},
		To:   []message.Address{{Email: "to@example.com"}},
		Cc:   []message.Address{{Email: "cc@example.com"}}, Bcc: []message.Address{{Email: "bcc@example.com"}},
		ReplyTo: message.Address{Email: "reply@example.com"}, Subject: "Olá 世界",
		Text: "Olá <world>", HTML: "<b>世界</b>",
	}
	receipt, err := s.Send(t.Context(), email)
	if err != nil || receipt.ID != "ses-id" {
		t.Fatalf("Send = %#v, %v", receipt, err)
	}
}

func TestSESConcurrentSends(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if err := sesVerifyRequest(r, body, "test-secret", "us-east-1", "session-token"); err != nil {
			t.Error(err)
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"MessageId":"ses-id"}`)
	}))
	defer server.Close()
	s, err := New(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			email := &message.Email{
				From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
				Subject: fmt.Sprintf("message %d", i), Text: "body",
			}
			if _, err := s.Send(t.Context(), email); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestSESResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		delivery bool
		id       string
	}{
		{"rejected", http.StatusBadRequest, `{"message":"rejected","__type":"MessageRejected"}`, true, ""},
		{"accepted empty body", http.StatusOK, "", false, ""},
		{"accepted invalid JSON", http.StatusOK, `{`, false, ""},
		{"accepted missing ID", http.StatusOK, `{}`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkSESResponse(t, tc.status, tc.body, tc.delivery, tc.id)
		})
	}
}

func checkSESResponse(t *testing.T, status int, body string, wantDelivery bool, wantID string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	s, err := New(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Send(t.Context(), &message.Email{})
	if wantDelivery {
		if err == nil {
			t.Fatal("expected error")
		}
		var delivery *message.DeliveryError
		if !errors.As(err, &delivery) {
			t.Fatalf("error %v is not a delivery error", err)
		}
		if delivery.StatusCode != status {
			t.Errorf("status = %d", delivery.StatusCode)
		}
		return
	}
	// An accepted message succeeds even when the response carries no usable receipt.
	if err != nil {
		t.Fatalf("accepted response produced error: %v", err)
	}
	if receipt.ID != wantID {
		t.Errorf("receipt ID = %q, want %q", receipt.ID, wantID)
	}
}

var sesOptionalFieldCases = []struct {
	name  string
	email message.Email
	check func(t *testing.T, payload map[string]any)
}{
	{
		name: "html only",
		email: message.Email{
			From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
			Subject: "optional fields", HTML: "<b>hi</b>",
		},
		check: func(t *testing.T, payload map[string]any) {
			body := sesSimpleBody(t, payload)
			if _, ok := body["Text"]; ok {
				t.Error("empty Text produced a Text key")
			}
			if _, ok := body["Html"]; !ok {
				t.Error("Html key missing")
			}
		},
	},
	{
		name: "text only",
		email: message.Email{
			From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
			Subject: "optional fields", Text: "hi",
		},
		check: func(t *testing.T, payload map[string]any) {
			body := sesSimpleBody(t, payload)
			if _, ok := body["Html"]; ok {
				t.Error("empty HTML produced an Html key")
			}
			if _, ok := body["Text"]; !ok {
				t.Error("Text key missing")
			}
		},
	},
	{
		name: "no reply to",
		email: message.Email{
			From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
			Subject: "optional fields", Text: "hi", HTML: "<b>hi</b>",
		},
		check: func(t *testing.T, payload map[string]any) {
			if _, ok := payload["ReplyToAddresses"]; ok {
				t.Error("empty ReplyTo produced a ReplyToAddresses key")
			}
		},
	},
	{
		name: "no cc or bcc",
		email: message.Email{
			From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
			Subject: "optional fields", Text: "hi", HTML: "<b>hi</b>",
		},
		check: func(t *testing.T, payload map[string]any) {
			destination, ok := payload["Destination"].(map[string]any)
			if !ok {
				t.Fatal("payload has no Destination object")
			}
			for _, key := range []string{"CcAddresses", "BccAddresses"} {
				if _, ok := destination[key]; ok {
					t.Errorf("empty recipient group produced a %s key", key)
				}
			}
		},
	},
}

// sesSimpleBody returns the Content.Simple.Body object of a decoded SES payload.
func sesSimpleBody(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	content, _ := payload["Content"].(map[string]any)
	simple, _ := content["Simple"].(map[string]any)
	body, _ := simple["Body"].(map[string]any)
	if body == nil {
		t.Fatal("payload has no Content.Simple.Body object")
	}
	return body
}

// sesOptionalServer decodes every request body and applies check, answering
// with an accepted receipt.
func sesOptionalServer(t *testing.T, check func(t *testing.T, payload map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("payload: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		check(t, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"MessageId":"optional-id"}`)
	}))
}

// TestSESOptionalFieldsOmitted proves each optional field stays absent from
// the SES payload when the corresponding Email field is left empty.
func TestSESOptionalFieldsOmitted(t *testing.T) {
	for _, tc := range sesOptionalFieldCases {
		t.Run(tc.name, func(t *testing.T) {
			server := sesOptionalServer(t, tc.check)
			defer server.Close()
			s, err := New(sesTestConfig(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := s.Send(t.Context(), &tc.email)
			if err != nil {
				t.Fatalf("Send = %v", err)
			}
			if receipt.ID != "optional-id" {
				t.Errorf("receipt ID = %q, want %q", receipt.ID, "optional-id")
			}
		})
	}
}

func TestSESContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("canceled request reached server")
	}))
	defer server.Close()
	s, err := New(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Send(ctx, &message.Email{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Send error = %v", err)
	}
}

func TestSESConfig(t *testing.T) {
	for _, region := range []string{
		"", "us-east-1.example.com", "us-east-1/path", "us-east-1:443",
		"-us", "US-EAST-1", strings.Repeat("a", 64), "cn-north-1",
	} {
		config := sesTestConfig("")
		config.Region = region
		if _, err := New(config); err == nil {
			t.Errorf("accepted region %q", region)
		}
	}
	config := sesTestConfig("")
	config.Region = "eu-west-1"
	s, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://email.eu-west-1.amazonaws.com"; s.http.BaseURL() != want {
		t.Errorf("endpoint = %q, want %q", s.http.BaseURL(), want)
	}
	for name, edit := range map[string]func(*provider.Config){
		"empty access key":        func(c *provider.Config) { c.AccessKeyID = "" },
		"empty secret":            func(c *provider.Config) { c.SecretAccessKey = "" },
		"access key delimiter":    func(c *provider.Config) { c.AccessKeyID = "AKID/x" },
		"access key control byte": func(c *provider.Config) { c.AccessKeyID = "AKID\x00" },
		"token with CR":           func(c *provider.Config) { c.SessionToken = "token\r" },
		"token with space":        func(c *provider.Config) { c.SessionToken = "to ken" },
	} {
		config := sesTestConfig("")
		edit(config)
		if _, err := New(config); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}
