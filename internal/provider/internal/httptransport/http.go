package httptransport

import (
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

type Client struct {
	client   *http.Client
	baseURL  string
	provider provider.Kind
}

func New(config *provider.Config, defaultURL string) (Client, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultURL
	}
	if err := validateOrigin(baseURL); err != nil {
		return Client{}, err
	}
	client, err := configuredClient(config.HTTPClient)
	if err != nil {
		return Client{}, err
	}
	return Client{client: &client, baseURL: strings.TrimSuffix(baseURL, "/"), provider: config.Provider}, nil
}

func validateOrigin(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return errors.New("pigeon: base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return errors.New("pigeon: base URL requires HTTPS except for literal loopback addresses")
		}
	}
	return nil
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
	if err != nil {
		return nil, nil, fmt.Errorf("pigeon: %s response: %w", h.provider, err)
	}
	if len(body) > maxResponseBytes {
		return nil, nil, fmt.Errorf("pigeon: %s response exceeds %d bytes", h.provider, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, responseError(h.provider, resp.StatusCode, body)
	}
	return body, resp.Header, nil
}

func (h *Client) BaseURL() string {
	return h.baseURL
}
