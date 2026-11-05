// Package message contains the provider-neutral values shared by the facade and adapters.
package message

import (
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Address is an ASCII email address with an optional Unicode display name.
// Email must contain only the addr-spec, such as ada@example.com.
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// String formats the address for a mail header.
func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// Email is a single message, with one or both body representations.
// A message needs a From, at least one To, a subject, and at most 50 recipients
// across To, Cc and Bcc. ReplyTo's zero value means no Reply-To header.
// Do not modify the message or its recipient slices while Send is running.
type Email struct {
	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	ReplyTo Address
	Subject string
	Text    string
	HTML    string
}

// Receipt identifies an accepted submission. It does not confirm delivery.
// ID may be empty when SendGrid accepts a request without X-Message-Id.
type Receipt struct {
	ID string
}

// DeliveryError reports a provider's unsuccessful HTTP response.
// Message and Code contain untrusted provider diagnostics and may include
// personal data. Error deliberately excludes them so ordinary logging is safe.
type DeliveryError struct {
	Provider   string
	StatusCode int
	Code       string
	Message    string
}

func (e *DeliveryError) Error() string {
	return "pigeon: " + e.Provider + " returned HTTP " + strconv.Itoa(e.StatusCode)
}

// AddressStrings formats a recipient slice for string-based provider APIs.
func AddressStrings(addresses []Address) []string {
	if len(addresses) == 0 {
		return nil
	}
	result := make([]string, len(addresses))
	for i, address := range addresses {
		result[i] = address.String()
	}
	return result
}

// Validate enforces the portable message contract before any provider is called.
func Validate(email *Email) error {
	if email == nil {
		return errors.New("pigeon: email is nil")
	}
	if len(email.To) == 0 {
		return errors.New("pigeon: at least one to recipient is required")
	}
	if len(email.To) > 50 || len(email.Cc) > 50-len(email.To) || len(email.Bcc) > 50-len(email.To)-len(email.Cc) {
		return errors.New("pigeon: at most 50 recipients are allowed")
	}
	if !validAddress(email.From) {
		return errors.New("pigeon: invalid from address")
	}
	for _, recipients := range [...][]Address{email.To, email.Cc, email.Bcc} {
		for i, address := range recipients {
			if !validAddress(address) {
				return fmt.Errorf("pigeon: invalid recipient at index %d", i)
			}
		}
	}
	if email.ReplyTo != (Address{}) && !validAddress(email.ReplyTo) {
		return errors.New("pigeon: invalid reply-to address")
	}
	if email.Subject == "" || !validHeader(email.Subject) {
		return errors.New("pigeon: subject must be nonempty UTF-8 without control characters")
	}
	if email.Text == "" && email.HTML == "" {
		return errors.New("pigeon: a text or HTML body is required")
	}
	if !utf8.ValidString(email.Text) || !utf8.ValidString(email.HTML) {
		return errors.New("pigeon: email bodies must be valid UTF-8")
	}
	return nil
}

func validHeader(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < ' ' || r == 127 {
			return false
		}
	}
	return true
}

func validAddress(a Address) bool {
	if len(a.Email) == 0 || len(a.Email) > 254 || !validHeader(a.Name) {
		return false
	}
	for i := range len(a.Email) {
		if a.Email[i] <= ' ' || a.Email[i] >= 127 {
			return false
		}
	}
	local, domain, ok := strings.CutLast(a.Email, "@")
	if !ok || local == "" || len(local) > 64 || domain == "" {
		return false
	}
	if simpleAddress(local, domain) {
		return true
	}
	parsed, err := mail.ParseAddress(a.Email)
	return err == nil && parsed.Name == "" && parsed.Address == a.Email
}

func simpleAddress(local, domain string) bool {
	for i := range len(local) {
		c := local[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-/=?^_`{|}~", rune(c)):
		case c == '.' && i > 0 && i+1 < len(local) && local[i-1] != '.':
		default:
			return false
		}
	}
	labelStart := 0
	for i := range len(domain) {
		c := domain[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' && i > labelStart:
		case c == '.' && i > labelStart && i-labelStart <= 63 && domain[i-1] != '-':
			labelStart = i + 1
		default:
			return false
		}
	}
	return labelStart < len(domain) && len(domain)-labelStart <= 63 && domain[len(domain)-1] != '-'
}
