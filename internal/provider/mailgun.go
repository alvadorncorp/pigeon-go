package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"go.alvadorn.org/pigeon/internal/message"
)

type mailgunSender struct {
	httpSender
	url  string
	auth string
}

// NewMailgun creates a sender for a Mailgun domain using the configured API key.
func NewMailgun(config Config) (*mailgunSender, error) {
	if err := requireAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	if !validMailgunDomain(config.Domain) {
		return nil, errors.New("mailgun: invalid domain")
	}
	h, err := newHTTPSender(config, "https://api.mailgun.net")
	if err != nil {
		return nil, err
	}
	return &mailgunSender{
		httpSender: h,
		url:        h.baseURL + "/v3/" + config.Domain + "/messages",
		auth:       "Basic " + base64.StdEncoding.EncodeToString([]byte("api:"+config.APIKey)),
	}, nil
}

// validMailgunDomain accepts DNS hostnames only, preventing path and URL injection.
func validMailgunDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	for label := range strings.SplitSeq(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Send posts the message as Mailgun's multipart form and returns its acceptance ID.
func (s *mailgunSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	var body bytes.Buffer
	body.Grow(len(email.Text) + len(email.HTML) + len(email.Subject) + 2048)
	form := multipart.NewWriter(&body)
	write := func(name, value string) error {
		if _, err := form.CreateFormField(name); err != nil {
			return fmt.Errorf("mailgun: write %s: %w", name, err)
		}
		if _, err := body.WriteString(value); err != nil {
			return fmt.Errorf("mailgun: write %s: %w", name, err)
		}
		return nil
	}
	if err := write("from", email.From.String()); err != nil {
		return message.Receipt{}, err
	}
	for _, group := range []struct {
		name      string
		addresses []message.Address
	}{{"to", email.To}, {"cc", email.Cc}, {"bcc", email.Bcc}} {
		for _, address := range group.addresses {
			if err := write(group.name, address.String()); err != nil {
				return message.Receipt{}, err
			}
		}
	}
	if email.ReplyTo.Email != "" {
		if err := write("h:Reply-To", email.ReplyTo.String()); err != nil {
			return message.Receipt{}, err
		}
	}
	if err := write("subject", email.Subject); err != nil {
		return message.Receipt{}, err
	}
	if email.Text != "" {
		if err := write("text", email.Text); err != nil {
			return message.Receipt{}, err
		}
	}
	if email.HTML != "" {
		if err := write("html", email.HTML); err != nil {
			return message.Receipt{}, err
		}
	}
	if err := form.Close(); err != nil {
		return message.Receipt{}, fmt.Errorf("mailgun: close multipart form: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body.Bytes()))
	if err != nil {
		return message.Receipt{}, fmt.Errorf("mailgun: create request: %w", err)
	}
	req.Header.Set("Authorization", s.auth)
	req.Header.Set("Content-Type", form.FormDataContentType())
	data, _, err := s.do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return message.Receipt{}, fmt.Errorf("mailgun: decode response: %w", err)
	}
	if result.ID == "" {
		return message.Receipt{}, errors.New("mailgun: response missing id")
	}
	return message.Receipt{ID: result.ID}, nil
}
