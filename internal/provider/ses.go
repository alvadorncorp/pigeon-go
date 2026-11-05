package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.alvadorn.org/pigeon/internal/message"
)

type sesSender struct {
	httpSender
	region, accessKey, secretKey, token string
	signingMu                           sync.Mutex
	signingDate                         string
	signingKey                          []byte
}

// NewSES constructs an SES v2 sender using a fixed snapshot of AWS credentials.
// Credentials are not discovered or refreshed automatically.
func NewSES(config Config) (*sesSender, error) {
	if !validSESRegion(config.Region) {
		return nil, errors.New("ses: invalid region")
	}
	if config.AccessKeyID == "" || config.SecretAccessKey == "" || strings.ContainsAny(config.AccessKeyID, " \t\r\n/,=") || strings.ContainsAny(config.SessionToken, "\r\n") {
		return nil, errors.New("ses: invalid credentials")
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(config.Region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	h, err := newHTTPSender(config, "https://email."+config.Region+"."+suffix)
	if err != nil {
		return nil, err
	}
	return &sesSender{httpSender: h, region: config.Region, accessKey: config.AccessKeyID, secretKey: config.SecretAccessKey, token: config.SessionToken}, nil
}

func validSESRegion(region string) bool {
	if len(region) == 0 || len(region) > 63 || region[0] == '-' || region[len(region)-1] == '-' {
		return false
	}
	for _, c := range region {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

type sesText struct {
	Data    string `json:"Data"`
	Charset string `json:"Charset"`
}

type sesPayload struct {
	From        string `json:"FromEmailAddress"`
	Destination struct {
		To  []string `json:"ToAddresses,omitempty"`
		Cc  []string `json:"CcAddresses,omitempty"`
		Bcc []string `json:"BccAddresses,omitempty"`
	} `json:"Destination"`
	ReplyTo []string `json:"ReplyToAddresses,omitempty"`
	Content struct {
		Simple struct {
			Subject sesText `json:"Subject"`
			Body    struct {
				Text *sesText `json:"Text,omitempty"`
				HTML *sesText `json:"Html,omitempty"`
			} `json:"Body"`
		} `json:"Simple"`
	} `json:"Content"`
}

// Send submits an email to SES and returns its acceptance identifier.
func (s *sesSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	var payload sesPayload
	payload.From = email.From.String()
	payload.Destination.To = message.AddressStrings(email.To)
	payload.Destination.Cc = message.AddressStrings(email.Cc)
	payload.Destination.Bcc = message.AddressStrings(email.Bcc)
	if email.ReplyTo.Email != "" {
		payload.ReplyTo = []string{email.ReplyTo.String()}
	}
	payload.Content.Simple.Subject = sesText{Data: email.Subject, Charset: "UTF-8"}
	if email.Text != "" {
		payload.Content.Simple.Body.Text = &sesText{Data: email.Text, Charset: "UTF-8"}
	}
	if email.HTML != "" {
		payload.Content.Simple.Body.HTML = &sesText{Data: email.HTML, Charset: "UTF-8"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return message.Receipt{}, fmt.Errorf("ses: encode email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v2/email/outbound-emails", bytes.NewReader(body))
	if err != nil {
		return message.Receipt{}, fmt.Errorf("ses: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	s.sign(req, body, time.Now())
	response, _, err := s.do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	var result struct {
		MessageID string `json:"MessageId"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return message.Receipt{}, fmt.Errorf("ses: decode response: %w", err)
	}
	if result.MessageID == "" {
		return message.Receipt{}, errors.New("ses: response missing message id")
	}
	return message.Receipt{ID: result.MessageID}, nil
}
