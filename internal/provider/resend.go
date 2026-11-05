package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/message"
)

type resendSender struct {
	http   httpSender
	apiKey string
}

// NewResend constructs a Resend sender using the configured API key and HTTP client.
func NewResend(config Config) (*resendSender, error) {
	if err := requireAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	h, err := newHTTPSender(config, "https://api.resend.com")
	if err != nil {
		return nil, err
	}
	return &resendSender{http: h, apiKey: config.APIKey}, nil
}

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Bcc     []string `json:"bcc,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}

// Send submits an email and returns Resend's acceptance ID, not delivery confirmation.
func (s *resendSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	payload := resendEmail{
		From:    email.From.String(),
		To:      message.AddressStrings(email.To),
		Cc:      message.AddressStrings(email.Cc),
		Bcc:     message.AddressStrings(email.Bcc),
		Subject: email.Subject,
		Text:    email.Text,
		HTML:    email.HTML,
	}
	if email.ReplyTo.Email != "" {
		payload.ReplyTo = email.ReplyTo.String()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return message.Receipt{}, fmt.Errorf("resend: encode email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.http.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return message.Receipt{}, fmt.Errorf("resend: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, _, err := s.http.do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return message.Receipt{}, fmt.Errorf("resend: decode response: %w", err)
	}
	if result.ID == "" {
		return message.Receipt{}, fmt.Errorf("resend: response missing id")
	}
	return message.Receipt{ID: result.ID}, nil
}
