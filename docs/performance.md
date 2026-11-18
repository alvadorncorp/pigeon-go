# Allocations and benchmarks

Pigeon reuses its configured HTTP client, sends one serialized payload per request, and uses concrete message values rather than maps. SES signs the exact bytes it sends and reuses its derived signing key until the UTC date changes. Common ASCII addresses have an allocation-free validation path, with `net/mail` handling other supported forms. Mailgun writes field contents directly into its multipart buffer without an intermediate byte copy. Local delivery copies recipient slices because callers may reuse them after sending.

The benchmark suite reports allocations as well as time. `BenchmarkSend` exercises the public API with an in-process HTTP transport, including validation and response decoding but excluding sockets and a remote server. Adapter-specific HTTP benchmarks include a local test server and should not be compared directly with those numbers. Measure with the workload you expect to send; HTTP transport, JSON encoding, signatures, and retaining local messages all have costs.

```sh
go test ./... -run '^$' -bench . -benchmem
```

These measurements used Go 1.27.3 on an Apple M2 Max, one ASCII sender and recipient, and a 4 KiB plain-text body. They compare the first working implementation with the measured changes described above, using `BenchmarkSend` and `-benchtime=100ms -count=1`. They exclude provider network latency; byte counts can vary slightly between runs.

The Local figures predate owned copies of retained strings. Current local delivery copies the content to avoid retaining larger backing buffers through substrings.

| Provider | Before, B/op | After, B/op | Before, allocs/op | After, allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Local | 230 | 39 | 11 | 1 |
| Amazon SES | 13,525 | 11,184 | 108 | 69 |
| Resend | 9,295 | 9,094 | 58 | 48 |
| Mailgun | 17,052 | 12,670 | 120 | 106 |
| Twilio SendGrid | 9,276 | 9,078 | 57 | 47 |
