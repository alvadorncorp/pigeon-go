<p align="center">
  <img src="docs/assets/pigeon.svg" width="240" alt="Pigeon">
</p>

# Pigeon

Pigeon sends email from Go applications. You build a message as an `Email` value, choose a provider in configuration, and call `Send`. Switching from a local inbox to Amazon SES, Resend, Mailgun, or Twilio SendGrid changes configuration only; the code that builds the message stays as it is.

The idea comes from [Swoosh](https://hexdocs.pm/swoosh/Swoosh.html), the Elixir email library. Pigeon brings that separation between messages and delivery adapters to Go. It calls each provider's HTTP API directly, including signing SES requests with AWS Signature Version 4, and has no dependencies outside the standard library.

## Why this shape

Email code usually mixes what the message says with how one particular provider wants it sent. Once that happens, testing a message means sending real email, and changing providers means editing the code that builds it.

Pigeon keeps those concerns apart: the message is a value you can inspect before sending, the mailer is a configured object you share across goroutines, and the provider is a line in the configuration. In tests and local development, the local adapter captures messages in memory and serves a web inbox, so you can read what your application sent without delivering anything to anyone.

The scope is deliberately narrow. Pigeon does not queue, retry, or track delivery. A receipt means the provider accepted the request; whether it reaches the inbox is the provider's business. A nil error from `Send` always means acceptance, even when `receipt.ID` comes back empty, so an empty ID is never a reason to retry. An application can retry a failed submission, but a timeout can happen after the provider has accepted the message, so a retry may produce a duplicate email. Deciding whether to accept that trade belongs to the application.

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
    mailer, err := pigeon.New(pigeon.ProviderLocal)
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

## Documentation

| Document | Contents |
| --- | --- |
| [Providers](docs/providers.md) | Options for each provider, base URLs, credential handling |
| [The local inbox](docs/local-inbox.md) | The development inbox: running it, mounting it, retention, security |
| [Message contract](docs/message-contract.md) | The `Email` value, validation rules, goroutine safety |
| [Errors and timeouts](docs/errors-and-timeouts.md) | `DeliveryError`, HTTP timeouts, context handling |
| [Allocations and benchmarks](docs/performance.md) | Allocation work and measured numbers |
| [Examples and development](docs/development.md) | Examples, repository layout, verification commands |

## License

[Apache License 2.0](LICENSE).
