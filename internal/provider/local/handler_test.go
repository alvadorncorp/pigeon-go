package local

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func localTestRequest(handler http.Handler, method, path string, destination ...string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, http.NoBody)
	if len(destination) > 0 {
		request.Header.Set("Sec-Fetch-Dest", destination[0])
	}
	handler.ServeHTTP(recorder, request)
	return recorder
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
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `href="./`+receipt.ID+`"`) ||
		strings.Contains(list.Body.String(), `<script>`) || strings.Contains(list.Body.String(), `<img src=x`) {
		t.Fatalf("list escaping or relative link: %d %s", list.Code, list.Body.String())
	}
	detail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"?format=text")
	body := detail.Body.String()
	if detail.Code != http.StatusOK || strings.Contains(body, `<script>`) ||
		!strings.Contains(body, `&lt;script&gt;`) || !strings.Contains(body, receipt.ID) {
		t.Fatalf("detail escaping or links: %d %s", detail.Code, body)
	}
	htmlDetail := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"?format=html")
	if !strings.Contains(htmlDetail.Body.String(), `sandbox=""`) ||
		!strings.Contains(htmlDetail.Body.String(), `src="./`+receipt.ID+`/html"`) {
		t.Fatalf("preview isolation or relative link: %s", htmlDetail.Body.String())
	}
	preview := localTestRequest(handler, http.MethodGet, "/mail/"+receipt.ID+"/html", "iframe")
	if preview.Code != http.StatusOK || preview.Body.String() != email.HTML {
		t.Fatalf("preview: %d %q", preview.Code, preview.Body.String())
	}
	checkLocalSafety(t, list, detail, preview)
	checkLocalMethods(t, handler, receipt.ID)
}

func checkLocalSafety(t *testing.T, list, detail, preview *httptest.ResponseRecorder) {
	t.Helper()
	csp := preview.Header().Get("Content-Security-Policy")
	for _, part := range []string{"sandbox", "default-src 'none'", "style-src 'unsafe-inline'", "form-action 'none'"} {
		if !strings.Contains(csp, part) {
			t.Errorf("preview CSP %q missing %q", csp, part)
		}
	}
	for _, response := range []*httptest.ResponseRecorder{list, detail, preview} {
		if response.Header().Get("Cache-Control") != "no-store" ||
			response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("missing safety headers: %v", response.Header())
		}
	}
}

func checkLocalMethods(t *testing.T, handler http.Handler, id string) {
	t.Helper()
	for _, path := range []string{"/mail/", "/mail/" + id, "/mail/" + id + "/html"} {
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
				t.Errorf("%s destination %q exposed raw HTML: status=%d body=%q",
					method, destination, response.Code, response.Body.String())
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
		checkLocalSelection(t, handler, tc.path, tc.selected, tc.other, tc.selectedID, older.ID, newer.ID,
			first.Subject, second.Subject)
	}
	third, err := inbox.Send(t.Context(), localTestEmail("latest subject"))
	if err != nil {
		t.Fatal(err)
	}
	if got := localTestRequest(handler, http.MethodGet, "/mail/"+older.ID).Code; got != http.StatusNotFound {
		t.Errorf("evicted selection status = %d, want 404", got)
	}
	retained := localTestRequest(handler, http.MethodGet, "/mail/"+newer.ID)
	if retained.Code != http.StatusOK || !strings.Contains(retained.Body.String(), second.Text) ||
		!strings.Contains(retained.Body.String(), `href="./`+third.ID+`"`) ||
		strings.Contains(retained.Body.String(), first.Subject) {
		t.Errorf("retained selection after eviction: status=%d body=%s", retained.Code, retained.Body.String())
	}
}

func checkLocalSelection(
	t *testing.T, handler http.Handler, path, selected, other, selectedID, olderID, newerID, first, second string,
) {
	t.Helper()
	response := localTestRequest(handler, http.MethodGet, path)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, selected) || strings.Contains(body, other) {
		t.Errorf("%s selected wrong body: status=%d body=%s", path, response.Code, body)
	}
	if !strings.Contains(body, `href="./`+selectedID+`" aria-current="true"`) {
		t.Errorf("%s missing selected navigation state", path)
	}
	for _, id := range []string{olderID, newerID} {
		if !strings.Contains(body, `href="./`+id+`"`) {
			t.Errorf("%s missing relative navigation to %s", path, id)
		}
	}
	if !strings.Contains(body, first) || !strings.Contains(body, second) ||
		strings.Index(body, `href="./`+newerID+`"`) > strings.LastIndex(body, `href="./`+olderID+`"`) {
		t.Errorf("%s missing newest-first inbox: %s", path, body)
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
			checkLocalFormats(t, inbox, base, receipt.ID, tc.text, tc.html)
		})
	}
}

func checkLocalFormats(t *testing.T, inbox *LocalInbox, base, id, text, html string) {
	t.Helper()
	if text != "" {
		response := localTestRequest(inbox, http.MethodGet, base+"?format=text")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), text) {
			t.Errorf("text format: status=%d body=%s", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `data-format="text" aria-controls="panel-text" aria-selected="true"`) {
			t.Errorf("text tab not selected: %s", response.Body.String())
		}
	}
	response := localTestRequest(inbox, http.MethodGet, base+"?format=html")
	preview := localTestRequest(inbox, http.MethodGet, base+"/html", "iframe")
	if html == "" {
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `<iframe`) ||
			preview.Code != http.StatusNotFound {
			t.Errorf("absent HTML: detail=%d preview=%d body=%s", response.Code, preview.Code, response.Body.String())
		}
		return
	}
	checkLocalHTMLFormat(t, response, preview, id, html)
}

func checkLocalHTMLFormat(
	t *testing.T, response, preview *httptest.ResponseRecorder, id, html string,
) {
	t.Helper()
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `sandbox=""`) ||
		!strings.Contains(response.Body.String(), `src="./`+id+`/html"`) ||
		strings.Contains(response.Body.String(), html) {
		t.Errorf("HTML format not isolated: status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `data-format="html" aria-controls="panel-html" aria-selected="true"`) {
		t.Errorf("HTML tab not selected: %s", response.Body.String())
	}
	if preview.Code != http.StatusOK || preview.Body.String() != html {
		t.Errorf("HTML preview: status=%d body=%q", preview.Code, preview.Body.String())
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
			checkLocalAssetMethods(t, handler, "/mail/"+asset.path, asset.contentType)
		})
	}
}

func TestLocalStaticAssetInvalidRoutes(t *testing.T) {
	handler := http.StripPrefix("/mail", localTestInbox(t, 1))
	for _, path := range []string{"/mail/missing.css", "/mail/local.css/extra", "/mail/assets/local.css"} {
		if got := localTestRequest(handler, http.MethodGet, path).Code; got != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, got)
		}
	}
}

func checkLocalAssetMethods(t *testing.T, handler http.Handler, path, contentType string) {
	t.Helper()
	get := localTestRequest(handler, http.MethodGet, path)
	if get.Code != http.StatusOK || get.Body.Len() == 0 || get.Header().Get("Content-Type") != contentType {
		t.Errorf("GET: status=%d content-type=%q bytes=%d", get.Code, get.Header().Get("Content-Type"), get.Body.Len())
	}
	checkLocalAssetHeaders(t, get)
	head := localTestRequest(handler, http.MethodHead, path)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != contentType {
		t.Errorf("HEAD: status=%d content-type=%q bytes=%d",
			head.Code, head.Header().Get("Content-Type"), head.Body.Len())
	}
	post := localTestRequest(handler, http.MethodPost, path)
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: status=%d allow=%q", post.Code, post.Header().Get("Allow"))
	}
}

func checkLocalAssetHeaders(t *testing.T, get *httptest.ResponseRecorder) {
	t.Helper()
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
}
