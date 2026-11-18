package pigeon_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.alvadorn.org/pigeon"
)

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		kind pigeon.Provider
		opts []pigeon.Option
	}{
		{
			name: "nil option",
			kind: pigeon.ProviderResend,
			opts: []pigeon.Option{pigeon.WithResend("key"), nil},
		},
		{
			name: "provider option for another provider",
			kind: pigeon.ProviderSES,
			opts: []pigeon.Option{
				pigeon.WithSES("us-east-1", pigeon.AWSCredentials{AccessKeyID: "id", SecretAccessKey: "secret"}),
				pigeon.WithMailgun("key", "mg.example.com"),
			},
		},
		{
			name: "local with another provider's option",
			kind: pigeon.ProviderLocal,
			opts: []pigeon.Option{pigeon.WithResend("key")},
		},
		{
			name: "SES without its provider option",
			kind: pigeon.ProviderSES,
		},
		{
			name: "Resend without its provider option",
			kind: pigeon.ProviderResend,
		},
		{
			name: "Mailgun without its provider option",
			kind: pigeon.ProviderMailgun,
		},
		{
			name: "SendGrid without its provider option",
			kind: pigeon.ProviderSendGrid,
		},
		{
			name: "local with WithBaseURL",
			kind: pigeon.ProviderLocal,
			opts: []pigeon.Option{pigeon.WithBaseURL("https://127.0.0.1:9000")},
		},
		{
			name: "local with WithHTTPClient",
			kind: pigeon.ProviderLocal,
			opts: []pigeon.Option{pigeon.WithHTTPClient(&http.Client{})},
		},
		{
			name: "local with negative capacity",
			kind: pigeon.ProviderLocal,
			opts: []pigeon.Option{pigeon.WithLocal(pigeon.LocalLimits{Capacity: -1})},
		},
		{
			name: "local with negative MaxBytes",
			kind: pigeon.ProviderLocal,
			opts: []pigeon.Option{pigeon.WithLocal(pigeon.LocalLimits{MaxBytes: -1})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pigeon.New(tc.kind, tc.opts...); err == nil {
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestNewLastRepeatedOptionWins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer new-key" {
			t.Errorf("authorization = %q, want credentials from the last option", got)
		}
		_, _ = io.WriteString(w, `{"id":"last-wins"}`)
	}))
	defer server.Close()
	mailer, err := pigeon.New(
		pigeon.ProviderResend,
		pigeon.WithResend("old-key"),
		pigeon.WithResend("new-key"),
		pigeon.WithBaseURL(server.URL),
	)
	if err != nil {
		t.Fatal(err)
	}
	email := &pigeon.Email{
		From:    pigeon.Address{Email: "sender@example.com"},
		To:      []pigeon.Address{{Email: "reader@example.com"}},
		Subject: "Last option wins",
		Text:    "A repeated option applies its final value.",
	}
	receipt, sendErr := mailer.Send(t.Context(), email)
	if sendErr != nil || receipt.ID != "last-wins" {
		t.Fatalf("receipt = %+v, error = %v", receipt, sendErr)
	}
}
