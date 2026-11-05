<p align="center">
  <img src="docs/assets/pigeon.svg" width="240" alt="Pigeon, a cheerful carrier pigeon holding a letter">
</p>

# Pigeon

Pigeon sends email from Go applications. You build a message, choose a provider in configuration, and call `Send`. Switching from a local inbox to Amazon SES, Resend, Mailgun, or Twilio SendGrid leaves your message-building code alone.

The idea comes from [Swoosh](https://hexdocs.pm/swoosh/Swoosh.html), the Elixir email library. Pigeon brings that separation between messages and delivery adapters to Go, with a small API and no dependencies outside the standard library. It calls each provider's HTTP API directly, including signing SES requests with AWS Signature Version 4.

For development, the local adapter keeps messages in memory and gives you a web inbox rendered with Go templates. You can inspect an email without delivering it to anyone.

Requires **Go 1.27 or newer**. The module path is `go.alvadorn.org/pigeon`.

## Send a message

Create the mailer once and reuse it. This example uses local delivery, so it needs no credentials and sends nothing over the network.

```go
package main

import (
    "context"
    "log"

    "go.alvadorn.org/pigeon"
)

func main() {
    mailer, err := pigeon.New(pigeon.Config{
        Provider: pigeon.ProviderLocal,
    })
    if err != nil {
        log.Fatal(err)
    }

    receipt, err := mailer.Send(context.Background(), &pigeon.Email{
        From:    pigeon.Address{Name: "Your app", Email: "hello@example.com"},
        To:      []pigeon.Address{{Name: "Ada", Email: "ada@example.com"}},
        Subject: "Your receipt",
        Text:    "Thanks, Ada. Your order is confirmed.",
        HTML:    "<p>Thanks, Ada. Your order is confirmed.</p>",
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Accepted: %s", receipt.ID)
}
```

A receipt means the provider accepted the request. Delivery, bounces, spam filtering, and account restrictions still belong to the provider. SendGrid may return an empty receipt ID if its acceptance response has no `X-Message-Id` header.

## Choose a provider

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
mailer, err := pigeon.New(pigeon.Config{
    Provider: pigeon.ProviderResend,
    APIKey:   os.Getenv("RESEND_API_KEY"),
})
```

Use a verified sender and a key permitted to send email. SES sandbox restrictions and provider quotas still apply. SES credentials are a snapshot: the library does not discover IAM roles or refresh expiring session credentials. Create a new mailer when those credentials change.

Mailgun uses its US endpoint by default. For an EU domain, set `BaseURL: "https://api.eu.mailgun.net"`. SendGrid EU accounts can use `https://api.eu.sendgrid.com`. A base URL is an origin, without a path, query, fragment, or embedded credentials. HTTPS is required; literal loopback HTTP addresses are allowed for local tests.

An empty or unrecognized provider returns an error. Pigeon never silently substitutes local delivery for a missing cloud configuration.

## Open the local inbox

```sh
go run ./examples/local
```

Open [127.0.0.1:8025](http://127.0.0.1:8025). The example submits two fictional messages through the same `Send` API used by cloud providers.

The inbox shows messages in a horizontal queue. Select a message or use the previous and next controls to move through it; HTML and Text views are offered when those formats are available. On mobile, use the message chooser to select an email and the native expandable envelope to inspect its addressing details.

The interface and its assets are embedded in the Go binary; no frontend build is required.

To add the inbox to an existing development server:

```go
mailer, err := pigeon.New(pigeon.Config{Provider: pigeon.ProviderLocal})
if err != nil {
    log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/mail/", http.StripPrefix("/mail", mailer.Inbox()))
```

`Inbox()` returns a read-only `http.Handler` for a local mailer and `nil` for a cloud mailer. The library does not start a server or receive SMTP. The handler shows the real messages sent to its own mailer instance in the same process. Message URLs can be opened directly by ID (for example, `/mail/<id>` with the mount above); `http.StripPrefix` keeps inbox links and previews working under the chosen prefix.

The inbox retains up to 100 messages by default and at most 16 MiB of message data, evicting the oldest messages when either limit is reached. Set `LocalCapacity` to change the message count. A single message larger than the byte limit is rejected. Data disappears when the process stops.

**Keep the inbox on a trusted development server.** It has no authentication and displays email contents, recipients, Bcc, and any private links inside messages. The example binds only to loopback. HTML previews run in a sandbox with remote resources blocked, so they can look different from a real mail client.

HTML bodies are available only inside the inbox's protected iframe. Opening a raw preview URL directly returns `403`. The preview requires a browser that sends Fetch Metadata headers (`Sec-Fetch-Dest`).

## Message contract

`Email` supports `From`, `To`, `Cc`, `Bcc`, one `ReplyTo`, `Subject`, `Text`, and `HTML`. It requires at least one `To` recipient, a nonempty subject, and a text or HTML body. The combined recipient limit is 50, keeping the same message within SES's recipient limit when switching providers.

Put the raw ASCII address in `Address.Email` and the optional display name in `Address.Name`. Display names, subjects, and bodies may contain Unicode. International domain names must already be in ASCII form. Header control characters and malformed addresses are rejected before a request is sent.

A mailer is safe to share across goroutines. Do not mutate a message or its recipient slices during `Send`. After it returns, those slices can be reused: the local adapter keeps its own snapshot, while cloud adapters retain no reference to the message.

Attachments, custom headers, provider-hosted templates, batch APIs, and delivery webhooks are outside the current API. Pigeon does not manage queues or automatic retries. An application can retry a failed submission, but a timeout may happen after the provider has accepted it, so retrying can produce duplicate emails.

## Errors and timeouts

Pass a context to bound an individual send. The default HTTP timeout is 30 seconds. You can supply an `HTTPClient`; Pigeon copies the client, shares its transport, and disables redirects. A zero client timeout gets the same 30-second default.

Provider HTTP failures support typed inspection:

```go
if delivery, ok := errors.AsType[*pigeon.DeliveryError](err); ok {
    log.Printf("Email provider returned HTTP %d", delivery.StatusCode)
}
```

`DeliveryError.Error()` excludes provider response text. The `Message` and `Code` fields retain diagnostic details when available, and may contain personal data; review them before logging. Context and transport errors remain discoverable with `errors.Is` and `errors.AsType`. Responses are limited to 64 KiB.

## Allocation work

Pigeon reuses its configured HTTP client, sends one serialized payload per request, and uses concrete message values rather than maps. SES signs the exact bytes it sends and reuses its derived signing key until the UTC date changes. Common ASCII addresses have an allocation-free validation path, with `net/mail` handling other supported forms. Mailgun writes field contents directly into its multipart buffer without an intermediate byte copy. Local delivery copies recipient slices because callers may reuse them after sending.

The benchmark suite reports allocations as well as time. `BenchmarkSend` exercises the public API with an in-process HTTP transport, including validation and response decoding but excluding sockets and a remote server. Adapter-specific HTTP benchmarks include a local test server and should not be compared directly with those numbers. Measure with the workload you expect to send; HTTP transport, JSON encoding, signatures, and retaining local messages all have costs.

```sh
go test ./... -run '^$' -bench . -benchmem
```

These measurements used Go 1.27.3 on an Apple M2 Max, one ASCII sender and recipient, and a 4 KiB plain-text body. They compare the first working implementation with the measured changes described above, using `BenchmarkSend` and `-benchtime=100ms -count=1`. They exclude provider network latency; byte counts can vary slightly between runs.

| Provider | Before, B/op | After, B/op | Before, allocs/op | After, allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Local | 230 | 39 | 11 | 1 |
| Amazon SES | 13,525 | 11,184 | 108 | 69 |
| Resend | 9,295 | 9,094 | 58 | 48 |
| Mailgun | 17,052 | 12,670 | 120 | 106 |
| Twilio SendGrid | 9,276 | 9,078 | 57 | 47 |

## Examples and development

- [`examples/local`](examples/local): a running inbox with text and HTML messages.
- [`examples/send`](examples/send): one submission, with provider configuration read from environment variables by the example application.

`go run ./examples/send` defaults to local delivery. To send a real message, set `PIGEON_PROVIDER` (`ses`, `resend`, `mailgun`, or `sendgrid`), `PIGEON_FROM`, and `PIGEON_TO`. Use `PIGEON_API_KEY` for the three API-key providers, `PIGEON_DOMAIN` for Mailgun, and `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional `AWS_SESSION_TOKEN` for SES. `PIGEON_BASE_URL` is an optional endpoint override.

```sh
go test -race ./...
go vet ./...
go doc -all .
```

Tests use local HTTP servers and fixed signing fixtures. They cover request formats, error handling, cancellation, local retention, concurrent access, and unsafe HTML previews without sending real email.

The public API lives in the root package. Message values live in `internal/message`; HTTP adapters, SES signing, and the local inbox live in `internal/provider`. Go's `internal` import rule prevents applications from importing the adapter implementations directly. Protocol references and implementation choices are in [the research notes](docs/research.md).

## License

[Apache License 2.0](LICENSE).
