# The local inbox

For development, the local adapter keeps messages in memory and serves a web inbox rendered with Go templates. You can inspect an email without delivering it to anyone.

## Try it

```sh
go run ./examples/local
```

Open [127.0.0.1:8025](http://127.0.0.1:8025). The example submits two fictional messages through the same `Send` API used by cloud providers.

The inbox shows messages in a horizontal queue. Select a message or use the previous and next controls to move through it; HTML and Text views are offered when those formats are available. On mobile, use the message chooser to select an email and the native expandable envelope to inspect its addressing details.

The interface and its assets are embedded in the Go binary; no frontend build is required.

## Add it to a server

To add the inbox to an existing development server:

```go
mailer, err := pigeon.New(&pigeon.Config{Provider: pigeon.ProviderLocal})
if err != nil {
    log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/mail/", http.StripPrefix("/mail", mailer.Inbox()))
```

`Inbox()` returns a read-only `http.Handler` for a local mailer and `nil` for a cloud mailer. The library does not start a server or receive SMTP. The handler shows the real messages sent to its own mailer instance in the same process. Message URLs can be opened directly by ID (for example, `/mail/<id>` with the mount above); `http.StripPrefix` keeps inbox links and previews working under the chosen prefix.

## Retention

By default the inbox retains up to 100 messages and 16 MiB of message content. Set `LocalCapacity` and `LocalMaxBytes` to override these independent limits. Oldest messages are evicted first. A message exceeding either `MaxMessageBytes` or the whole inbox byte budget is rejected. These limits are not process heap limits. Data disappears when the process stops.

The queue and browser title abbreviate subjects after 120 Unicode code points. The selected message shows the full subject.

## Security

Keep the inbox on a trusted development server. It has no authentication and displays email contents, recipients, Bcc, and any private links inside messages. The example binds only to loopback. HTML previews run in a sandbox with remote resources blocked, so they can look different from a real mail client.

HTML bodies are available only inside the inbox's protected iframe. Opening a raw preview URL directly returns `403`. The preview requires a browser that sends Fetch Metadata headers (`Sec-Fetch-Dest`).
