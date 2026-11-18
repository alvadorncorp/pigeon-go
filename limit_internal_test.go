package pigeon

import (
	"math"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
)

func TestResolveMessageBytesDefault(t *testing.T) {
	for _, provider := range []Provider{ProviderLocal, ProviderMailgun, ProviderSendGrid, ProviderSES, ProviderResend} {
		limit, err := resolveMessageBytes(provider, 0)
		if err != nil || limit != defaultMessageBytes {
			t.Errorf("provider %s zero limit = %d, %v; want %d, nil", provider, limit, err, defaultMessageBytes)
		}
	}
}

func TestResolveMessageBytesNegative(t *testing.T) {
	for _, provider := range []Provider{ProviderLocal, ProviderMailgun, ProviderSendGrid, ProviderSES, ProviderResend} {
		if _, err := resolveMessageBytes(provider, -1); err == nil {
			t.Errorf("provider %s accepted a negative limit", provider)
		}
	}
}

func TestResolveMessageBytesProviderCaps(t *testing.T) {
	const (
		mailgunPublishedMax  = 25_000_000
		sendGridExclusiveMax = 30_000_000
		sesPublishedMax      = 40_000_000
		resendPublishedMax   = 40_000_000
	)
	cases := []struct {
		provider Provider
		cap      int
	}{
		{ProviderMailgun, mailgunPublishedMax},
		{ProviderSendGrid, sendGridExclusiveMax - 1},
		{ProviderSES, sesPublishedMax},
		{ProviderResend, resendPublishedMax},
	}
	for _, tc := range cases {
		for _, configured := range []int{tc.cap, tc.cap + 1, math.MaxInt} {
			limit, err := resolveMessageBytes(tc.provider, configured)
			if err != nil || limit != tc.cap {
				t.Errorf("provider %s configured %d limit = %d, %v; want %d, nil", tc.provider, configured, limit, err, tc.cap)
			}
		}
		if limit, err := resolveMessageBytes(tc.provider, tc.cap-1); err != nil || limit != tc.cap-1 {
			t.Errorf("provider %s below-cap limit = %d, %v; want unchanged", tc.provider, limit, err)
		}
	}
}

func TestResolveMessageBytesLocalUncapped(t *testing.T) {
	limit, err := resolveMessageBytes(ProviderLocal, math.MaxInt)
	if err != nil || limit != math.MaxInt {
		t.Errorf("local limit = %d, %v; want configured value unchanged", limit, err)
	}
}

func TestMessageSizeCountsAllStrings(t *testing.T) {
	email := &message.Email{
		From:    message.Address{Name: "Sender", Email: "sender@example.com"},
		To:      []message.Address{{Email: "a@example.com"}, {Name: "Bee", Email: "b@example.com"}},
		Cc:      []message.Address{{Email: "c@example.com"}},
		Bcc:     []message.Address{{Email: "d@example.com"}},
		ReplyTo: message.Address{Name: "Reply", Email: "reply@example.com"},
		Subject: "Hello",
		Text:    "text",
		HTML:    "<p>html</p>",
	}
	var want int
	add := func(s string) { want += len(s) }
	address := func(a message.Address) { add(a.Name); add(a.Email) }
	address(email.From)
	address(email.ReplyTo)
	add(email.Subject)
	add(email.Text)
	add(email.HTML)
	for _, group := range [][]message.Address{email.To, email.Cc, email.Bcc} {
		for _, a := range group {
			address(a)
		}
	}
	if got := message.Size(email); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
}

func TestMessageSizeBoundary(t *testing.T) {
	email := &message.Email{
		From:    message.Address{Email: "sender@example.com"},
		To:      []message.Address{{Email: "reader@example.com"}},
		Subject: "Boundary",
		Text:    "body",
	}
	size := message.Size(email)
	email.Text += "a"
	if got := message.Size(email); got != size+1 {
		t.Errorf("Size grew by %d, want 1", got-size)
	}
}
