package pigeon_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon"
)

// benchmarkTransport includes body consumption and response decoding, but excludes
// sockets and a server so the result measures the submitting application's cost.
type benchmarkTransport struct{}

func (benchmarkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := io.Copy(io.Discard, req.Body); err != nil {
		return nil, err
	}
	if err := req.Body.Close(); err != nil {
		return nil, err
	}
	status := http.StatusOK
	if req.URL.Path == "/v3/mail/send" {
		status = http.StatusAccepted
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"id":"bench","MessageId":"bench"}`)), Header: make(http.Header)}, nil
}

func BenchmarkSend(b *testing.B) {
	for _, kind := range []pigeon.Provider{pigeon.ProviderLocal, pigeon.ProviderSES, pigeon.ProviderResend, pigeon.ProviderMailgun, pigeon.ProviderSendGrid} {
		b.Run(string(kind), func(b *testing.B) {
			for _, workload := range []struct {
				name string
				size int
			}{{"small", 64}, {"typical", 4096}, {"large", 256 << 10}} {
				b.Run(workload.name, func(b *testing.B) {
					mailer, err := pigeon.New(pigeon.Config{Provider: kind, APIKey: "benchmark-key", Domain: "example.com", Region: "us-east-1", AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "benchmark-secret", HTTPClient: &http.Client{Transport: benchmarkTransport{}}})
					if err != nil {
						b.Fatal(err)
					}
					email := &pigeon.Email{From: pigeon.Address{Email: "sender@example.com"}, To: []pigeon.Address{{Email: "reader@example.com"}}, Subject: "Benchmark", Text: strings.Repeat("a", workload.size)}
					b.ReportAllocs()
					b.SetBytes(int64(workload.size))
					for b.Loop() {
						if _, err := mailer.Send(context.Background(), email); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
