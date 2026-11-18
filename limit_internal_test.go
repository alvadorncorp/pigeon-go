package pigeon

import (
	"math"
	"testing"
)

func TestResolveMessageBytesDefault(t *testing.T) {
	for _, provider := range []Provider{ProviderLocal, ProviderMailgun, ProviderSendGrid, ProviderSES, ProviderResend} {
		limit, err := resolveMessageBytes(provider, 0)
		if err != nil || limit != defaultMessageBytes {
			t.Errorf("provider %s zero limit = %d, %v; want %d, nil", provider, limit, err, defaultMessageBytes)
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
				t.Errorf(
					"provider %s configured %d limit = %d, %v; want %d, nil",
					tc.provider,
					configured,
					limit,
					err,
					tc.cap,
				)
			}
		}
		if limit, err := resolveMessageBytes(tc.provider, tc.cap-1); err != nil || limit != tc.cap-1 {
			t.Errorf("provider %s below-cap limit = %d, %v; want unchanged", tc.provider, limit, err)
		}
	}
}

func TestResolveMessageBytesLocalUncapped(t *testing.T) {
	if limit, err := resolveMessageBytes(ProviderLocal, math.MaxInt); err != nil || limit != math.MaxInt {
		t.Errorf("local limit = %d, %v; want configured value unchanged", limit, err)
	}
}
