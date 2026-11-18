package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.alvadorn.org/pigeon/internal/provider"
)

const (
	maxResponseBytes = 64 << 10
	defaultTimeout   = 30 * time.Second
)

// Client is the shared HTTP policy of the cloud adapters.
type Client struct {
	client   *http.Client
	baseURL  string
	provider provider.Kind
}

// New validates the origin and HTTP client in config and falls back to defaultURL.
func New(config *provider.Config, defaultURL string) (Client, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultURL
	}
	origin, err := validateOrigin(baseURL)
	if err != nil {
		return Client{}, err
	}
	client, err := configuredClient(config.HTTPClient)
	if err != nil {
		return Client{}, err
	}
	return Client{client: &client, baseURL: origin, provider: config.Provider}, nil
}

// validateOrigin returns the normalized scheme://host of baseURL. It rebuilds
// the origin so a trailing "?" or "#" cannot survive into request URLs.
func validateOrigin(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		strings.ContainsRune(baseURL, '#') || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("pigeon: base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return "", errors.New("pigeon: base URL requires HTTPS except for literal loopback addresses")
		}
	}
	return u.Scheme + "://" + u.Host, nil
}

func configuredClient(original *http.Client) (http.Client, error) {
	client := http.Client{Timeout: defaultTimeout}
	if original != nil {
		client = *original
		if client.Timeout == 0 {
			client.Timeout = defaultTimeout
		}
		if client.Timeout < 0 {
			return http.Client{}, errors.New("pigeon: HTTP client timeout cannot be negative")
		}
	}
	// Redirects can resend a message or leak its credentials and body.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client, nil
}

// RequireAPIKey rejects empty keys and anything but printable non-space ASCII,
// so a credential can never split an HTTP header.
func RequireAPIKey(key string) error {
	if key == "" {
		return errors.New("pigeon: API key is required")
	}
	for i := range len(key) {
		if key[i] <= ' ' || key[i] >= 127 {
			return errors.New("pigeon: API key must contain only printable non-space ASCII")
		}
	}
	return nil
}

// EncodeJSON marshals v without HTML escaping. Email HTML is full of <, > and
// &, which json.Marshal would expand to six-byte \u00XX sequences.
func EncodeJSON(v any) ([]byte, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(body.Bytes(), []byte("\n")), nil
}

// Do sends req and returns the body and headers of a 2xx response. Any other
// status becomes a *message.DeliveryError, even when its body exceeds the cap.
// A 2xx body that cannot be read or exceeds maxResponseBytes returns a nil body
// with nil error: the provider accepted the message, and reporting a failure
// would make the documented retry guidance duplicate an accepted delivery.
func (h *Client) Do(req *http.Request) ([]byte, http.Header, error) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pigeon")
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("pigeon: %s request: %w", h.provider, err)
	}
	// A close error must not turn an accepted delivery into a retry and duplicate.
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, responseError(h.provider, resp, body[:min(len(body), maxResponseBytes)])
	}
	// The provider accepted the message, so an unreadable or oversized body only
	// costs the receipt. It must not become an error: the application would
	// retry an accepted delivery and the recipient would get a duplicate.
	if err != nil || len(body) > maxResponseBytes {
		return nil, resp.Header, nil
	}
	return body, resp.Header, nil
}

// BaseURL returns the validated scheme://host origin.
func (h *Client) BaseURL() string {
	return h.baseURL
}
