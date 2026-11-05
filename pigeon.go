package pigeon

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/local"
)

// Address is an addr-spec and optional display name.
type Address = message.Address

// Email contains the provider-independent message and its recipients.
type Email = message.Email

// Receipt identifies provider acceptance, not final delivery.
type Receipt = message.Receipt

// DeliveryError reports an unsuccessful provider HTTP response.
// Use errors.AsType[*DeliveryError] to inspect StatusCode. Message and Code
// are untrusted diagnostics; Error() omits them to avoid accidental disclosure.
type DeliveryError = message.DeliveryError

// Provider selects the adapter when constructing a Mailer.
type Provider = provider.Kind

const (
	ProviderSES      Provider = provider.SES
	ProviderResend   Provider = provider.Resend
	ProviderMailgun  Provider = provider.Mailgun
	ProviderSendGrid Provider = provider.SendGrid
	ProviderLocal    Provider = provider.Local
)

// Config holds the provider selection and credentials. See its fields for
// provider-specific requirements. Pigeon never reads environment variables.
type Config = provider.Config

type sender interface {
	Send(context.Context, *message.Email) (message.Receipt, error)
}

// Mailer sends email through one configured provider. It is safe for concurrent
// use. Construct it with New; the zero value cannot send.
type Mailer struct {
	sender sender
	inbox  http.Handler
}

// New validates configuration and creates a reusable mailer without network I/O.
// An empty or unknown Provider is an error; it never falls back to local delivery.
func New(config Config) (*Mailer, error) {
	var backend sender
	var err error
	switch config.Provider {
	case ProviderSES:
		backend, err = provider.NewSES(config)
	case ProviderResend:
		backend, err = provider.NewResend(config)
	case ProviderMailgun:
		backend, err = provider.NewMailgun(config)
	case ProviderSendGrid:
		backend, err = provider.NewSendGrid(config)
	case ProviderLocal:
		inbox, err := local.New(config)
		if err != nil {
			return nil, err
		}
		return &Mailer{sender: inbox, inbox: inbox}, nil
	default:
		return nil, fmt.Errorf("pigeon: unknown provider %q", config.Provider)
	}
	if err != nil {
		return nil, err
	}
	return &Mailer{sender: backend}, nil
}

// Send validates and synchronously submits one email, respecting ctx.
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
	return m.sender.Send(ctx, email)
}

// Inbox returns the local mailbox's read-only HTTP handler, or nil for a cloud
// provider. Mount it on a loopback-only development server. It has no
// authentication and can display private email, including Bcc and reset links.
// The handler shares memory with this Mailer; it does not receive SMTP or
// messages from another process. Pigeon never starts an HTTP server itself.
func (m *Mailer) Inbox() http.Handler {
	if m == nil {
		return nil
	}
	return m.inbox
}
