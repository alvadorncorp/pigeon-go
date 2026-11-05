package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
)

func localTestEmail(subject string) *message.Email {
	return &message.Email{
		From:    message.Address{Name: "Sender", Email: "sender@example.com"},
		To:      []message.Address{{Name: "Recipient", Email: "recipient@example.com"}},
		Subject: subject,
		Text:    "plain text",
	}
}

func localTestInbox(t *testing.T, capacity int) *LocalInbox {
	t.Helper()
	inbox, err := NewLocal(Config{LocalCapacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	return inbox
}

func localTestRequest(handler http.Handler, method, path string, destination ...string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	if len(destination) > 0 {
		request.Header.Set("Sec-Fetch-Dest", destination[0])
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestLocalRetentionAndSnapshot(t *testing.T) {
	inbox := localTestInbox(t, 2)
	original := localTestEmail("first")
	original.Cc = []message.Address{{Email: "cc@example.com"}}
	original.Bcc = []message.Address{{Email: "bcc@example.com"}}
	first, err := inbox.Send(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	original.To[0].Email = "changed@example.com"
	original.Cc[0].Email = "changed@example.com"
	original.Bcc[0].Email = "changed@example.com"
	original.Subject = "changed subject"
	page := localTestRequest(inbox, http.MethodGet, "/"+first.ID)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "recipient@example.com") || !strings.Contains(page.Body.String(), "cc@example.com") || !strings.Contains(page.Body.String(), "bcc@example.com") || strings.Contains(page.Body.String(), "changed") {
		t.Fatalf("snapshot changed: status=%d body=%s", page.Code, page.Body.String())
	}
	second, err := inbox.Send(t.Context(), localTestEmail("second"))
	if err != nil {
		t.Fatal(err)
	}
	third, err := inbox.Send(t.Context(), localTestEmail("third"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "1" || second.ID != "2" || third.ID != "3" {
		t.Fatalf("non-monotonic IDs: %q %q %q", first.ID, second.ID, third.ID)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+first.ID).Code; got != http.StatusNotFound {
		t.Fatalf("oldest status = %d, want 404", got)
	}
	list := localTestRequest(inbox, http.MethodGet, "/").Body.String()
	if strings.Contains(list, "first") || !strings.Contains(list, "second") || !strings.Contains(list, "third") || strings.Index(list, "third") > strings.Index(list, "second") {
		t.Fatalf("unexpected retained list: %s", list)
	}
}

func TestLocalByteBudgetAndOversized(t *testing.T) {
	inbox := localTestInbox(t, 3)
	large := localTestEmail("large")
	large.Text = strings.Repeat("x", 9<<20)
	first, err := inbox.Send(t.Context(), large)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inbox.Send(t.Context(), large)
	if err != nil {
		t.Fatal(err)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+first.ID).Code; got != http.StatusNotFound {
		t.Fatalf("byte eviction status = %d", got)
	}
	oversized := localTestEmail("oversized")
	oversized.From.Name = strings.Repeat("x", localBudget)
	if _, err := inbox.Send(t.Context(), oversized); err == nil {
		t.Fatal("oversized message accepted")
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+second.ID).Code; got != http.StatusOK {
		t.Fatalf("rejection evicted existing message: %d", got)
	}
	if inbox.bytes > localBudget || inbox.count != 1 {
		t.Fatalf("retention invariant: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
}

func TestLocalHTTPMountAndIsolation(t *testing.T) {
	inbox := localTestInbox(t, 10)
	email := localTestEmail(`<script>alert("subject")</script>`)
	email.From.Name = `<img src=x onerror=alert(1)>`
	email.Text = `<script>alert("text")</script>`
	email.HTML = `<script>alert("html")</script><img src="https://example.com/track">`
	receipt, err := inbox.Send(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.StripPrefix("/mail", inbox)
	list := localTestRequest(handler, http.MethodGet, "/mail/")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `href="./`+receipt.ID+`"`) || strings.Contains(list.Body.String(), `<script>`) || strings.Contains(list.Body.String(), `<img src=x`) {
		t.Fatalf("list escaping or relative link: %d %s", list.Code, list.Body.String())
	}
	detail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"?format=text")
	body := detail.Body.String()
	if detail.Code != http.StatusOK || strings.Contains(body, `<script>`) || !strings.Contains(body, `&lt;script&gt;`) || !strings.Contains(body, receipt.ID) {
		t.Fatalf("detail escaping or links: %d %s", detail.Code, body)
	}
	htmlDetail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"?format=html")
	if !strings.Contains(htmlDetail.Body.String(), `sandbox=""`) || !strings.Contains(htmlDetail.Body.String(), `src="./`+receipt.ID+`/html"`) {
		t.Fatalf("preview isolation or relative link: %s", htmlDetail.Body.String())
	}
	preview := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"/html", "iframe")
	if preview.Code != http.StatusOK || preview.Body.String() != email.HTML {
		t.Fatalf("preview: %d %q", preview.Code, preview.Body.String())
	}
	csp := preview.Header().Get("Content-Security-Policy")
	for _, part := range []string{"sandbox", "default-src 'none'", "style-src 'unsafe-inline'", "form-action 'none'"} {
		if !strings.Contains(csp, part) {
			t.Errorf("preview CSP %q missing %q", csp, part)
		}
	}
	for _, response := range []*httptest.ResponseRecorder{list, detail, preview} {
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("missing safety headers: %v", response.Header())
		}
	}
	for _, path := range []string{"/mail/", "/mail/" + receipt.ID, "/mail/" + receipt.ID + "/html"} {
		head := localTestRequest(handler, http.MethodHead, path, "iframe")
		if head.Code != http.StatusOK || head.Body.Len() != 0 {
			t.Errorf("HEAD %s: status=%d body=%q", path, head.Code, head.Body.String())
		}
	}
	post := localTestRequest(handler, http.MethodPost, "/mail/")
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: status=%d allow=%q", post.Code, post.Header().Get("Allow"))
	}
	if got := localTestRequest(handler, http.MethodGet, "/mail/unknown").Code; got != http.StatusNotFound {
		t.Errorf("unknown message status = %d", got)
	}
}

func TestLocalPreviewRequiresIframe(t *testing.T) {
	inbox := localTestInbox(t, 1)
	email := localTestEmail("private preview")
	email.HTML = `<a href="https://example.com/track">private canary</a>`
	receipt, err := inbox.Send(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.StripPrefix("/mail", inbox)
	path := "/mail/" + receipt.ID + "/html"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, destination := range []string{"", "document", "empty", "image", "object"} {
			response := localTestRequest(handler, method, path, destination)
			if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private canary") {
				t.Errorf("%s destination %q exposed raw HTML: status=%d body=%q", method, destination, response.Code, response.Body.String())
			}
		}
	}
	preview := localTestRequest(handler, http.MethodGet, path, "iframe")
	if preview.Code != http.StatusOK || preview.Body.String() != email.HTML {
		t.Fatalf("legitimate preview failed: %d %q", preview.Code, preview.Body.String())
	}
	if !strings.Contains(preview.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatal("preview must restrict its parent origin")
	}
	detail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID)
	if !strings.Contains(detail.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("inbox must reject embedding in other pages")
	}
}

func TestLocalConcurrentSendAndRead(t *testing.T) {
	inbox := localTestInbox(t, 20)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range 50 {
				if _, err := inbox.Send(t.Context(), localTestEmail(fmt.Sprintf("%d-%d", worker, i))); err != nil {
					t.Error(err)
					return
				}
				if got := localTestRequest(inbox, http.MethodGet, "/").Code; got != http.StatusOK {
					t.Errorf("list status = %d", got)
					return
				}
			}
		})
	}
	wg.Wait()
	if inbox.count != 20 || inbox.bytes > localBudget || inbox.next != 400 {
		t.Fatalf("retention after concurrency: count=%d bytes=%d next=%d", inbox.count, inbox.bytes, inbox.next)
	}
}

func TestLocalErrors(t *testing.T) {
	if _, err := NewLocal(Config{LocalCapacity: -1}); err == nil {
		t.Fatal("negative capacity accepted")
	}
	inbox := localTestInbox(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := inbox.Send(ctx, localTestEmail("cancelled")); err != context.Canceled {
		t.Fatalf("cancelled send: %v", err)
	}
	if _, err := inbox.Send(t.Context(), nil); err == nil {
		t.Fatal("nil email accepted")
	}
	empty := localTestRequest(inbox, http.MethodGet, "/")
	if empty.Code != http.StatusOK || strings.Contains(empty.Body.String(), `<iframe`) {
		t.Fatalf("empty inbox: status=%d body=%s", empty.Code, empty.Body.String())
	}
	if got := localTestRequest(inbox, http.MethodGet, "/1").Code; got != http.StatusNotFound {
		t.Fatalf("empty inbox detail status = %d, want 404", got)
	}
}

func TestLocalInboxSelectionAndNavigation(t *testing.T) {
	inbox := localTestInbox(t, 2)
	first := localTestEmail("older subject")
	first.Text = "older body canary"
	older, err := inbox.Send(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	second := localTestEmail("newer subject")
	second.Text = "newer body canary"
	newer, err := inbox.Send(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.StripPrefix("/mail", inbox)
	for _, tc := range []struct {
		path, selected, other, selectedID string
	}{
		{"/mail/", second.Text, first.Text, newer.ID},
		{"/mail/" + older.ID, first.Text, second.Text, older.ID},
		{"/mail/" + newer.ID, second.Text, first.Text, newer.ID},
	} {
		response := localTestRequest(handler, http.MethodGet, tc.path)
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, tc.selected) || strings.Contains(body, tc.other) {
			t.Errorf("%s selected wrong body: status=%d body=%s", tc.path, response.Code, body)
		}
		if !strings.Contains(body, `href="./`+tc.selectedID+`" aria-current="true"`) {
			t.Errorf("%s missing selected navigation state", tc.path)
		}
		for _, id := range []string{older.ID, newer.ID} {
			if !strings.Contains(body, `href="./`+id+`"`) {
				t.Errorf("%s missing relative navigation to %s", tc.path, id)
			}
		}
		if !strings.Contains(body, first.Subject) || !strings.Contains(body, second.Subject) || strings.Index(body, `href="./`+newer.ID+`"`) > strings.LastIndex(body, `href="./`+older.ID+`"`) {
			t.Errorf("%s missing newest-first inbox: %s", tc.path, body)
		}
	}
	third, err := inbox.Send(t.Context(), localTestEmail("latest subject"))
	if err != nil {
		t.Fatal(err)
	}
	if got := localTestRequest(handler, http.MethodGet, "/mail/"+older.ID).Code; got != http.StatusNotFound {
		t.Errorf("evicted selection status = %d, want 404", got)
	}
	retained := localTestRequest(handler, http.MethodGet, "/mail/"+newer.ID)
	if retained.Code != http.StatusOK || !strings.Contains(retained.Body.String(), second.Text) || !strings.Contains(retained.Body.String(), `href="./`+third.ID+`"`) || strings.Contains(retained.Body.String(), first.Subject) {
		t.Errorf("retained selection after eviction: status=%d body=%s", retained.Code, retained.Body.String())
	}
}

func TestLocalOptionalBodyFormats(t *testing.T) {
	for _, tc := range []struct {
		name, text, html string
	}{
		{"text only", "plain body canary", ""},
		{"html only", "", "<p>html body canary</p>"},
		{"both", "plain body canary", "<p>html body canary</p>"},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbox := localTestInbox(t, 1)
			email := localTestEmail(tc.name)
			email.Text, email.HTML = tc.text, tc.html
			receipt, err := inbox.Send(t.Context(), email)
			if err != nil {
				t.Fatal(err)
			}
			base := "/" + receipt.ID
			if tc.text != "" {
				response := localTestRequest(inbox, http.MethodGet, base+"?format=text")
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), tc.text) {
					t.Errorf("text format: status=%d body=%s", response.Code, response.Body.String())
				}
				if !strings.Contains(response.Body.String(), `data-format="text" aria-controls="panel-text" aria-selected="true"`) {
					t.Errorf("text tab not selected: %s", response.Body.String())
				}
			}
			response := localTestRequest(inbox, http.MethodGet, base+"?format=html")
			preview := localTestRequest(inbox, http.MethodGet, base+"/html", "iframe")
			if tc.html != "" {
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `sandbox=""`) || !strings.Contains(response.Body.String(), `src="./`+receipt.ID+`/html"`) || strings.Contains(response.Body.String(), tc.html) {
					t.Errorf("HTML format not isolated: status=%d body=%s", response.Code, response.Body.String())
				}
				if !strings.Contains(response.Body.String(), `data-format="html" aria-controls="panel-html" aria-selected="true"`) {
					t.Errorf("HTML tab not selected: %s", response.Body.String())
				}
				if preview.Code != http.StatusOK || preview.Body.String() != tc.html {
					t.Errorf("HTML preview: status=%d body=%q", preview.Code, preview.Body.String())
				}
			} else if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `<iframe`) || preview.Code != http.StatusNotFound {
				t.Errorf("absent HTML: detail=%d preview=%d body=%s", response.Code, preview.Code, response.Body.String())
			}
		})
	}
}

func TestLocalStaticAssets(t *testing.T) {
	handler := http.StripPrefix("/mail", localTestInbox(t, 1))
	for _, asset := range []struct{ path, contentType string }{
		{"local.css", "text/css; charset=utf-8"},
		{"local.js", "text/javascript; charset=utf-8"},
		{"local.svg", "image/svg+xml"},
	} {
		t.Run(asset.path, func(t *testing.T) {
			path := "/mail/" + asset.path
			get := localTestRequest(handler, http.MethodGet, path)
			if get.Code != http.StatusOK || get.Body.Len() == 0 || get.Header().Get("Content-Type") != asset.contentType {
				t.Errorf("GET: status=%d content-type=%q bytes=%d", get.Code, get.Header().Get("Content-Type"), get.Body.Len())
			}
			for _, header := range []struct{ name, value string }{
				{"Cache-Control", "no-store"},
				{"X-Content-Type-Options", "nosniff"},
			} {
				if got := get.Header().Get(header.name); got != header.value {
					t.Errorf("%s = %q, want %q", header.name, got, header.value)
				}
			}
			for _, directive := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'none'"} {
				if !strings.Contains(get.Header().Get("Content-Security-Policy"), directive) {
					t.Errorf("CSP missing %q: %q", directive, get.Header().Get("Content-Security-Policy"))
				}
			}
			head := localTestRequest(handler, http.MethodHead, path)
			if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != asset.contentType {
				t.Errorf("HEAD: status=%d content-type=%q bytes=%d", head.Code, head.Header().Get("Content-Type"), head.Body.Len())
			}
			post := localTestRequest(handler, http.MethodPost, path)
			if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("POST: status=%d allow=%q", post.Code, post.Header().Get("Allow"))
			}
		})
	}
	for _, path := range []string{"/mail/missing.css", "/mail/local.css/extra", "/mail/assets/local.css"} {
		if got := localTestRequest(handler, http.MethodGet, path).Code; got != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, got)
		}
	}
}

func BenchmarkLocalSend(b *testing.B) {
	inbox, err := NewLocal(Config{})
	if err != nil {
		b.Fatal(err)
	}
	email := localTestEmail("benchmark")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := inbox.Send(b.Context(), email); err != nil {
			b.Fatal(err)
		}
	}
}
