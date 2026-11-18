package message

import (
	"fmt"
	"math"
	"net/mail"
	"strings"
	"testing"
)

func validEmail() Email {
	return Email{
		From: Address{Email: "sender@example.com"}, To: []Address{{Email: "reader@example.com"}},
		Subject: "Hello", Text: "A letter.",
	}
}

func TestValidateRejectsUnportableMessages(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Email)
	}{
		{"missing from", func(e *Email) { e.From = Address{} }},
		{"missing to", func(e *Email) { e.To = nil; e.Bcc = []Address{{Email: "reader@example.com"}} }},
		{"invalid cc", func(e *Email) { e.Cc = []Address{{Email: "not-an-address"}} }},
		{"embedded display name", func(e *Email) { e.From.Email = "Ada <ada@example.com>" }},
		{
			"address header injection",
			func(e *Email) { e.To[0].Email = "reader@example.com\r\nBcc: victim@example.com" },
		},
		{"name header injection", func(e *Email) { e.From.Name = "Ada\nBcc: victim@example.com" }},
		{"subject header injection", func(e *Email) { e.Subject = "Hello\r\nBcc: victim@example.com" }},
		{"non-ASCII address", func(e *Email) { e.From.Email = "josé@example.com" }},
		{"missing reply address", func(e *Email) { e.ReplyTo.Name = "Help" }},
		{"long local part", func(e *Email) { e.From.Email = strings.Repeat("a", 65) + "@example.com" }},
		{"missing subject", func(e *Email) { e.Subject = "" }},
		{"missing body", func(e *Email) { e.Text = "" }},
		{"invalid body UTF-8", func(e *Email) { e.HTML = "\xff" }},
		{"invalid name UTF-8", func(e *Email) { e.From.Name = "\xff" }},
		{"too many recipients", func(e *Email) { e.Bcc = make([]Address, 50) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := validEmail()
			tc.edit(&e)
			if err := Validate(&e); err == nil {
				t.Fatal("invalid message was accepted")
			}
		})
	}
	if err := Validate(nil); err == nil {
		t.Fatal("nil email was accepted")
	}
}

func TestValidatePortableBoundary(t *testing.T) {
	e := validEmail()
	e.From.Name = "João, Silva"
	e.Subject = "Olá, 世界"
	e.Text = ""
	e.HTML = "<p>Olá</p>"
	e.ReplyTo = Address{Email: "help+letters@example.com"}
	e.Bcc = make([]Address, 49)
	for i := range e.Bcc {
		e.Bcc[i] = Address{Email: fmt.Sprintf("copy%d@example.com", i)}
	}
	if err := Validate(&e); err != nil {
		t.Fatal(err)
	}
}

func recipients(prefix string, n int) []Address {
	list := make([]Address, n)
	for i := range list {
		list[i] = Address{Email: fmt.Sprintf("%s%d@example.com", prefix, i)}
	}
	return list
}

func TestValidateCombinedRecipientLimit(t *testing.T) {
	for _, tc := range []struct {
		to, cc, bcc int
		ok          bool
	}{{1, 25, 24, true}, {1, 25, 25, false}, {50, 0, 0, true}, {51, 0, 0, false}, {1, 50, 0, false}} {
		e := validEmail()
		e.To, e.Cc, e.Bcc = recipients("to", tc.to), recipients("cc", tc.cc), recipients("bcc", tc.bcc)
		if got := Validate(&e) == nil; got != tc.ok {
			t.Errorf("to=%d cc=%d bcc=%d accepted = %v, want %v", tc.to, tc.cc, tc.bcc, got, tc.ok)
		}
	}
}

func TestValidateRejectsRepeatedRecipients(t *testing.T) {
	for name, edit := range map[string]func(*Email){
		"to and bcc":       func(e *Email) { e.Bcc = []Address{{Email: "reader@example.com"}} },
		"twice in to":      func(e *Email) { e.To = append(e.To, Address{Email: "reader@example.com"}) },
		"case-insensitive": func(e *Email) { e.Cc = []Address{{Email: "READER@example.com"}} },
		"cc and bcc": func(e *Email) {
			e.Cc = []Address{{Email: "extra@example.com"}}
			e.Bcc = []Address{{Email: "extra@example.com"}}
		},
	} {
		e := validEmail()
		edit(&e)
		if Validate(&e) == nil {
			t.Errorf("%s: repeated recipient accepted", name)
		}
	}
}

// Addresses at the validator's acceptance boundary. The 254-byte case is the
// longest accepted address: its local part and every domain label sit at their
// own limits, so only the total-length rule decides it. The 255-byte case is
// rejected by that rule alone.
var (
	addressAcceptCases = []string{
		"sender@example.com", "FIRST.Last+tag@sub.example.com", "!#$%&'*+-/=?^_`{|}~@example.com",
		"user@xn--bcher-kva.example", strings.Repeat("a", 64) + "@example.com",
		"user@" + strings.Repeat("a", 63) + ".com",
		strings.Repeat("a", 64) + "@" + strings.Repeat("a", 63) + "." +
			strings.Repeat("a", 63) + "." + strings.Repeat("a", 61),
	}
	addressRejectCases = []string{
		`"quoted"@example.com`, "user@[127.0.0.1]", ".user@example.com", "user.@example.com", "u..ser@example.com",
		"user@.example.com", "user@example.com.", "user@example..com", "user@-example.com", "user@example-.com",
		"user@example_domain.com", "user@ex!ample.com", "user@@example.com", "@example.com", "user@",
		"Ada <ada@example.com>", "user@example.com\r\nBcc: victim@example.com", "josé@example.com",
		"user@bücher.de", "user@example.com\x00", "", strings.Repeat("a", 65) + "@example.com",
		"user@" + strings.Repeat("a", 64) + ".com", "a@" + strings.Repeat("a", 253),
		strings.Repeat("a", 64) + "@" + strings.Repeat("a", 63) + "." +
			strings.Repeat("a", 63) + "." + strings.Repeat("a", 62),
	}
)

func TestValidateAddressHostnameRules(t *testing.T) {
	for _, address := range addressAcceptCases {
		if !validAddress(Address{Email: address}) {
			t.Errorf("rejected valid address %q", address)
		}
	}
	for _, address := range addressRejectCases {
		if validAddress(Address{Email: address}) {
			t.Errorf("accepted invalid address %q", address)
		}
	}
}

// FuzzValidateAddress explores validAddress, seeded with the boundary cases
// above.
func FuzzValidateAddress(f *testing.F) {
	for _, address := range addressAcceptCases {
		f.Add(address)
	}
	for _, address := range addressRejectCases {
		f.Add(address)
	}
	f.Fuzz(func(t *testing.T, address string) {
		if !validAddress(Address{Email: address}) {
			return
		}
		assertPortableAddress(t, address)
	})
}

// assertPortableAddress checks that an accepted address stays within the
// portable subset: no space or control bytes, and net/mail must parse the
// same addr-spec back out of it.
func assertPortableAddress(t *testing.T, address string) {
	t.Helper()
	for i := range len(address) {
		if c := address[i]; c <= ' ' || c == 127 {
			t.Fatalf("accepted address %q contains byte %#02x", address, c)
		}
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		t.Fatalf("net/mail rejected an accepted address %q: %v", address, err)
	}
	if parsed.Address != address {
		t.Fatalf("net/mail parsed addr-spec %q from %q", parsed.Address, address)
	}
}

func TestSizeCountsEveryString(t *testing.T) {
	email := &Email{
		From:    Address{Name: "Sender", Email: "sender@example.com"},                       // 6 + 18
		To:      []Address{{Email: "a@example.com"}, {Name: "Bee", Email: "b@example.com"}}, // 13 + 3 + 13
		Cc:      []Address{{Email: "c@example.com"}},                                        // 13
		Bcc:     []Address{{Email: "d@example.com"}},                                        // 13
		ReplyTo: Address{Name: "Reply", Email: "reply@example.com"},                         // 5 + 17
		Subject: "Hello",                                                                    // 5
		Text:    "text",                                                                     // 4
		HTML:    "<p>html</p>",                                                              // 11
	}
	if got := Size(email); got != 121 {
		t.Errorf("Size = %d, want 121", got)
	}
}

func TestSizeSaturatesInsteadOfOverflowing(t *testing.T) {
	for _, tc := range []struct{ total, piece, want int }{
		{0, 5, 5},
		{math.MaxInt, 0, math.MaxInt},
		{math.MaxInt, 7, math.MaxInt},
		{math.MaxInt - 1, 1, math.MaxInt},
		{math.MaxInt - 1, 2, math.MaxInt},
		{math.MaxInt - 5, 3, math.MaxInt - 2},
	} {
		if got := addSaturating(tc.total, tc.piece); got != tc.want {
			t.Errorf("addSaturating(%d, %d) = %d, want %d", tc.total, tc.piece, got, tc.want)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	e := validEmail()
	for _, size := range []struct {
		name string
		n    int
	}{{"small", 64}, {"typical", 4096}, {"large", 256 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			e.Text = strings.Repeat("a", size.n)
			b.ReportAllocs()
			b.SetBytes(int64(size.n))
			for b.Loop() {
				if err := Validate(&e); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
