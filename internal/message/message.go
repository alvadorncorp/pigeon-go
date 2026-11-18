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

// maxRecipients keeps one message within SES's per-message recipient limit.
const maxRecipients = 50

// Address is an ASCII email address with an optional Unicode display name.
// Email must contain only the addr-spec, such as ada@example.com.
type Address struct {
	Name  string
	Email string
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
// A nil error from a send always means the provider accepted the message;
// ID may be empty, for any provider, when the acceptance response omits or
// garbles its identifier.
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

// Size returns the combined byte length of every string field in the email:
// the subject, both bodies, and every address name and addr-spec, including
// From, ReplyTo and all recipients. It saturates instead of overflowing.
// Validate the email first; Size walks every recipient list it is given.
func Size(email *Email) int {
	const maxInt = int(^uint(0) >> 1)
	total := 0
	add := func(s string) {
		if piece := len(s); maxInt-total >= piece {
			total += piece
		} else {
			total = maxInt
		}
	}
	address := func(a Address) { add(a.Name); add(a.Email) }
	address(email.From)
	address(email.ReplyTo)
	add(email.Subject)
	add(email.Text)
	add(email.HTML)
	for _, group := range [][]Address{email.To, email.Cc, email.Bcc} {
		for _, a := range group {
			address(a)
		}
	}
	return total
}

// Validate enforces the portable message contract before any provider is called.
func Validate(email *Email) error {
	if email == nil {
		return errors.New("pigeon: email is nil")
	}
	if len(email.To) == 0 {
		return errors.New("pigeon: at least one to recipient is required")
	}
	if len(email.To)+len(email.Cc)+len(email.Bcc) > maxRecipients {
		return errors.New("pigeon: at most 50 recipients are allowed")
	}
	if !validAddress(email.From) {
		return errors.New("pigeon: invalid from address")
	}
	if err := validateRecipients(email); err != nil {
		return err
	}
	if email.ReplyTo != (Address{}) && !validAddress(email.ReplyTo) {
		return errors.New("pigeon: invalid reply-to address")
	}
	return validateContent(email)
}

func validateContent(email *Email) error {
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

// validateRecipients rejects invalid recipients and any address repeated across
// To, Cc and Bcc, which SendGrid refuses with HTTP 400.
func validateRecipients(email *Email) error {
	groups := [...][]Address{email.To, email.Cc, email.Bcc}
	for g, recipients := range groups {
		for i, address := range recipients {
			if !validAddress(address) {
				return fmt.Errorf("pigeon: invalid recipient at index %d", i)
			}
			if seenBefore(groups[:g+1], g, i, address.Email) {
				return fmt.Errorf("pigeon: recipient at index %d repeats an address", i)
			}
		}
	}
	return nil
}

// seenBefore reports whether email occurs, case-insensitively, before position
// i of groups[g]. It allocates nothing; with at most 50 recipients a quadratic
// scan is cheaper than a map.
func seenBefore(groups [][]Address, g, i int, email string) bool {
	for earlier, previous := range groups {
		if earlier == g {
			previous = previous[:i]
		}
		for _, other := range previous {
			if strings.EqualFold(other.Email, email) {
				return true
			}
		}
	}
	return false
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
	if a.Email == "" || len(a.Email) > 254 || !validHeader(a.Name) {
		return false
	}
	local, domain, ok := strings.CutLast(a.Email, "@")
	if !ok || local == "" || len(local) > 64 || domain == "" {
		return false
	}
	return simpleLocal(local) && simpleDomain(domain)
}

func simpleLocal(local string) bool {
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
	return true
}

func simpleDomain(domain string) bool {
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
