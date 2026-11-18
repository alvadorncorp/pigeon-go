package pigeon_test

import (
	"fmt"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon"
)

func TestAWSCredentialsFormattingNeverRevealsSecrets(t *testing.T) {
	// AWS's documented example values; only compared against rendered output.
	credentials := pigeon.AWSCredentials{ // #nosec G101 -- placeholder credentials, never sent anywhere
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "AQoDYXdzEPTakah",
	}
	for _, layout := range []string{"%v", "%+v", "%#v"} {
		rendered := fmt.Sprintf(layout, credentials)
		for _, secret := range []string{
			credentials.AccessKeyID,
			credentials.SecretAccessKey,
			credentials.SessionToken,
		} {
			if strings.Contains(rendered, secret) {
				t.Errorf("%s rendered the credential %q: %s", layout, secret, rendered)
			}
		}
		if !strings.Contains(rendered, "redacted") {
			t.Errorf("%s rendered %q without a redaction placeholder", layout, rendered)
		}
	}
}
