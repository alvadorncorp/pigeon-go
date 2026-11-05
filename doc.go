// Package pigeon sends email through a configured provider without changing the
// message-building code. It supports Amazon SES, Resend, Mailgun, Twilio SendGrid,
// and a local development inbox using only the Go standard library.
//
// Create a Mailer once with New, then share it across goroutines. Build messages
// with Email and Address values and submit them with Mailer.Send. A successful
// Receipt means the provider accepted the submission, not that it was delivered.
// Pigeon does not queue, retry, refresh credentials, or track delivery events.
//
// The portable message contract requires an ASCII From address, at least one To
// recipient, a nonempty subject, and a text or HTML body. Up to 50 recipients
// across To, Cc and Bcc are supported. Display names and bodies may use Unicode;
// international domains must be supplied in their ASCII form. Attachments,
// arbitrary headers and provider-specific templates are not supported.
//
// With ProviderLocal, Send retains a bounded in-memory snapshot. Mailer.Inbox
// exposes an http.Handler for viewing those messages. Serve it only in trusted
// development environments; it has no authentication and loses all messages
// when the process exits. HTML previews are sandboxed and remote resources are
// blocked, so their appearance can differ from a real email client. The raw HTML
// route is iframe-only and requires browser Fetch Metadata headers; opening it
// directly returns HTTP 403.
package pigeon
