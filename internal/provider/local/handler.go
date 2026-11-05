package local

import (
	"io"
	"net/http"
	"strings"

	"go.alvadorn.org/pigeon/internal/message"
)

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
