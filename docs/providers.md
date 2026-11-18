# Providers

Only configuration changes between providers. Credentials are supplied by your application; the library does not read environment variables.

| Provider | Required configuration | Optional configuration |
| --- | --- | --- |
| `ProviderLocal` | None | `LocalCapacity` |
| `ProviderSES` | `Region`, `AccessKeyID`, `SecretAccessKey` | `SessionToken`, `BaseURL`, `HTTPClient` |
| `ProviderResend` | `APIKey` | `BaseURL`, `HTTPClient` |
| `ProviderMailgun` | `APIKey`, `Domain` | `BaseURL`, `HTTPClient` |
| `ProviderSendGrid` | `APIKey` | `BaseURL`, `HTTPClient` |

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

SendGrid may return an empty receipt ID if its acceptance response has no `X-Message-Id` header.

`New` now takes `*pigeon.Config`: migrate calls such as `pigeon.New(pigeon.Config{...})` to
`pigeon.New(&pigeon.Config{...})`. A nil config is rejected. Configuration is read synchronously
and snapshotted during `New`; later changes to the config do not reconfigure the mailer.
