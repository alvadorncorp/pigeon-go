# The local inbox

For development, the local adapter keeps messages in memory and serves a web inbox rendered with Go templates. You can inspect an email without delivering it to anyone.

## Try it

```sh
go run ./examples/local
```

Open [127.0.0.1:8025](http://127.0.0.1:8025). The example submits two fictional messages through the same `Send` API used by cloud providers.

The inbox lists the newest 100 messages in a horizontal queue. Select a message or use the previous and next controls to move through it — including messages past the 100 newest, which stay openable by their ID — and HTML and Text views are offered when those formats are available. On mobile, use the message chooser to select an email and the native expandable envelope to inspect its addressing details.

The interface and its assets are embedded in the Go binary; no frontend build is required.

## Add it to a server

To add the inbox to an existing development server:

```go
mailer, err := pigeon.New(pigeon.ProviderLocal)
if err != nil {
    log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/mail/", http.StripPrefix("/mail", mailer.Inbox()))
```

`Inbox()` returns a read-only `http.Handler` for a local mailer and a handler that answers 404 for a cloud mailer; it is never nil. The library does not start a server or receive SMTP. The handler shows the real messages sent to its own mailer instance in the same process. Message URLs can be opened directly by ID (for example, `/mail/<id>` with the mount above); `http.StripPrefix` keeps inbox links and previews working under the chosen prefix.

## Retention

By default the inbox retains up to 100 messages and 16 MiB of message content. Pass `WithLocal(LocalLimits{Capacity, MaxBytes})` to override these independent limits; zero selects the default and negative values are invalid. Slots are allocated as messages arrive, so a large `Capacity` costs nothing up front. Oldest messages are evicted first. A message exceeding either the `WithMaxMessageBytes` limit or the whole inbox byte budget is rejected. The byte budget approximates what a retained message costs in memory: its content size plus a fixed per-message allowance for bookkeeping and a small allowance per recipient, so eviction starts before retained heap drifts far past the configured limit. It is an approximation, not an exact heap measure, and these limits are not process heap limits. Data disappears when the process stops.

The queue and browser title abbreviate subjects after 120 Unicode code points. The selected message shows the full subject.

## Security

Keep the inbox on a trusted development server. It has no authentication and displays email contents, recipients, Bcc, and any private links inside messages. The example binds only to loopback. HTML previews run in a sandbox with remote resources blocked, so they can look different from a real mail client.

The handler answers only requests whose `Host` is an IP literal, `localhost`, or a `*.localhost` name; any other hostname gets `403`. This blocks DNS rebinding, where an attacker's domain is pointed at 127.0.0.1 to read a loopback server. Reach the inbox through `localhost`, `*.localhost`, or an IP address, not a custom development domain.

HTML bodies are available only inside the inbox's protected iframe. Opening a stored message's raw preview URL directly returns `403`. The preview requires a browser that sends Fetch Metadata headers (`Sec-Fetch-Dest`).
