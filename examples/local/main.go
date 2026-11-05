// Command local runs an in-process development inbox with two example messages.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"go.alvadorn.org/pigeon"
)

func main() {
	mailer, err := pigeon.New(pigeon.Config{Provider: pigeon.ProviderLocal})
	if err != nil {
		log.Fatal(err)
	}
	for _, email := range []pigeon.Email{
		{
			From:    pigeon.Address{Name: "Pigeon examples", Email: "hello@example.com"},
			To:      []pigeon.Address{{Name: "Ada", Email: "ada@example.com"}},
			Subject: "Your first local letter",
			Text:    "This is an example message. Pigeon kept it in this process instead of sending it to an email provider.",
			HTML:    `<html><body style="font-family:system-ui;padding:24px;color:#243841"><h1>Your first local letter</h1><p>This is an example message for Ada.</p><p>Pigeon kept it in this process instead of sending it to an email provider.</p><p><strong>No email left your machine.</strong></p></body></html>`,
		},
		{
			From:    pigeon.Address{Name: "Pigeon examples", Email: "hello@example.com"},
			To:      []pigeon.Address{{Email: "reader@example.com"}},
			Cc:      []pigeon.Address{{Name: "Review", Email: "review@example.com"}},
			Bcc:     []pigeon.Address{{Email: "archive@example.com"}},
			ReplyTo: pigeon.Address{Name: "Replies", Email: "reply@example.com"},
			Subject: "Plain text works too",
			Text:    "This second example has no HTML.\n\nOpen it to inspect To, Cc, Bcc and Reply-To.\nThese addresses are fictional.",
		},
	} {
		if _, err := mailer.Send(context.Background(), &email); err != nil {
			log.Fatal(err)
		}
	}
	server := &http.Server{
		Addr:              "127.0.0.1:8025",
		Handler:           mailer.Inbox(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
	}
	log.Printf("Development inbox: http://%s (memory only; no authentication)", server.Addr)
	log.Fatal(server.ListenAndServe())
}
