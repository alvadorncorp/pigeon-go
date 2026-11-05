package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.alvadorn.org/pigeon/internal/message"
)

const maxResponseBytes = 64 << 10

type httpSender struct {
	client   *http.Client
	baseURL  string
	provider Kind
}

func newHTTPSender(config Config, defaultURL string) (httpSender, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return httpSender{}, errors.New("pigeon: base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return httpSender{}, errors.New("pigeon: base URL requires HTTPS except for literal loopback addresses")
		}
	}
	client := http.Client{Timeout: 30 * time.Second}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
		if client.Timeout < 0 {
			return httpSender{}, errors.New("pigeon: HTTP client timeout cannot be negative")
		}
	}
	// Redirects can resend a message or leak its credentials and body.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return httpSender{client: &client, baseURL: strings.TrimSuffix(baseURL, "/"), provider: config.Provider}, nil
}

func requireAPIKey(key string) error {
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

func (h *httpSender) do(req *http.Request) ([]byte, http.Header, error) {
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

func responseError(provider Kind, status int, body []byte) *message.DeliveryError {
	result := &message.DeliveryError{Provider: string(provider), StatusCode: status}
	var detail struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Type    string `json:"__type"`
		Code    string `json:"code"`
		Errors  []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &detail) == nil {
		result.Message = detail.Message
		result.Code = detail.Code
		if result.Code == "" {
			result.Code = detail.Name
		}
		if result.Code == "" {
			result.Code = detail.Type
		}
		if result.Message == "" && len(detail.Errors) > 0 {
			result.Message = detail.Errors[0].Message
		}
	}
	return result
}
