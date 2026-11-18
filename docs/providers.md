# Providers

Only configuration changes between providers. Credentials are supplied by your application; the library does not read environment variables.

| Provider | Required option | Optional options |
| --- | --- | --- |
| `ProviderLocal` | None | `WithLocal(LocalLimits{Capacity, MaxBytes})` |
| `ProviderSES` | `WithSES(region, AWSCredentials{AccessKeyID, SecretAccessKey})` | `WithBaseURL`, `WithHTTPClient` |
| `ProviderResend` | `WithResend(apiKey)` | `WithBaseURL`, `WithHTTPClient` |
| `ProviderMailgun` | `WithMailgun(apiKey, domain)` | `WithBaseURL`, `WithHTTPClient` |
| `ProviderSendGrid` | `WithSendGrid(apiKey)` | `WithBaseURL`, `WithHTTPClient` |

All providers accept `WithMaxMessageBytes`; see the [message contract](message-contract.md).

For example, Resend configuration can come from your application's environment:

```go
mailer, err := pigeon.New(pigeon.ProviderResend,
    pigeon.WithResend(os.Getenv("RESEND_API_KEY")),
)
```

Use a verified sender and a key permitted to send email. SES sandbox restrictions and provider quotas still apply. `AWSCredentials.SessionToken` is optional. SES credentials are a snapshot: the library does not discover IAM roles or refresh expiring session credentials. Create a new mailer when those credentials change.

Mailgun uses its US endpoint by default. For an EU domain, pass `WithBaseURL("https://api.eu.mailgun.net")`. SendGrid EU accounts can pass `WithBaseURL("https://api.eu.sendgrid.com")`. A base URL is an origin, without a path, query, fragment, or embedded credentials. HTTPS is required; literal loopback HTTP addresses are allowed for local tests.

An empty or unrecognized provider returns an error. Pigeon never silently substitutes local delivery for a missing cloud configuration.

`New` validates the options for the provider: a provider option passed for another provider, a cloud provider missing its own option, a nil option, and `WithBaseURL` or `WithHTTPClient` with `ProviderLocal` are all errors. Repeating an option is not an error; the last one wins.

Concurrent sends share one HTTP client. The default transport keeps only two idle connections per host, so a burst of parallel sends opens a fresh TLS connection for every request past the second. If you send in bursts, pass `WithHTTPClient` with a client whose `Transport` sets `MaxIdleConnsPerHost` to your expected concurrency.

Any provider may return an empty receipt ID when its acceptance response omits or garbles its message identifier, for example an unreadable body or a missing ID field. That is still a successful send: a nil error always means the message was accepted, and an empty ID is never a reason to retry.

Options capture their arguments when `New` runs; changing them afterwards, such as credentials or the `http.Client`, does not reconfigure the mailer. Create a new one.
