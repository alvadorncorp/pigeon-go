# Providers

Only configuration changes between providers. Credentials are supplied by your application; the library does not read environment variables.

| Provider | Required configuration | Optional configuration |
| --- | --- | --- |
| `ProviderLocal` | None | `LocalCapacity`, `LocalMaxBytes` |
| `ProviderSES` | `Region`, `AccessKeyID`, `SecretAccessKey` | `SessionToken`, `BaseURL`, `HTTPClient` |
| `ProviderResend` | `APIKey` | `BaseURL`, `HTTPClient` |
| `ProviderMailgun` | `APIKey`, `Domain` | `BaseURL`, `HTTPClient` |
| `ProviderSendGrid` | `APIKey` | `BaseURL`, `HTTPClient` |

All providers accept `MaxMessageBytes`; see the [message contract](message-contract.md).

For example, Resend configuration can come from your application's environment:

```go
mailer, err := pigeon.New(&pigeon.Config{
    Provider: pigeon.ProviderResend,
    APIKey:   os.Getenv("RESEND_API_KEY"),
})
```

Use a verified sender and a key permitted to send email. SES sandbox restrictions and provider quotas still apply. SES credentials are a snapshot: the library does not discover IAM roles or refresh expiring session credentials. Create a new mailer when those credentials change.

Mailgun uses its US endpoint by default. For an EU domain, set `BaseURL: "https://api.eu.mailgun.net"`. SendGrid EU accounts can use `https://api.eu.sendgrid.com`. A base URL is an origin, without a path, query, fragment, or embedded credentials. HTTPS is required; literal loopback HTTP addresses are allowed for local tests.

An empty or unrecognized provider returns an error. Pigeon never silently substitutes local delivery for a missing cloud configuration.

Concurrent sends share one HTTP client. The default transport keeps only two idle connections per host, so a burst of parallel sends opens a fresh TLS connection for every request past the second. If you send in bursts, pass an `HTTPClient` whose `Transport` sets `MaxIdleConnsPerHost` to your expected concurrency.

SendGrid may return an empty receipt ID if its acceptance response has no `X-Message-Id` header.

`New` requires a non-nil `*Config` and reads it once: later changes to the config do not reconfigure the mailer.
