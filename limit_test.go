package pigeon_test

import (
	"math"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon"
)

func limitTestMailer(t *testing.T, maxBytes int) *pigeon.Mailer {
	t.Helper()
	mailer, err := pigeon.New(&pigeon.Config{
		Provider:        pigeon.ProviderLocal,
		MaxMessageBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mailer
}

func validEmail(body string) *pigeon.Email {
	return &pigeon.Email{
		From:    pigeon.Address{Email: "sender@example.com"},
		To:      []pigeon.Address{{Email: "reader@example.com"}},
		Subject: "Limit",
		Text:    body,
	}
}

func TestSendRejectsNegativeMaxMessageBytes(t *testing.T) {
	for _, provider := range []pigeon.Provider{
		pigeon.ProviderLocal, pigeon.ProviderMailgun, pigeon.ProviderSendGrid,
		pigeon.ProviderSES, pigeon.ProviderResend,
	} {
		config := &pigeon.Config{Provider: provider, MaxMessageBytes: -1}
		switch provider {
		case pigeon.ProviderMailgun:
			config.APIKey, config.Domain = "key", "example.com"
		case pigeon.ProviderSendGrid, pigeon.ProviderResend:
			config.APIKey = "key"
		case pigeon.ProviderSES:
			config.Region, config.AccessKeyID, config.SecretAccessKey = "us-east-1", "id", "secret"
		}
		if _, err := pigeon.New(config); err == nil {
			t.Errorf("provider %s accepted a negative MaxMessageBytes", provider)
		}
	}
}

func TestSendEnforcesDefaultOneMebibyte(t *testing.T) {
	mailer := limitTestMailer(t, 0)
	const oneMiB = 1 << 20
	oversized := validEmail(strings.Repeat("a", oneMiB))
	if _, err := mailer.Send(t.Context(), oversized); err == nil {
		t.Fatal("a message over the 1 MiB default was accepted")
	}
	envelopeBytes := len(oversized.Subject) + len(oversized.From.Email) + len(oversized.To[0].Email)
	undersized := validEmail(oversized.Text[:oneMiB-envelopeBytes])
	if _, err := mailer.Send(t.Context(), undersized); err != nil {
		t.Fatalf("a message within the 1 MiB default failed: %v", err)
	}
}

func TestSendHonorsConfiguredLimitBoundary(t *testing.T) {
	const body = "test message"
	limit := len(validEmail(body).Subject) + len(body) +
		len("sender@example.com") + len("reader@example.com")
	mailer := limitTestMailer(t, limit)
	if _, err := mailer.Send(t.Context(), validEmail(body)); err != nil {
		t.Fatalf("a message exactly at the limit failed: %v", err)
	}
	tight := limitTestMailer(t, limit-1)
	if _, err := tight.Send(t.Context(), validEmail(body)); err == nil {
		t.Fatal("a message one byte over the configured limit was accepted")
	}
}

func TestSendLocalConfiguredLimitNotProviderCapped(t *testing.T) {
	// The local provider has no published cap, so a large configured limit stays unchanged.
	if _, err := pigeon.New(&pigeon.Config{
		Provider:        pigeon.ProviderLocal,
		MaxMessageBytes: math.MaxInt,
	}); err != nil {
		t.Fatalf("local New with a large limit: %v", err)
	}
}
