# Errors and timeouts

Pass a context to bound an individual send. The default HTTP timeout is 30 seconds. You can supply an `HTTPClient`; Pigeon copies the client, shares its transport, and disables redirects. A zero client timeout gets the same 30-second default.

Provider HTTP failures support typed inspection:

```go
if delivery, ok := errors.AsType[*pigeon.DeliveryError](err); ok {
    log.Printf("Email provider returned HTTP %d", delivery.StatusCode)
}
```

`DeliveryError.Error()` excludes provider response text. The `Message` and `Code` fields retain diagnostic details when available, and may contain personal data; review them before logging. Context and transport errors remain discoverable with `errors.Is` and `errors.AsType`. Responses are limited to 64 KiB.
