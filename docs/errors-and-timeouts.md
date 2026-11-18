# Errors and timeouts

Pass a context to bound an individual send. The default HTTP timeout is 30 seconds. You can supply `WithHTTPClient`; Pigeon copies the client, shares its transport, and disables redirects. A zero client timeout gets the same 30-second default.

Provider HTTP failures support typed inspection:

```go
if delivery, ok := errors.AsType[*pigeon.DeliveryError](err); ok {
    log.Printf("Email provider returned HTTP %d", delivery.StatusCode)
}
```

`DeliveryError.Error()` excludes provider response text. The `Message` and `Code` fields retain diagnostic details when available, and may contain personal data; review them before logging. Context and transport errors remain discoverable with `errors.Is` and `errors.AsType`.

A 2xx response means the provider accepted the message: `Send` returns a nil error even when the response body is unreadable, exceeds the 64 KiB read limit, or omits the provider's message identifier. In those cases `receipt.ID` is empty. Never treat an empty ID as a failure or retry because of it; retrying an accepted message can send a duplicate. Response bodies are read up to 64 KiB: a larger accepted body is discarded rather than failing the send, while error diagnostics keep at most 64 KiB of provider text.
