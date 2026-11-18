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
	var opts []pigeon.Option
	if opt := providerOption(kind); opt != nil {
		opts = append(opts, opt)
	}
	if baseURL := os.Getenv("PIGEON_BASE_URL"); baseURL != "" {
		opts = append(opts, pigeon.WithBaseURL(baseURL))
	}
	mailer, err := pigeon.New(kind, opts...)
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

// providerOption builds the provider-specific option for kind from the
// environment. It returns nil for local delivery, where the option is
// optional, and for an unknown kind, which New reports.
func providerOption(kind pigeon.Provider) pigeon.Option {
	apiKey := os.Getenv("PIGEON_API_KEY")
	switch kind {
	case pigeon.ProviderSES:
		return pigeon.WithSES(os.Getenv("AWS_REGION"), pigeon.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		})
	case pigeon.ProviderResend:
		return pigeon.WithResend(apiKey)
	case pigeon.ProviderMailgun:
		return pigeon.WithMailgun(apiKey, os.Getenv("PIGEON_DOMAIN"))
	case pigeon.ProviderSendGrid:
		return pigeon.WithSendGrid(apiKey)
	default:
		return nil
	}
}
