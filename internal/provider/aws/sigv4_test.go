package aws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSESAWSPublishedSigningVector(t *testing.T) {
	// AWS aws-c-auth signing test suite, v4/post-vanilla: the published
	// string-to-sign and signature independently exercise key derivation.
	const sts = "AWS4-HMAC-SHA256\n20150830T123600Z\n20150830/us-east-1/service/aws4_request\n" +
		"553f88c9e4d10fc9e109e2aeb65f030801b70c2f6468faca261d401ae622fc87"
	key := sesSigningKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "service")
	got := hex.EncodeToString(sesHMAC(key, sts))
	const want = "5da7c1a2acd57cee7505fc6676e4e544621c30862966e37dddb68e92efbe5d6b"
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

// sesVerifyRequest independently reconstructs the signature from the received wire bytes.
func sesVerifyRequest(req *http.Request, body []byte, secret, region, token string) error {
	timestamp := req.Header.Get("X-Amz-Date")
	if len(timestamp) != 16 {
		return fmt.Errorf("invalid timestamp %q", timestamp)
	}
	headers := "content-type:" + req.Header.Get(
		"Content-Type",
	) + "\nhost:" + req.Host + "\nx-amz-date:" + timestamp + "\n"
	signed := "content-type;host;x-amz-date"
	if token != "" {
		if req.Header.Get("X-Amz-Security-Token") != token {
			return fmt.Errorf("missing session token")
		}
		headers += "x-amz-security-token:" + strings.Join(strings.Fields(token), " ") + "\n"
		signed += ";x-amz-security-token"
	}
	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%x",
		req.Method, req.URL.EscapedPath(), req.URL.RawQuery, headers, signed, bodyHash)
	canonicalHash := sha256.Sum256([]byte(canonical))
	scope := timestamp[:8] + "/" + region + "/ses/aws4_request"
	toSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%x", timestamp, scope, canonicalHash)
	mac := func(key []byte, value string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(value))
		return h.Sum(nil)
	}
	key := mac([]byte("AWS4"+secret), timestamp[:8])
	key = mac(key, region)
	key = mac(key, "ses")
	key = mac(key, "aws4_request")
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/" + scope + ", SignedHeaders=" + signed +
		", Signature=" + hex.EncodeToString(mac(key, toSign))
	if got := req.Header.Get("Authorization"); got != want {
		return fmt.Errorf("authorization = %q, want %q", got, want)
	}
	return nil
}

func TestSESSignStaticAndSessionCredentials(t *testing.T) {
	for _, token := range []string{"", "temporary-session-token"} {
		s := sesSender{region: "us-east-1", accessKey: "AKIDEXAMPLE", secretKey: "test-secret", token: token}
		req, err := http.NewRequest(
			http.MethodPost, "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails", http.NoBody,
		)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = req.URL.Host
		req.Header.Set("Content-Type", "application/json")
		body := []byte(`{"Data":"Olá"}`)
		s.sign(req, body, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
		if err := sesVerifyRequest(req, body, "test-secret", "us-east-1", token); err != nil {
			t.Fatal(err)
		}
		if err := sesVerifyRequest(req, []byte(`{"Data":"altered"}`), "test-secret", "us-east-1", token); err == nil {
			t.Fatal("signature accepted a different payload")
		}
	}
}

func TestSESSignDateChanges(t *testing.T) {
	s := sesSender{region: "us-east-1", accessKey: "AKIDEXAMPLE", secretKey: "test-secret"}
	times := []time.Time{
		time.Date(2024, 1, 2, 23, 59, 59, 0, time.UTC),
		time.Date(2024, 1, 3, 1, 0, 0, 0, time.FixedZone("east", 2*60*60)),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 23, 59, 59, 0, time.UTC),
	}
	signAndVerify := func(now time.Time) error { return sesSignDateAndVerify(&s, now) }
	for _, now := range times {
		if err := signAndVerify(now); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 32 {
		for _, now := range times {
			wg.Go(func() {
				if err := signAndVerify(now); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
}

func sesSignDateAndVerify(s *sesSender, now time.Time) error {
	req, err := http.NewRequest(
		http.MethodPost, "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails", http.NoBody,
	)
	if err != nil {
		return err
	}
	req.Host = req.URL.Host
	req.Header.Set("Content-Type", "application/json")
	body := []byte(`{"Data":"date regression"}`)
	s.sign(req, body, now)
	if got, want := req.Header.Get("X-Amz-Date"), now.UTC().Format("20060102T150405Z"); got != want {
		return fmt.Errorf("timestamp = %q, want %q", got, want)
	}
	return sesVerifyRequest(req, body, "test-secret", "us-east-1", "")
}

func BenchmarkSESSign(b *testing.B) {
	s := sesSender{region: "us-east-1", accessKey: "AKIDEXAMPLE", secretKey: "secret", token: "session-token"}
	req, err := http.NewRequest(
		http.MethodPost, "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails", http.NoBody,
	)
	if err != nil {
		b.Fatal(err)
	}
	body := []byte(`{"Subject":"Hello"}`)
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		s.sign(req, body, now)
	}
}
