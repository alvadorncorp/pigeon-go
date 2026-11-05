package message

import (
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
		{"address header injection", func(e *Email) { e.To[0].Email = "reader@example.com\r\nBcc: victim@example.com" }},
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
		e.Bcc[i] = Address{Email: "copy@example.com"}
	}
	if err := Validate(&e); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAddressMatchesOriginalParser(t *testing.T) {
	addresses := []string{
		"sender@example.com", "FIRST.Last+tag@sub.example.com",
		"!#$%&'*+-/=?^_`{|}~@example.com",
		`"quoted"@example.com`, `"a.b"@example.com`, "user@[127.0.0.1]",
		".user@example.com", "user.@example.com", "u..ser@example.com",
		"user@.example.com", "user@example.com.", "user@example..com",
		"user@-example.com", "user@example-.com", "user@example_domain.com",
		"user@example", "user@@example.com", "@example.com", "user@",
		"Ada <ada@example.com>", "user@example.com\r\nBcc: victim@example.com",
		"josé@example.com", "user@example.com\x00", "",
		strings.Repeat("a", 64) + "@example.com",
		strings.Repeat("a", 65) + "@example.com",
		"user@" + strings.Repeat("a", 63) + ".com",
		"user@" + strings.Repeat("a", 64) + ".com",
		"a@" + strings.Repeat("a", 252), "a@" + strings.Repeat("a", 253),
	}
	for c := byte(33); c < 127; c++ {
		addresses = append(addresses, "a"+string(c)+"b@example.com", "a@ex"+string(c)+"ample.com")
	}
	for _, address := range addresses {
		t.Run(address, func(t *testing.T) {
			want := originalAddressAccepted(address)
			e := validEmail()
			e.From.Email = address
			if got := Validate(&e) == nil; got != want {
				t.Errorf("Validate(from=%q) accepted = %v, original parser accepted = %v", address, got, want)
			}
		})
	}
}

func originalAddressAccepted(address string) bool {
	local, domain, ok := strings.CutLast(address, "@")
	want := address != "" && len(address) <= 254 && ok && local != "" && len(local) <= 64 && domain != ""
	for i := range len(address) {
		if address[i] <= ' ' || address[i] >= 127 {
			want = false
		}
	}
	parsed, err := mail.ParseAddress(address)
	return want && err == nil && parsed.Name == "" && parsed.Address == address
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
