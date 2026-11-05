package aws

import (
	"errors"
	"strings"
	"sync"

	"go.alvadorn.org/pigeon/internal/provider"
	"go.alvadorn.org/pigeon/internal/provider/internal/httptransport"
)

type sesSender struct {
	http                                httptransport.Client
	region, accessKey, secretKey, token string
	signingMu                           sync.Mutex
	signingDate                         string
	signingKey                          []byte
}

// NewSES constructs an SES v2 sender using a fixed snapshot of AWS credentials.
// Credentials are not discovered or refreshed automatically.
func NewSES(config provider.Config) (*sesSender, error) {
	if !validSESRegion(config.Region) {
		return nil, errors.New("ses: invalid region")
	}
	if config.AccessKeyID == "" || config.SecretAccessKey == "" || strings.ContainsAny(config.AccessKeyID, " \t\r\n/,=") || strings.ContainsAny(config.SessionToken, "\r\n") {
		return nil, errors.New("ses: invalid credentials")
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(config.Region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	h, err := httptransport.New(config, "https://email."+config.Region+"."+suffix)
	if err != nil {
		return nil, err
	}
	return &sesSender{http: h, region: config.Region, accessKey: config.AccessKeyID, secretKey: config.SecretAccessKey, token: config.SessionToken}, nil
}

func validSESRegion(region string) bool {
	if len(region) == 0 || len(region) > 63 || region[0] == '-' || region[len(region)-1] == '-' {
		return false
	}
	for _, c := range region {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
