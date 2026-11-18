# Examples and development

## Examples

- [`examples/local`](../examples/local): a running inbox with text and HTML messages.
- [`examples/send`](../examples/send): one submission, with provider configuration read from environment variables by the example application.

`go run ./examples/send` defaults to local delivery. To send a real message, set `PIGEON_PROVIDER` (`ses`, `resend`, `mailgun`, or `sendgrid`), `PIGEON_FROM`, and `PIGEON_TO`. Use `PIGEON_API_KEY` for the three API-key providers, `PIGEON_DOMAIN` for Mailgun, and `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional `AWS_SESSION_TOKEN` for SES. `PIGEON_BASE_URL` is an optional endpoint override for cloud providers; the local provider rejects it.

## Verification

```sh
make lint   # golangci-lint fmt --diff and golangci-lint run
make test   # go test -race ./... and go build ./examples/...
```

Tests use local HTTP servers and fixed signing fixtures. They cover request formats, error handling, cancellation, local retention, concurrent access, and unsafe HTML previews without sending real email.

CI runs `make test` and `make lint` on pull requests and pushes to `main`. Merging locally
does not run hosted CI until the resulting commit is pushed.
Formatting uses `gofumpt` and `golines`, with a 120-column target matching the `lll` limit.

## Repository layout

The public API lives in the root package. One exception is deliberate: `Address`, `Email`, `Receipt` and `DeliveryError` are type aliases — the types are defined in `internal/message` and re-exported by the root, so a change to their exported fields or methods is a public API change. The shape check in the root tests (`api_test.go`) fails when that happens. `internal/provider` defines the shared configuration. Each adapter has its own package under `internal/provider`: `aws` (SES and SigV4 signing), `resend`, `mailgun`, `sendgrid`, and `local`. Cloud adapters separate client construction from sending; the local inbox separates message retention, HTTP handling, and embedded assets. Shared HTTP policy, JSON encoding and error handling live in `internal/httptransport`. `architecture_test.go` pins which internal package may import which, that `internal/message` stays a leaf with no module imports, and that `examples/` imports only the root package. Go's `internal` import rule prevents applications from importing the adapter implementations directly.

Protocol references and implementation choices are in [the research notes](research.md).
