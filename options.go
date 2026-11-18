package pigeon

import (
	"errors"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/provider"
)

// Option configures a Mailer in New. Provider-specific options (WithSES,
// WithResend, WithMailgun, WithSendGrid, WithLocal) must match the Provider
// passed to New. When an option is repeated, the last one wins.
// Options capture their arguments; keep them out of logs, as they hold
// credentials.
type Option func(*options)

type options struct {
	config provider.Config
	// targets lists the provider of every provider-specific option applied.
	targets []provider.Kind
	// usesHTTP reports whether WithBaseURL or WithHTTPClient was applied.
	usesHTTP bool
}

func (o *options) target(kind provider.Kind) {
	o.targets = append(o.targets, kind)
}

// AWSCredentials is a fixed snapshot of AWS credentials for SES. SessionToken
// is optional. Pigeon does not discover or refresh credentials.
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// LocalLimits bounds the local development inbox. A zero Capacity selects 100
// messages and a zero MaxBytes selects 16 MiB of retained content; negative
// values are invalid. The oldest messages are evicted first.
type LocalLimits struct {
	Capacity int
	MaxBytes int
}

// WithSES configures ProviderSES with an SES v2 region and credentials.
// It is required for ProviderSES.
func WithSES(region string, credentials AWSCredentials) Option {
	return func(o *options) {
		o.target(provider.SES)
		o.config.Region = region
		o.config.AccessKeyID = credentials.AccessKeyID
		o.config.SecretAccessKey = credentials.SecretAccessKey
		o.config.SessionToken = credentials.SessionToken
	}
}

// WithResend configures ProviderResend with its API key. It is required for
// ProviderResend.
func WithResend(apiKey string) Option {
	return func(o *options) {
		o.target(provider.Resend)
		o.config.APIKey = apiKey
	}
}

// WithMailgun configures ProviderMailgun with its API key and verified sending
// domain. It is required for ProviderMailgun.
func WithMailgun(apiKey, domain string) Option {
	return func(o *options) {
		o.target(provider.Mailgun)
		o.config.APIKey = apiKey
		o.config.Domain = domain
	}
}

// WithSendGrid configures ProviderSendGrid with its API key. It is required for
// ProviderSendGrid.
func WithSendGrid(apiKey string) Option {
	return func(o *options) {
		o.target(provider.SendGrid)
		o.config.APIKey = apiKey
	}
}

// WithLocal sets the retention limits of ProviderLocal. It is optional; without
// it the inbox uses the LocalLimits defaults.
func WithLocal(limits LocalLimits) Option {
	return func(o *options) {
		o.target(provider.Local)
		o.config.LocalCapacity = limits.Capacity
		o.config.LocalMaxBytes = limits.MaxBytes
	}
}

// WithBaseURL overrides a cloud provider's origin, for example Mailgun's EU API.
// Paths, query strings, fragments and user information are not accepted.
// HTTPS is required except for literal loopback hosts used in local tests.
// ProviderLocal rejects it.
func WithBaseURL(baseURL string) Option {
	return func(o *options) {
		o.usesHTTP = true
		o.config.BaseURL = baseURL
	}
}

// WithHTTPClient sets the HTTP client of a cloud provider. New copies it,
// shares its Transport, and disables redirects to prevent credential forwarding
// and accidental repeat sends. A zero Timeout becomes 30 seconds; a negative
// Timeout is invalid. Do not mutate the Transport while the Mailer is in use.
// ProviderLocal rejects it.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.usesHTTP = true
		o.config.HTTPClient = client
	}
}

// WithMaxMessageBytes bounds the combined bytes of all email strings before
// encoding. Without it, or with zero, the limit is 1 MiB. Cloud providers cap
// larger values at their published limit. Negative values are invalid.
func WithMaxMessageBytes(n int) Option {
	return func(o *options) {
		o.config.MaxMessageBytes = n
	}
}

// resolveOptions applies opts for kind and rejects an unknown kind, options
// that do not apply to it, a missing required provider option, and nil options.
func resolveOptions(kind Provider, opts []Option) (*provider.Config, error) {
	switch kind {
	case ProviderSES, ProviderResend, ProviderMailgun, ProviderSendGrid, ProviderLocal:
	default:
		return nil, fmt.Errorf("pigeon: unknown provider %q", kind)
	}
	o := options{config: provider.Config{Provider: kind}}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("pigeon: option is nil")
		}
		opt(&o)
	}
	configured := false
	for _, target := range o.targets {
		if target != kind {
			return nil, fmt.Errorf("pigeon: %s option does not apply to provider %q", target, kind)
		}
		configured = true
	}
	if kind == ProviderLocal {
		if o.usesHTTP {
			return nil, errors.New("pigeon: local provider does not accept WithBaseURL or WithHTTPClient")
		}
	} else if !configured {
		return nil, fmt.Errorf("pigeon: provider %q requires its provider option", kind)
	}
	return &o.config, nil
}
