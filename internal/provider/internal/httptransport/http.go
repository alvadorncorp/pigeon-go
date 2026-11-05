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

const maxResponseBytes = 64 << 10

type Client struct {
	client   *http.Client
	baseURL  string
	provider provider.Kind
}

func New(config provider.Config, defaultURL string) (Client, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return Client{}, errors.New("pigeon: base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return Client{}, errors.New("pigeon: base URL requires HTTPS except for literal loopback addresses")
		}
	}
	client := http.Client{Timeout: 30 * time.Second}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
		if client.Timeout < 0 {
			return Client{}, errors.New("pigeon: HTTP client timeout cannot be negative")
		}
	}
	// Redirects can resend a message or leak its credentials and body.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return Client{client: &client, baseURL: strings.TrimSuffix(baseURL, "/"), provider: config.Provider}, nil
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
	defer resp.Body.Close()
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
