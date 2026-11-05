# API research

Pigeon follows Swoosh's separation between an email value, a configured mailer, and a delivery adapter. Its local adapter captures messages for a development preview. Pigeon implements the shared message fields, without attempting full Swoosh feature parity.

- [Swoosh](https://hexdocs.pm/swoosh/Swoosh.html)
- [Swoosh.Mailer](https://hexdocs.pm/swoosh/Swoosh.Mailer.html)
- [Swoosh.Adapters.Local](https://hexdocs.pm/swoosh/Swoosh.Adapters.Local.html)
- [Plug.Swoosh.MailboxPreview](https://hexdocs.pm/swoosh/Plug.Swoosh.MailboxPreview.html)

## Amazon SES

[SES v2 SendEmail](https://docs.aws.amazon.com/ses/latest/APIReference-V2/API_SendEmail.html) accepts `POST /v2/email/outbound-emails`. The simple-message payload separates `Destination`, `ReplyToAddresses`, and `Content.Simple`. SES constructs the MIME message. A successful response supplies `MessageId`; acceptance does not establish delivery.

[Signature Version 4](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html) uses a SHA-256 payload hash, canonical request, credential scope, and HMAC-derived signing key. The SES service name is `ses`, although the endpoint host starts with `email`. This distinction is also visible in [AWS's generated endpoint code](https://github.com/aws/aws-sdk-go-v2/blob/main/service/sesv2/endpoints.go). Session credentials add the security token to both the request and signed headers. The signer hashes the same JSON buffer passed to HTTP.

[AWS signing fixtures](https://github.com/awslabs/aws-c-auth/tree/main/tests/aws-signing-test-suite/v4) provide independent expected signatures. Generic fixtures establish signing mechanics; SES request tests still need to cover its service name, payload, path, and optional token.

The endpoint is regional. See [SES endpoints](https://docs.aws.amazon.com/general/latest/gr/ses.html). Temporary credentials are supplied explicitly; role discovery and refresh are outside this library.

## Resend

[Send Email](https://resend.com/docs/api-reference/emails/send-email) uses `POST https://api.resend.com/emails`, Bearer authentication, and JSON. Recipient fields are strings or arrays; Pigeon emits arrays. `reply_to` is separate from `from`. Successful responses contain `id`.

The [API introduction](https://resend.com/docs/api-reference/introduction) requires a User-Agent for direct requests. Pigeon sends one through its shared HTTP code. Optional body and recipient fields are omitted when absent. Provider errors are decoded defensively rather than assuming an SDK-specific envelope. See [errors](https://resend.com/docs/api-reference/errors).

## Mailgun

The [Messages endpoint](https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/messages/post-v3--domain-name--messages) uses `POST /v3/{domain}/messages`, Basic authentication with username `api`, and multipart form data. The [HTTP sending guide](https://documentation.mailgun.com/docs/mailgun/user-manual/sending-messages/send-http) demonstrates repeated recipient fields. Pigeon uses those fields so a display name containing a comma is not mistaken for another recipient.

Reply-To uses Mailgun's `h:` header parameter, `h:Reply-To`. The default API host is `api.mailgun.net`; EU domains use `api.eu.mailgun.net`. Success returns an `id`. The implementation uses the documented multipart encoding even without attachments, rather than assuming URL-encoded forms work.

## Twilio SendGrid

[Mail Send v3](https://www.twilio.com/docs/sendgrid/api-reference/mail-send/mail-send) uses `POST /v3/mail/send` with Bearer authentication. Pigeon maps one email to one personalization, with address objects for To/Cc/Bcc, a sender object, and optional `reply_to`. The content array places plain text before HTML.

Success is `202 Accepted` with no required response body. [X-Message-ID](https://www.twilio.com/docs/sendgrid/glossary/x-message-id) identifies the submission when present; a missing header does not turn an accepted message into a failure. Errors may contain an `errors` array, unlike the other providers' top-level messages. EU accounts can select their regional API origin through configuration.

## Local preview and memory

[html/template](https://pkg.go.dev/html/template) escapes untrusted data according to its output context. Email HTML must not be cast into trusted HTML in the inbox document. The local adapter serves it separately under an empty-permissions iframe sandbox and restrictive Content Security Policy. Scripts, remote resources, and form submission are blocked.

The parent inbox's `frame-src 'self'` policy also blocks navigation to another origin. A sandbox alone does not prevent every kind of navigation: a direct visit to the raw HTML endpoint can follow a link without that parent policy. The endpoint therefore accepts only iframe requests identified by `Sec-Fetch-Dest`; direct navigation and requests without that header receive `403`. The shell rejects framing, while previews allow only same-origin ancestors.

The local inbox is a bounded, process-local sink. It clones recipient slices at submission, shares immutable strings, and protects its ring with a mutex. It releases the lock before rendering templates or writing to a browser. A count limit and byte budget prevent unlimited retained email data; they are not a process-wide heap limit.

## Scope and verification

A single module and internal packages keep provider code inaccessible to consumers. Public aliases preserve one representation of messages across that boundary. The module targets Go 1.27; the [official release feed](https://go.dev/dl/?mode=json) reported Go 1.27.3 during implementation.

No SDK, third-party HTTP client, template engine, serializer, or authentication package is needed. Standard-library serializers and cryptographic primitives are the initial implementation. Allocation benchmarks guide optimization; a hand-written encoder or object pool needs measured evidence to justify its extra maintenance cost.

Protocol tests and smoke runs use local servers. Live provider acceptance, sender verification, account quotas, and final delivery require account credentials and are not established by these checks. Automatic retries are deliberately absent because a lost response can leave acceptance ambiguous.
