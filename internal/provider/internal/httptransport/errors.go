package httptransport

import (
	"encoding/json"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

func responseError(kind provider.Kind, status int, body []byte) *message.DeliveryError {
	result := &message.DeliveryError{Provider: string(kind), StatusCode: status}
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
		result.Code = detail.Code
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
