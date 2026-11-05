package provider

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.alvadorn.org/pigeon/internal/message"
)

const localBudget = 16 << 20

//go:embed local.html
var localHTML string

//go:embed local.css
var localCSS string

//go:embed local.js
var localJS string

//go:embed local.svg
var localSVG string

var localTemplate = template.Must(template.New("inbox").Parse(localHTML))

type localMessage struct {
	ID    string
	Email message.Email
	size  int
}

type localSummary struct {
	ID      string
	From    message.Address
	To      []message.Address
	Subject string
}

type localPage struct {
	Messages []localSummary
	Selected localMessage
	Position int
	Previous string
	Next     string
	Format   string
}

// LocalInbox is a process-local, volatile development inbox. It is safe for
// concurrent sends and HTTP requests; it is not an SMTP server.
type LocalInbox struct {
	mu                  sync.RWMutex
	messages            []localMessage
	start, count, bytes int
	next                uint64
}

// NewLocal creates a bounded development inbox. A zero capacity defaults to
// 100 messages; a negative capacity is invalid. Retained content is also
// limited to 16 MiB, with the oldest messages evicted first.
func NewLocal(cfg Config) (*LocalInbox, error) {
	capacity := cfg.LocalCapacity
	if capacity < 0 {
		return nil, fmt.Errorf("local capacity must not be negative")
	}
	if capacity == 0 {
		capacity = 100
	}
	return &LocalInbox{messages: make([]localMessage, capacity)}, nil
}

// Send stores a snapshot of the email and returns its local, monotonic ID.
// The caller must not mutate the email until Send returns.
func (inbox *LocalInbox) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return message.Receipt{}, err
	}
	if email == nil {
		return message.Receipt{}, fmt.Errorf("local email is nil")
	}
	size, ok := localSize(email)
	if !ok {
		return message.Receipt{}, fmt.Errorf("local email exceeds 16 MiB retention limit")
	}
	copyEmail := *email
	copyEmail.To = slices.Clone(email.To)
	copyEmail.Cc = slices.Clone(email.Cc)
	copyEmail.Bcc = slices.Clone(email.Bcc)

	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return message.Receipt{}, err
	}
	for inbox.count > 0 && (inbox.count == len(inbox.messages) || inbox.bytes+size > localBudget) {
		old := &inbox.messages[inbox.start]
		inbox.bytes -= old.size
		*old = localMessage{}
		inbox.start = (inbox.start + 1) % len(inbox.messages)
		inbox.count--
	}
	inbox.next++
	id := strconv.FormatUint(inbox.next, 10)
	index := (inbox.start + inbox.count) % len(inbox.messages)
	inbox.messages[index] = localMessage{ID: id, Email: copyEmail, size: size}
	inbox.count++
	inbox.bytes += size
	return message.Receipt{ID: id}, nil
}

func localSize(email *message.Email) (int, bool) {
	size := 0
	add := func(s string) bool {
		if len(s) > localBudget-size {
			return false
		}
		size += len(s)
		return true
	}
	address := func(a message.Address) bool { return add(a.Name) && add(a.Email) }
	if !address(email.From) || !address(email.ReplyTo) || !add(email.Subject) || !add(email.Text) || !add(email.HTML) {
		return 0, false
	}
	for _, group := range [][]message.Address{email.To, email.Cc, email.Bcc} {
		for _, a := range group {
			if !address(a) {
				return 0, false
			}
		}
	}
	return size, true
}

// ServeHTTP serves a read-only development inbox. Mount it with StripPrefix;
// the stripped prefix should end at the inbox root, including its trailing slash.
func (inbox *LocalInbox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	switch path {
	case "local.css", "local.js", "local.svg":
		var content, contentType string
		switch path {
		case "local.css":
			content, contentType = localCSS, "text/css; charset=utf-8"
		case "local.js":
			content, contentType = localJS, "text/javascript; charset=utf-8"
		case "local.svg":
			content, contentType = localSVG, "image/svg+xml"
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, content)
		}
		return
	}
	id, suffix, hasSuffix := strings.Cut(path, "/")
	if hasSuffix && (id == "" || suffix != "html") {
		http.NotFound(w, r)
		return
	}
	if hasSuffix {
		inbox.mu.RLock()
		var item localMessage
		for i := range inbox.count {
			candidate := inbox.messages[(inbox.start+i)%len(inbox.messages)]
			if candidate.ID == id {
				item = candidate
				break
			}
		}
		inbox.mu.RUnlock()
		if item.ID == "" {
			http.NotFound(w, r)
			return
		}
		// The parent frame policy blocks navigation that sandbox alone permits.
		if r.Header.Get("Sec-Fetch-Dest") != "iframe" {
			http.Error(w, "HTML preview requires the inbox iframe", http.StatusForbidden)
			return
		}
		if item.Email.HTML == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			if _, err := io.WriteString(w, item.Email.HTML); err != nil {
				return
			}
		}
		return
	}
	inbox.mu.RLock()
	page := localPage{Messages: make([]localSummary, inbox.count)}
	for i := range inbox.count {
		candidate := inbox.messages[(inbox.start+inbox.count-1-i)%len(inbox.messages)]
		page.Messages[i] = localSummary{ID: candidate.ID, From: candidate.Email.From, To: candidate.Email.To, Subject: candidate.Email.Subject}
		if candidate.ID == id || (id == "" && i == 0) {
			page.Selected = candidate
			page.Position = i + 1
		}
	}
	inbox.mu.RUnlock()
	if id != "" && page.Selected.ID == "" {
		http.NotFound(w, r)
		return
	}
	item := page.Selected
	if page.Position > 1 {
		page.Previous = page.Messages[page.Position-2].ID
	}
	if page.Position > 0 && page.Position < len(page.Messages) {
		page.Next = page.Messages[page.Position].ID
	}
	if item.Email.HTML != "" {
		page.Format = "html"
	} else if item.Email.Text != "" {
		page.Format = "text"
	}
	if requested := r.URL.Query().Get("format"); (requested == "html" && item.Email.HTML != "") || (requested == "text" && item.Email.Text != "") {
		page.Format = requested
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodGet {
		_ = localTemplate.ExecuteTemplate(w, "inbox", page)
	}
}
