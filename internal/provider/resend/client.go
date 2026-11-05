package resend

import (
	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/internal/httptransport"
)

type resendSender struct {
	http   httptransport.Client
	apiKey string
}

// New constructs a Resend sender using the configured API key and HTTP client.
func New(config provider.Config) (*resendSender, error) {
	if err := httptransport.RequireAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	h, err := httptransport.New(config, "https://api.resend.com")
	if err != nil {
		return nil, err
	}
	return &resendSender{http: h, apiKey: config.APIKey}, nil
}
