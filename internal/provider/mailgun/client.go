package mailgun

import (
	"encoding/base64"
	"errors"
	"strings"

	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/internal/httptransport"
)

type mailgunSender struct {
	http httptransport.Client
	url  string
	auth string
}

// New creates a sender for a Mailgun domain using the configured API key.
func New(config provider.Config) (*mailgunSender, error) {
	if err := httptransport.RequireAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	if !validMailgunDomain(config.Domain) {
		return nil, errors.New("mailgun: invalid domain")
	}
	h, err := httptransport.New(config, "https://api.mailgun.net")
	if err != nil {
		return nil, err
	}
	return &mailgunSender{
		http: h,
		url:  h.BaseURL() + "/v3/" + config.Domain + "/messages",
		auth: "Basic " + base64.StdEncoding.EncodeToString([]byte("api:"+config.APIKey)),
	}, nil
}

// validMailgunDomain accepts DNS hostnames only, preventing path and URL injection.
func validMailgunDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	for label := range strings.SplitSeq(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
