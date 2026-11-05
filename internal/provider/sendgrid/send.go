package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/message"
)

type sendGridPersonalization struct {
	To  []message.Address `json:"to"`
	Cc  []message.Address `json:"cc,omitempty"`
	Bcc []message.Address `json:"bcc,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sendGridPayload struct {
	Personalizations [1]sendGridPersonalization `json:"personalizations"`
	From             message.Address            `json:"from"`
	ReplyTo          *message.Address           `json:"reply_to,omitempty"`
	Subject          string                     `json:"subject"`
	Content          []sendGridContent          `json:"content"`
}

// Send submits one message and returns SendGrid's optional acceptance ID.
func (s *sendGridSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	payload := sendGridPayload{
		Personalizations: [1]sendGridPersonalization{{To: email.To, Cc: email.Cc, Bcc: email.Bcc}},
		From:             email.From,
		Subject:          email.Subject,
	}
	if email.ReplyTo.Email != "" {
		payload.ReplyTo = &email.ReplyTo
	}
	if email.Text != "" {
		payload.Content = append(payload.Content, sendGridContent{Type: "text/plain", Value: email.Text})
	}
	if email.HTML != "" {
		payload.Content = append(payload.Content, sendGridContent{Type: "text/html", Value: email.HTML})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return message.Receipt{}, fmt.Errorf("encode sendgrid message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.http.BaseURL()+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return message.Receipt{}, fmt.Errorf("create sendgrid request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	_, headers, err := s.http.Do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	return message.Receipt{ID: headers.Get("X-Message-Id")}, nil
}
