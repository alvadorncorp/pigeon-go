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
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `href="`+receipt.ID+`"`) || strings.Contains(list.Body.String(), `<script>`) || strings.Contains(list.Body.String(), `<img src=x`) {
		t.Fatalf("list escaping or relative link: %d %s", list.Code, list.Body.String())
	}
	detail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID)
	body := detail.Body.String()
	if detail.Code != http.StatusOK || strings.Contains(body, `<script>`) || !strings.Contains(body, `&lt;script&gt;`) || !strings.Contains(body, `sandbox=""`) || !strings.Contains(body, `src="`+receipt.ID+`/html"`) || !strings.Contains(body, `href="./"`) {
		t.Fatalf("detail escaping or links: %d %s", detail.Code, body)
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
	if got := localTestRequest(inbox, http.MethodGet, "/"); !strings.Contains(got.Body.String(), "No messages yet") {
		t.Fatalf("missing empty state: %s", got.Body.String())
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
