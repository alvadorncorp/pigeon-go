package aws

import (
	"errors"
	"strings"
	"sync"

	"go.alvadorn.org/pigeon/internal/httptransport"
	"go.alvadorn.org/pigeon/internal/provider"
)

type sesSender struct {
	http                                httptransport.Client
	region, accessKey, secretKey, token string
	signingMu                           sync.Mutex
	signingDate                         string
	signingKey                          []byte
}

// New constructs an SES v2 sender using a fixed snapshot of AWS credentials.
// Credentials are not discovered or refreshed automatically.
func New(config *provider.Config) (*sesSender, error) {
	if !validSESRegion(config.Region) {
		return nil, errors.New("ses: invalid region")
	}
	if httptransport.RequireAPIKey(config.AccessKeyID) != nil || config.SecretAccessKey == "" ||
		strings.ContainsAny(config.AccessKeyID, "/,=") ||
		(config.SessionToken != "" && httptransport.RequireAPIKey(config.SessionToken) != nil) {
		return nil, errors.New("ses: invalid credentials")
	}
	h, err := httptransport.New(config, "https://email."+config.Region+".amazonaws.com")
	if err != nil {
		return nil, err
	}
	return &sesSender{
		http: h, region: config.Region, accessKey: config.AccessKeyID,
		secretKey: config.SecretAccessKey, token: config.SessionToken,
	}, nil
}

// validSESRegion accepts DNS-safe region names. SES has no endpoint in the
// China (cn-) partition, so those are rejected instead of failing at send time.
func validSESRegion(region string) bool {
	if region == "" || len(region) > 63 || region[0] == '-' || region[len(region)-1] == '-' ||
		strings.HasPrefix(region, "cn-") {
		return false
	}
	for _, c := range region {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
