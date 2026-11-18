package pigeon_test

import (
	"context"
	"fmt"
	"log"

	"go.alvadorn.org/pigeon"
)

// Every Email field in one message. ReplyTo, Cc, Bcc and one of Text or HTML
// are optional; an address may appear only once across To, Cc and Bcc.
func ExampleMailer_Send() {
	mailer, err := pigeon.New(pigeon.ProviderLocal)
	if err != nil {
		log.Fatal(err)
	}
	receipt, err := mailer.Send(context.Background(), &pigeon.Email{
		From:    pigeon.Address{Name: "Your app", Email: "hello@example.com"},
		To:      []pigeon.Address{{Name: "Ada", Email: "ada@example.com"}},
		Cc:      []pigeon.Address{{Email: "team@example.com"}},
		Bcc:     []pigeon.Address{{Email: "archive@example.com"}},
		ReplyTo: pigeon.Address{Email: "support@example.com"},
		Subject: "Your receipt",
		Text:    "Thanks, Ada. Your order is confirmed.",
		HTML:    "<p>Thanks, Ada. Your order is confirmed.</p>",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("accepted:", receipt.ID)
	// Output: accepted: 1
}
