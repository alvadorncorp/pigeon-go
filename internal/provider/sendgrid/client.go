package sendgrid

import (
	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/internal/httptransport"
)

type sendGridSender struct {
	http   httptransport.Client
	apiKey string
}

// New constructs a SendGrid sender using the configured API key and HTTP client.
func New(config *provider.Config) (*sendGridSender, error) {
	if err := httptransport.RequireAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	h, err := httptransport.New(config, "https://api.sendgrid.com")
	if err != nil {
		return nil, err
	}
	return &sendGridSender{http: h, apiKey: config.APIKey}, nil
}
