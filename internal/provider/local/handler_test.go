package local

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.alvadorn.org/pigeon/internal/provider"
)

func localTestRequest(handler http.Handler, method, path string, destination ...string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, http.NoBody)
	request.Host = "127.0.0.1:8025"
	if len(destination) > 0 {
		request.Header.Set("Sec-Fetch-Dest", destination[0])
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

// localTagAttributes parses the attributes of one tag's body, the text
// between the tag name and the closing bracket. The inbox template emits
// quoted constant values or numeric IDs, so entity decoding is not needed.
func localTagAttributes(tag string) map[string]string {
	attrs := make(map[string]string)
	for i := 0; i < len(tag); {
		if isLocalAttrSpace(tag[i]) {
			i++
			continue
		}
		name, value, next := localTagAttribute(tag, i)
		if name != "" {
			attrs[name] = value
		}
		i = next
	}
	return attrs
}

// localTagAttribute parses one attribute at start, returning its name, its
// value and the offset where parsing resumes.
func localTagAttribute(tag string, start int) (name, value string, next int) {
	i := start
	for i < len(tag) && tag[i] != '=' && !isLocalAttrSpace(tag[i]) {
		i++
	}
	name = tag[start:i]
	if i == len(tag) || tag[i] != '=' {
		return name, "", i
	}
	i++ // equals sign
	if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
		value, next = localQuotedValue(tag, i)
		return name, value, next
	}
	valueStart := i
	for i < len(tag) && !isLocalAttrSpace(tag[i]) {
		i++
	}
	return name, tag[valueStart:i], i
}

// localQuotedValue reads a quoted attribute value from its opening quote,
// returning the value and the offset after the closing quote.
func localQuotedValue(tag string, at int) (value string, next int) {
	quote := tag[at]
	i := at + 1
	valueStart := i
	for i < len(tag) && tag[i] != quote {
		i++
	}
	value = tag[valueStart:i]
	if i < len(tag) {
		i++ // closing quote
	}
	return value, i
}

func isLocalAttrSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n'
}

func isLocalTagBoundary(c byte) bool {
	return isLocalAttrSpace(c) || c == '>' || c == '/'
}

func hasAllAttributes(attrs, want map[string]string) bool {
	for name, value := range want {
		got, ok := attrs[name]
		if !ok || got != value {
			return false
		}
	}
	return true
}

// findLocalTag returns the attributes of the first tag element in body that
// carries every name-value pair in want, or nil when none does.
func findLocalTag(body, tag string, want map[string]string) map[string]string {
	open := "<" + tag
	for from := 0; ; {
		at := strings.Index(body[from:], open)
		if at < 0 {
			return nil
		}
		at += from
		after := at + len(open)
		from = at + 1
		if after >= len(body) || !isLocalTagBoundary(body[after]) {
			continue // a longer tag name, for example "a" inside "article"
		}
		end := strings.IndexByte(body[at:], '>')
		if end < 0 {
			return nil
		}
		attrs := localTagAttributes(body[after : at+end])
		if hasAllAttributes(attrs, want) {
			return attrs
		}
	}
}

// assertLocalTag fails when no element with the given tag carries all the
// attributes, regardless of their order in the markup.
func assertLocalTag(t *testing.T, body, description, tag string, want map[string]string) {
	t.Helper()
	if findLocalTag(body, tag, want) == nil {
		t.Errorf("%s: no <%s> with %v", description, tag, want)
	}
}

// assertNoLocalTag fails when an element with the given tag carries all the
// attributes, regardless of their order in the markup.
func assertNoLocalTag(t *testing.T, body, description, tag string, want map[string]string) {
	t.Helper()
	if findLocalTag(body, tag, want) != nil {
		t.Errorf("%s: unexpected <%s> with %v", description, tag, want)
	}
}

func assertDetailRendering(t *testing.T, body, subject, abbreviated string) {
	t.Helper()
	for _, want := range []string{
		`<title>` + abbreviated + ` · Pigeon — Inbox local</title>`,
		`<span class="c-message-subject">` + abbreviated + `</span>`,
		`<h1 id="c-subject">` + subject + `</h1>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page missing %q", want)
		}
	}
	assertLocalTag(t, body, "HTML preview", "iframe",
		map[string]string{"class": "mail-preview", "title": "Prévia HTML da mensagem"})
	if subject != abbreviated && strings.Count(body, subject) != 1 {
		t.Errorf("full subject should appear only in the detail heading")
	}
}

func assertQueueRendering(t *testing.T, body, subject, abbreviated string) {
	t.Helper()
	if !strings.Contains(body, `<span class="c-message-subject">`+abbreviated+`</span>`) {
		t.Error("unselected message missing abbreviated queue subject")
	}
	if subject != abbreviated && strings.Contains(body, subject) {
		t.Error("unselected message exposed its full subject")
	}
}

func TestLocalSubjectRendering(t *testing.T) {
	prefix := strings.Repeat("界", subjectMaxRunes-2) + "🙂"
	for _, tc := range []struct {
		name, subject, abbreviated string
	}{
		{"below limit", prefix, prefix},
		{"at limit", prefix + "é", prefix + "é"},
		{"above limit", prefix + "é終", prefix + "é…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbox := localTestInbox(t, 2)
			email := localTestEmail(tc.subject)
			email.HTML = "<p>preview</p>"
			receipt, err := inbox.Send(t.Context(), email)
			if err != nil {
				t.Fatal(err)
			}
			response := localTestRequest(inbox, http.MethodGet, "/"+receipt.ID)
			if response.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", response.Code)
			}
			assertDetailRendering(t, response.Body.String(), tc.subject, tc.abbreviated)

			if _, err := inbox.Send(t.Context(), localTestEmail("other")); err != nil {
				t.Fatal(err)
			}
			other := localTestRequest(inbox, http.MethodGet, "/").Body.String()
			assertQueueRendering(t, other, tc.subject, tc.abbreviated)
		})
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
	assertLocalTag(t, htmlDetail.Body.String(), "preview isolation", "iframe",
		map[string]string{"sandbox": "", "src": "./" + receipt.ID + "/html"})
	if htmlDetail.Code != http.StatusOK {
		t.Fatalf("html detail status = %d", htmlDetail.Code)
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

// The pager anchors are distinct from the queue links: swapping Previous and
// Next, dropping the pager, or changing the default selection must fail here.
func TestLocalPagerAnchors(t *testing.T) {
	inbox := localTestInbox(t, 3)
	var ids []string
	for _, subject := range []string{"oldest", "middle", "newest"} {
		receipt, err := inbox.Send(t.Context(), localTestEmail(subject))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, receipt.ID)
	}
	oldest, middle, newest := ids[0], ids[1], ids[2]
	for _, tc := range []struct {
		name, selected, previous, next string
	}{
		{"newest has no previous", newest, "", middle},
		{"middle has both", middle, newest, oldest},
		{"oldest has no next", oldest, middle, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := localTestRequest(inbox, http.MethodGet, "/"+tc.selected).Body.String()
			if tc.previous == "" {
				assertNoLocalTag(t, body, tc.name, "a",
					map[string]string{"aria-label": "Mensagem anterior"})
			} else {
				assertLocalTag(t, body, tc.name, "a",
					map[string]string{"href": "./" + tc.previous, "aria-label": "Mensagem anterior"})
			}
			if tc.next == "" {
				assertNoLocalTag(t, body, tc.name, "a",
					map[string]string{"aria-label": "Próxima mensagem"})
			} else {
				assertLocalTag(t, body, tc.name, "a",
					map[string]string{"href": "./" + tc.next, "aria-label": "Próxima mensagem"})
			}
		})
	}
}

// The queue lists at most the newest 100 summaries; a message outside the
// window stays viewable by ID and navigable through the pager.
func TestLocalQueueListsNewestHundred(t *testing.T) {
	const retained = 120
	inbox, err := New(&provider.Config{LocalCapacity: retained})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= retained; i++ {
		if _, err := inbox.Send(t.Context(), localTestEmail(fmt.Sprintf("m%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	// Local IDs are sequential from 1, so message 1 is the oldest.
	response := localTestRequest(inbox, http.MethodGet, "/1")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `<h1 id="c-subject">m1</h1>`) {
		t.Fatalf("outside-window selection: status=%d body=%s", response.Code, body)
	}
	assertLocalTag(t, body, "oldest previous pager", "a",
		map[string]string{"href": "./2", "aria-label": "Mensagem anterior"})
	assertNoLocalTag(t, body, "oldest hides next pager", "a",
		map[string]string{"aria-label": "Próxima mensagem"})
	if got := strings.Count(body, `class="c-message"`); got != localQueueLimit {
		t.Errorf("queue entries = %d, want %d", got, localQueueLimit)
	}
	if !strings.Contains(body, `href="./120"`) || !strings.Contains(body, `href="./21"`) ||
		strings.Contains(body, `href="./20"`) || strings.Contains(body, `aria-current="true"`) {
		t.Errorf("queue window keeps only the newest: %s", body)
	}
	if !strings.Contains(body, `<span class="c-count">120</span>`) || !strings.Contains(body, "120 de 120") {
		t.Errorf("counters must show the full queue: %s", body)
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
	assertLocalTag(t, body, path+" selected queue entry", "a",
		map[string]string{"href": "./" + selectedID, "aria-current": "true"})
	for _, id := range []string{olderID, newerID} {
		if !strings.Contains(body, `href="./`+id+`"`) {
			t.Errorf("%s missing relative navigation to %s", path, id)
		}
	}
	// The pager anchors carry aria-labels, which queue links never do.
	if selectedID == olderID {
		assertLocalTag(t, body, path+" previous pager", "a",
			map[string]string{"href": "./" + newerID, "aria-label": "Mensagem anterior"})
		assertNoLocalTag(t, body, path+" oldest hides next pager", "a",
			map[string]string{"aria-label": "Próxima mensagem"})
	} else {
		assertLocalTag(t, body, path+" next pager", "a",
			map[string]string{"href": "./" + olderID, "aria-label": "Próxima mensagem"})
		assertNoLocalTag(t, body, path+" newest hides previous pager", "a",
			map[string]string{"aria-label": "Mensagem anterior"})
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
	// A plain request selects the default tab: html when both bodies exist,
	// text when only text does.
	plain := localTestRequest(inbox, http.MethodGet, base).Body.String()
	switch {
	case html != "":
		assertLocalTag(t, plain, "plain request selects html tab", "a",
			map[string]string{"data-format": "html", "aria-selected": "true"})
	case text != "":
		assertLocalTag(t, plain, "plain request selects text tab", "a",
			map[string]string{"data-format": "text", "aria-selected": "true"})
		assertNoLocalTag(t, plain, "text-only renders no html tab link", "a",
			map[string]string{"data-format": "html"})
	default:
		assertNoLocalTag(t, plain, "bodyless message has no format tabs", "a",
			map[string]string{"data-format": "html"})
		assertNoLocalTag(t, plain, "bodyless message has no format tabs", "a",
			map[string]string{"data-format": "text"})
	}
	if text != "" {
		response := localTestRequest(inbox, http.MethodGet, base+"?format=text")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), text) {
			t.Errorf("text format: status=%d body=%s", response.Code, response.Body.String())
		}
		assertLocalTag(t, response.Body.String(), "text tab", "a", map[string]string{
			"data-format": "text", "aria-controls": "panel-text", "aria-selected": "true",
		})
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
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), html) {
		t.Errorf("HTML format not isolated: status=%d body=%s", response.Code, response.Body.String())
	}
	assertLocalTag(t, response.Body.String(), "isolated preview", "iframe",
		map[string]string{"sandbox": "", "src": "./" + id + "/html"})
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
	for _, directive := range []string{
		"default-src 'none'", "script-src 'self'", "frame-src 'self'", "frame-ancestors 'none'",
	} {
		if !strings.Contains(get.Header().Get("Content-Security-Policy"), directive) {
			t.Errorf("CSP missing %q: %q", directive, get.Header().Get("Content-Security-Policy"))
		}
	}
}
