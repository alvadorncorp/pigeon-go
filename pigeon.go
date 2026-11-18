package pigeon

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/aws"
	"go.alvadorn.org/pigeon/internal/provider/local"
	"go.alvadorn.org/pigeon/internal/provider/mailgun"
	"go.alvadorn.org/pigeon/internal/provider/resend"
	"go.alvadorn.org/pigeon/internal/provider/sendgrid"
)

// Address is an addr-spec and optional display name.
type Address = message.Address

// Email contains the provider-independent message and its recipients.
type Email = message.Email

// Receipt identifies provider acceptance, not final delivery. A nil error from
// Send always means acceptance; ID may be empty when the provider's acceptance
// response carries no usable identifier, for any provider.
type Receipt = message.Receipt

// DeliveryError reports an unsuccessful provider HTTP response.
// Use errors.AsType[*DeliveryError] to inspect StatusCode. Message and Code
// are untrusted diagnostics; Error() omits them to avoid accidental disclosure.
type DeliveryError = message.DeliveryError

// Provider selects the adapter when constructing a Mailer.
type Provider string

// The supported providers. ProviderLocal keeps messages in memory for development.
const (
	ProviderSES      Provider = "ses"
	ProviderResend   Provider = "resend"
	ProviderMailgun  Provider = "mailgun"
	ProviderSendGrid Provider = "sendgrid"
	ProviderLocal    Provider = "local"
)

type sender interface {
	Send(context.Context, *message.Email) (message.Receipt, error)
}

// defaultMessageBytes is the per-message limit when WithMaxMessageBytes is unset.
const defaultMessageBytes = 1 << 20

// backends maps each provider to its constructor and published per-message
// byte cap. A zero cap means none: the local inbox enforces its own budget.
var backends = map[Provider]struct {
	maxBytes int
	build    func(*provider.Config) (sender, error)
}{
	ProviderSES:      {40_000_000, func(c *provider.Config) (sender, error) { return aws.New(c) }},
	ProviderResend:   {40_000_000, func(c *provider.Config) (sender, error) { return resend.New(c) }},
	ProviderMailgun:  {25_000_000, func(c *provider.Config) (sender, error) { return mailgun.New(c) }},
	ProviderSendGrid: {29_999_999, func(c *provider.Config) (sender, error) { return sendgrid.New(c) }},
	ProviderLocal:    {0, func(c *provider.Config) (sender, error) { return local.New(c) }},
}

// resolveMessageBytes applies the documented semantics of WithMaxMessageBytes:
// zero selects 1 MiB, a negative value is invalid, and cloud providers cap
// larger values at their published limit.
func resolveMessageBytes(kind Provider, configured int) (int, error) {
	if configured < 0 {
		return 0, errors.New("pigeon: max message bytes must not be negative")
	}
	if configured == 0 {
		return defaultMessageBytes, nil
	}
	if limit := backends[kind].maxBytes; limit > 0 {
		return min(configured, limit), nil
	}
	return configured, nil
}

// Mailer sends email through one configured provider. It is safe for concurrent
// use. Construct it with New; the zero value cannot send.
type Mailer struct {
	sender   sender
	inbox    http.Handler
	maxBytes int
}

// New validates the options for provider and creates a reusable mailer without
// network I/O. An empty or unknown provider is an error; it never falls back to
// local delivery. Cloud providers require their provider option (WithSES,
// WithResend, WithMailgun or WithSendGrid); provider options for another
// provider, nil options, and HTTP options with ProviderLocal are rejected.
// Pigeon never reads environment variables.
func New(kind Provider, opts ...Option) (*Mailer, error) {
	o, err := resolveOptions(kind, opts)
	if err != nil {
		return nil, err
	}
	maxBytes, err := resolveMessageBytes(kind, o.maxMessageBytes)
	if err != nil {
		return nil, err
	}
	backend, err := backends[kind].build(&o.config)
	if err != nil {
		return nil, err
	}
	inbox, ok := backend.(http.Handler)
	if !ok {
		inbox = http.NotFoundHandler()
	}
	return &Mailer{sender: backend, inbox: inbox, maxBytes: maxBytes}, nil
}

// Send validates and synchronously submits one email, respecting ctx.
// It rejects messages whose combined string bytes exceed the limit resolved
// from WithMaxMessageBytes.
// It makes no automatic retries: a timeout can mean the provider accepted the
// message but its response was lost. Retrying can send a duplicate.
// The caller must not mutate email or its slices until Send returns. Afterward,
// the caller may reuse them; the local adapter retains its own snapshot.
func (m *Mailer) Send(ctx context.Context, email *Email) (Receipt, error) {
	if m == nil || m.sender == nil {
		return Receipt{}, errors.New("pigeon: mailer is not initialized")
	}
	if ctx == nil {
		return Receipt{}, errors.New("pigeon: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if err := message.Validate(email); err != nil {
		return Receipt{}, err
	}
	if size := message.Size(email); size > m.maxBytes {
		return Receipt{}, fmt.Errorf(
			"pigeon: message size %d bytes exceeds the configured limit of %d bytes",
			size,
			m.maxBytes,
		)
	}
	return m.sender.Send(ctx, email)
}

// Inbox returns the local mailbox's read-only HTTP handler. For a cloud
// provider it returns a handler that answers 404, never nil. Mount it on a
// loopback-only development server; it rejects hosts other than IP literals
// and localhost names. It has no authentication and can display private email,
// including Bcc and reset links. The handler shares memory with this Mailer;
// it does not receive SMTP or messages from another process. Pigeon never
// starts an HTTP server itself.
func (m *Mailer) Inbox() http.Handler {
	if m == nil || m.inbox == nil {
		return http.NotFoundHandler()
	}
	return m.inbox
}
