// Package provider defines the shared delivery configuration. Applications use package pigeon.
package provider

import "net/http"

// Kind selects the delivery backend when constructing a mailer.
type Kind string

const (
	SES      Kind = "ses"
	Resend   Kind = "resend"
	Mailgun  Kind = "mailgun"
	SendGrid Kind = "sendgrid"
	Local    Kind = "local"
)

// Config configures a mailer once. Only fields relevant to Provider are read.
// Credentials are explicit snapshots; Pigeon does not read environment variables
// or refresh temporary AWS credentials. Keep Config out of logs.
type Config struct {
	Provider Kind
	// APIKey authenticates Resend, Mailgun or SendGrid.
	APIKey string
	// Domain is the verified Mailgun sending domain.
	Domain string
	// Region and access keys authenticate SES. SessionToken is optional.
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// BaseURL overrides the provider's origin, for example Mailgun's EU API.
	// Paths, query strings, fragments and user information are not accepted.
	// HTTPS is required except for literal loopback hosts used in local tests.
	BaseURL string
	// HTTPClient is optional. New copies it, shares its Transport, and disables
	// redirects to prevent credential forwarding and accidental repeat sends.
	// A zero Timeout becomes 30 seconds. Do not mutate the Transport while used.
	HTTPClient *http.Client
	// LocalCapacity bounds retained emails. Zero selects 100; negative is invalid.
	// The local inbox also limits retained message data to 16 MiB.
	LocalCapacity int
}
