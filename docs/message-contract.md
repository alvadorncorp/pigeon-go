# Message contract

`Email` supports `From`, `To`, `Cc`, `Bcc`, one `ReplyTo`, `Subject`, `Text`, and `HTML`. It requires at least one `To` recipient, a nonempty subject, and a text or HTML body. The combined recipient limit is 50, keeping the same message within SES's recipient limit when switching providers.

Put the raw ASCII address in `Address.Email` and the optional display name in `Address.Name`. Display names, subjects, and bodies may contain Unicode. International domain names must already be in ASCII form. Header control characters and malformed addresses are rejected before a request is sent.

A mailer is safe to share across goroutines. Do not mutate a message or its recipient slices during `Send`. After it returns, those slices can be reused: the local adapter keeps its own snapshot, while cloud adapters retain no reference to the message.

Attachments, custom headers, provider-hosted templates, batch APIs, and delivery webhooks are outside the current API.
