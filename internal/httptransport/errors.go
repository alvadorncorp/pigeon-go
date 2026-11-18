package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func responseError(kind provider.Kind, resp *http.Response, body []byte) *message.DeliveryError {
	result := &message.DeliveryError{
		Provider: string(kind), StatusCode: resp.StatusCode, Code: amazonErrorType(resp.Header),
	}
	var detail struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Type    string `json:"__type"`
		Code    string `json:"code"`
		Errors  []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &detail) == nil {
		result.Message = detail.Message
		if result.Code == "" {
			result.Code = detail.Code
		}
		if result.Code == "" {
			result.Code = detail.Name
		}
		if result.Code == "" {
			result.Code = detail.Type
		}
		if result.Message == "" && len(detail.Errors) > 0 {
			result.Message = detail.Errors[0].Message
		}
	}
	return result
}

// amazonErrorType returns the code AWS restJson1 services such as SES v2 put in
// X-Amzn-Errortype, formatted as "Code:namespace" or "namespace#Code".
func amazonErrorType(header http.Header) string {
	code, _, _ := strings.Cut(header.Get("X-Amzn-Errortype"), ":")
	if _, after, found := strings.Cut(code, "#"); found {
		return after
	}
	return code
}
