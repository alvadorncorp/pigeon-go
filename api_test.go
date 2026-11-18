package pigeon_test

import (
	"fmt"
	"testing"

	"go.alvadorn.org/pigeon"
)

// The public message types are type aliases of internal/message types, where
// their fields and methods are actually defined. Each assertion below
// compiles only while an alias target keeps exactly the exported fields and
// methods listed here, so renaming, retyping, adding or removing one of them
// fails this test instead of silently changing the public API.
func TestAliasedTypesKeepTheirPublicShape(t *testing.T) {
	var _ pigeon.Address = struct {
		Name  string
		Email string
	}{}
	var _ pigeon.Email = struct {
		From    pigeon.Address
		To      []pigeon.Address
		Cc      []pigeon.Address
		Bcc     []pigeon.Address
		ReplyTo pigeon.Address
		Subject string
		Text    string
		HTML    string
	}{}
	var _ pigeon.Receipt = struct {
		ID string
	}{}
	_ = (*pigeon.DeliveryError)(&struct {
		Provider   string
		StatusCode int
		Code       string
		Message    string
	}{})
	var _ fmt.Stringer = pigeon.Address{}
	var _ error = (*pigeon.DeliveryError)(nil)
	var _ fmt.Stringer = pigeon.AWSCredentials{}
	var _ fmt.GoStringer = pigeon.AWSCredentials{}
}
