# Allocations and benchmarks

Pigeon reuses its configured HTTP client, sends one serialized payload per request, and uses concrete message values rather than maps. SES signs the exact bytes it sends and reuses its derived signing key until the UTC date changes. Common ASCII addresses have an allocation-free validation path, with `net/mail` handling other supported forms. Mailgun writes field contents directly into its multipart buffer without an intermediate byte copy. Local delivery copies recipient slices because callers may reuse them after sending.

The benchmark suite reports allocations as well as time. `BenchmarkSend` exercises the public API with an in-process HTTP transport, including validation and response decoding but excluding sockets and a remote server. Adapter-specific HTTP benchmarks include a local test server and should not be compared directly with those numbers. Measure with the workload you expect to send; HTTP transport, JSON encoding, signatures, and retaining local messages all have costs.

```sh
go test ./... -run '^$' -bench . -benchmem
```

These measurements used Go 1.27.4 on an Apple M2 Max, one ASCII sender and recipient, and a 4 KiB plain-text body, using `BenchmarkSend` and `-benchtime=100ms -count=1`. "Before" is the first working implementation. They exclude provider network latency; byte counts can vary slightly between runs.

Local delivery now copies the content (4 KiB here) so it never retains larger backing buffers through substrings. JSON adapters encode without HTML escaping: that costs two small allocations on a plain-text body, and saves about half the payload on HTML email, where `<`, `>` and `&` would otherwise grow to six bytes each (a 50 KB HTML body went from +57 % to +4 % over its raw size).

| Provider | Before, B/op | After, B/op | Before, allocs/op | After, allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Local | 230 | 4,192 | 11 | 6 |
| Amazon SES | 13,525 | 11,357 | 108 | 71 |
| Resend | 9,295 | 9,247 | 58 | 50 |
| Mailgun | 17,052 | 12,677 | 120 | 106 |
| Twilio SendGrid | 9,276 | 9,260 | 57 | 50 |
