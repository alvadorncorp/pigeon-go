// Package sendgrid delivers email through the Twilio SendGrid v3 Mail Send API.
package sendgrid

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"go.alvadorn.org/pigeon/internal/httptransport"
	"go.alvadorn.org/pigeon/internal/message"
)

// sendGridAddress is SendGrid's address object; the public Address carries no wire tags.
type sendGridAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type sendGridPersonalization struct {
	To  []sendGridAddress `json:"to"`
	Cc  []sendGridAddress `json:"cc,omitempty"`
	Bcc []sendGridAddress `json:"bcc,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sendGridPayload struct {
	Personalizations [1]sendGridPersonalization `json:"personalizations"`
	From             sendGridAddress            `json:"from"`
	ReplyTo          *sendGridAddress           `json:"reply_to,omitempty"`
	Subject          string                     `json:"subject"`
	Content          []sendGridContent          `json:"content"`
}

func addresses(list []message.Address) []sendGridAddress {
	if len(list) == 0 {
		return nil
	}
	result := make([]sendGridAddress, len(list))
	for i, a := range list {
		result[i] = sendGridAddress(a)
	}
	return result
}

// Send submits one message and returns SendGrid's optional acceptance ID.
func (s *sendGridSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	payload := sendGridPayload{
		Personalizations: [1]sendGridPersonalization{{
			To: addresses(email.To), Cc: addresses(email.Cc), Bcc: addresses(email.Bcc),
		}},
		From:    sendGridAddress(email.From),
		Subject: email.Subject,
	}
	if email.ReplyTo.Email != "" {
		replyTo := sendGridAddress(email.ReplyTo)
		payload.ReplyTo = &replyTo
	}
	if email.Text != "" {
		payload.Content = append(payload.Content, sendGridContent{Type: "text/plain", Value: email.Text})
	}
	if email.HTML != "" {
		payload.Content = append(payload.Content, sendGridContent{Type: "text/html", Value: email.HTML})
	}
	body, err := httptransport.EncodeJSON(payload)
	if err != nil {
		return message.Receipt{}, fmt.Errorf("sendgrid: encode email: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.http.BaseURL()+"/v3/mail/send",
		bytes.NewReader(body),
	)
	if err != nil {
		return message.Receipt{}, fmt.Errorf("sendgrid: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	_, headers, err := s.http.Do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	return message.Receipt{ID: headers.Get("X-Message-Id")}, nil
}
