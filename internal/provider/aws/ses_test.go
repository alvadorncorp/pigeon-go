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
		"FromEmailAddress": "from@example.com",
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
	s, err := NewSES(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	email := &message.Email{
		From: message.Address{Email: "from@example.com"}, To: []message.Address{{Email: "to@example.com"}},
		Cc: []message.Address{{Email: "cc@example.com"}}, Bcc: []message.Address{{Email: "bcc@example.com"}},
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
	s, err := NewSES(sesTestConfig(server.URL))
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
	}{
		{"rejected", http.StatusBadRequest, `{"message":"rejected","__type":"MessageRejected"}`, true},
		{"invalid JSON", http.StatusOK, `{`, false},
		{"missing ID", http.StatusOK, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkSESResponse(t, tc.status, tc.body, tc.delivery)
		})
	}
}

func checkSESResponse(t *testing.T, status int, body string, wantDelivery bool) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	s, err := NewSES(sesTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Send(t.Context(), &message.Email{})
	if err == nil {
		t.Fatal("expected error")
	}
	var delivery *message.DeliveryError
	if errors.As(err, &delivery) != wantDelivery {
		t.Fatalf("delivery error = %v; error %v", wantDelivery, err)
	}
	if wantDelivery && delivery.StatusCode != status {
		t.Errorf("status = %d", delivery.StatusCode)
	}
}

func TestSESContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("canceled request reached server")
	}))
	defer server.Close()
	s, err := NewSES(sesTestConfig(server.URL))
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
		"-us", "US-EAST-1", strings.Repeat("a", 64),
	} {
		config := sesTestConfig("")
		config.Region = region
		if _, err := NewSES(config); err == nil {
			t.Errorf("accepted region %q", region)
		}
	}
	for _, region := range []string{"us-east-1", "cn-north-1"} {
		config := sesTestConfig("")
		config.Region = region
		s, err := NewSES(config)
		if err != nil {
			t.Fatal(err)
		}
		want := "https://email." + region + ".amazonaws.com"
		if strings.HasPrefix(region, "cn-") {
			want += ".cn"
		}
		if s.http.BaseURL() != want {
			t.Errorf("endpoint = %q, want %q", s.http.BaseURL(), want)
		}
	}
	config := sesTestConfig("")
	config.AccessKeyID = ""
	if _, err := NewSES(config); err == nil {
		t.Error("accepted empty access key")
	}
	config = sesTestConfig("")
	config.SecretAccessKey = ""
	if _, err := NewSES(config); err == nil {
		t.Error("accepted empty secret")
	}
}
