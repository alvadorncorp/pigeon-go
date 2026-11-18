# Message contract

`Email` supports `From`, `To`, `Cc`, `Bcc`, one `ReplyTo`, `Subject`, `Text`, and `HTML`; see `ExampleMailer_Send` in the package docs. It requires at least one `To` recipient, a nonempty subject, and a text or HTML body. The combined recipient limit is 50, keeping the same message within SES's recipient limit when switching providers. An address may appear only once across `To`, `Cc` and `Bcc` (compared case-insensitively), because SendGrid rejects repeats.

`WithMaxMessageBytes` limits the combined UTF-8 bytes of subject, text, HTML, and all address names and email addresses before encoding. Omitting it or passing zero selects 1 MiB; negative values are invalid. An oversized email is rejected without truncation or submission. Larger configured limits are capped at 25,000,000 bytes for Mailgun, 29,999,999 for SendGrid (strictly below 30 MB), and 40,000,000 for SES v2 and Resend. The encoded message can be larger, so passing local validation does not guarantee provider acceptance.

Put the raw ASCII address in `Address.Email` and the optional display name in `Address.Name`. Display names, subjects, and bodies may contain Unicode. International domain names must already be in ASCII form. The domain must be a DNS hostname: domain literals such as `[127.0.0.1]` and quoted local parts are rejected. Header control characters and malformed addresses are rejected before a request is sent.

A mailer is safe to share across goroutines. Do not mutate a message or its recipient slices during `Send`. After it returns, those slices can be reused: the local adapter keeps its own snapshot, while cloud adapters retain no reference to the message.

Attachments, custom headers, provider-hosted templates, batch APIs, and delivery webhooks are outside the current API.
