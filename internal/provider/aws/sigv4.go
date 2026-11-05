package aws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

func sesHMAC(key []byte, text string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(text)) // hash.Hash.Write never returns an error.
	return mac.Sum(nil)
}

func sesSigningKey(secret, date, region, service string) []byte {
	key := sesHMAC([]byte("AWS4"+secret), date)
	key = sesHMAC(key, region)
	key = sesHMAC(key, service)
	return sesHMAC(key, "aws4_request")
}

func (s *sesSender) keyForDate(date string) []byte {
	s.signingMu.Lock()
	defer s.signingMu.Unlock()
	if s.signingDate != date || s.signingKey == nil {
		s.signingKey = sesSigningKey(s.secretKey, date, s.region, "ses")
		s.signingDate = date
	}
	return s.signingKey
}

func (s *sesSender) sign(req *http.Request, body []byte, now time.Time) {
	timestamp := now.UTC().Format("20060102T150405Z")
	date := timestamp[:8]
	req.Header.Set("X-Amz-Date", timestamp)
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	canonicalHeaders := "content-type:application/json\nhost:" + host + "\nx-amz-date:" + timestamp + "\n"
	signedHeaders := "content-type;host;x-amz-date"
	if s.token != "" {
		req.Header.Set("X-Amz-Security-Token", s.token)
		canonicalHeaders += "x-amz-security-token:" + strings.Join(strings.Fields(s.token), " ") + "\n"
		signedHeaders += ";x-amz-security-token"
	}
	payloadHash := sha256.Sum256(body)
	canonical := req.Method + "\n" + req.URL.EscapedPath() + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hex.EncodeToString(payloadHash[:])
	canonicalHash := sha256.Sum256([]byte(canonical))
	scope := date + "/" + s.region + "/ses/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	signature := hex.EncodeToString(sesHMAC(s.keyForDate(date), stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}
