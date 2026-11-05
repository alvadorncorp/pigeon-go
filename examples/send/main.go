// Command send submits one message using configuration read by this application.
// It defaults to local delivery; a cloud provider requires explicit addresses.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.alvadorn.org/pigeon"
)

const sendTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	kind := pigeon.Provider(os.Getenv("PIGEON_PROVIDER"))
	if kind == "" {
		kind = pigeon.ProviderLocal
	}
	from, to := os.Getenv("PIGEON_FROM"), os.Getenv("PIGEON_TO")
	if kind == pigeon.ProviderLocal {
		if from == "" {
			from = "sender@example.com"
		}
		if to == "" {
			to = "reader@example.com"
		}
	} else if from == "" || to == "" {
		return fmt.Errorf("set PIGEON_FROM and PIGEON_TO before sending through a cloud provider")
	}
	mailer, err := pigeon.New(&pigeon.Config{
		Provider:        kind,
		APIKey:          os.Getenv("PIGEON_API_KEY"),
		Domain:          os.Getenv("PIGEON_DOMAIN"),
		Region:          os.Getenv("AWS_REGION"),
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		BaseURL:         os.Getenv("PIGEON_BASE_URL"),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	receipt, err := mailer.Send(ctx, &pigeon.Email{
		From:    pigeon.Address{Name: "Pigeon", Email: from},
		To:      []pigeon.Address{{Email: to}},
		Subject: "A letter from Pigeon",
		Text:    "The same message works with every configured provider.",
		HTML:    "<p>The same message works with every configured provider.</p>",
	})
	if err != nil {
		return err
	}
	fmt.Printf("Accepted by %s: %s\n", kind, receipt.ID)
	return nil
}
