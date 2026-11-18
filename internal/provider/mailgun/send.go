package mailgun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"

	"go.alvadorn.org/pigeon/internal/message"
)

// Send posts the message as Mailgun's multipart form and returns its acceptance ID.
func (s *mailgunSender) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	var body bytes.Buffer
	const multipartOverhead = 2048
	body.Grow(message.Size(email) + multipartOverhead)
	form := multipart.NewWriter(&body)
	writeMessageFields(form, &body, email)
	_ = form.Close() // Writes the closing boundary into body; bytes.Buffer never fails.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body.Bytes()))
	if err != nil {
		return message.Receipt{}, fmt.Errorf("mailgun: create request: %w", err)
	}
	req.Header.Set("Authorization", s.auth)
	req.Header.Set("Content-Type", form.FormDataContentType())
	data, _, err := s.http.Do(req)
	if err != nil {
		return message.Receipt{}, err
	}
	// A 2xx status means Mailgun accepted the message. A body that is empty,
	// unreadable or not the expected JSON must not turn acceptance into an
	// error: the documented retry guidance would then send a duplicate email.
	// The receipt ID is simply unknown.
	var result struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &result)
	return message.Receipt{ID: result.ID}, nil
}

func writeMessageFields(form *multipart.Writer, body *bytes.Buffer, email *message.Email) {
	writeFormField(form, body, "from", email.From.String())
	for _, group := range []struct {
		name      string
		addresses []message.Address
	}{{"to", email.To}, {"cc", email.Cc}, {"bcc", email.Bcc}} {
		for _, address := range group.addresses {
			writeFormField(form, body, group.name, address.String())
		}
	}
	if email.ReplyTo.Email != "" {
		writeFormField(form, body, "h:Reply-To", email.ReplyTo.String())
	}
	writeFormField(form, body, "subject", email.Subject)
	if email.Text != "" {
		writeFormField(form, body, "text", email.Text)
	}
	if email.HTML != "" {
		writeFormField(form, body, "html", email.HTML)
	}
}

// writeFormField writes value straight into body, the writer's destination,
// avoiding the []byte copy of multipart.Writer.WriteField. Writes into a
// bytes.Buffer cannot fail.
func writeFormField(form *multipart.Writer, body *bytes.Buffer, name, value string) {
	_, _ = form.CreateFormField(name)
	body.WriteString(value)
}
