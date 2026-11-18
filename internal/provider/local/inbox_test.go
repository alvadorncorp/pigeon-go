package local

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
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
	inbox, err := New(&provider.Config{LocalCapacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	return inbox
}

// localPaddedEmail returns an email whose retained size is exactly size; it
// must exceed the retained size of the bare test email.
func localPaddedEmail(size int) *message.Email {
	email := localTestEmail("")
	email.Subject = strings.Repeat("x", size-localRetainedSize(email))
	return email
}

// sendLocalPadded sends padded emails of the given retained sizes into a
// fresh inbox capped by budget and returns it.
func sendLocalPadded(t *testing.T, budget int, sizes []int) *LocalInbox {
	t.Helper()
	inbox, err := New(&provider.Config{LocalCapacity: 10, LocalMaxBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range sizes {
		if _, err := inbox.Send(t.Context(), localPaddedEmail(size)); err != nil {
			t.Fatal(err)
		}
	}
	return inbox
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
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "recipient@example.com") ||
		!strings.Contains(page.Body.String(), "cc@example.com") ||
		!strings.Contains(page.Body.String(), "bcc@example.com") || strings.Contains(page.Body.String(), "changed") {
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
	checkLocalRetainedList(t, inbox)
}

func checkLocalRetainedList(t *testing.T, inbox *LocalInbox) {
	t.Helper()
	list := localTestRequest(inbox, http.MethodGet, "/").Body.String()
	if strings.Contains(list, "first") || !strings.Contains(list, "second") ||
		!strings.Contains(list, "third") || strings.Index(list, "third") > strings.Index(list, "second") {
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
	oversized.From.Name = strings.Repeat("x", localDefaultMaxBytes)
	if _, err := inbox.Send(t.Context(), oversized); err == nil {
		t.Fatal("oversized message accepted")
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+second.ID).Code; got != http.StatusOK {
		t.Fatalf("rejection evicted existing message: %d", got)
	}
	if inbox.bytes > localDefaultMaxBytes || inbox.count != 1 {
		t.Fatalf("retention invariant: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
}

func TestLocalMaxBytesConfig(t *testing.T) {
	if _, err := New(&provider.Config{LocalMaxBytes: -1}); err == nil {
		t.Fatal("negative LocalMaxBytes accepted")
	}
	defaultInbox, err := New(&provider.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if defaultInbox.maxBytes != localDefaultMaxBytes {
		t.Fatalf("default maxBytes = %d, want %d", defaultInbox.maxBytes, localDefaultMaxBytes)
	}
}

func assertRetention(t *testing.T, inbox *LocalInbox, budget, count int) {
	t.Helper()
	if inbox.bytes != budget || inbox.count != count {
		t.Fatalf("retention invariant: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
}

func TestLocalCustomMaxBytesBudget(t *testing.T) {
	const retainedCount = 2
	fits := localTestEmail("fits")
	budget := retainedCount * localRetainedSize(fits)
	inbox, err := New(&provider.Config{LocalCapacity: retainedCount + 1, LocalMaxBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	first, err := inbox.Send(t.Context(), fits)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inbox.Send(t.Context(), fits)
	if err != nil {
		t.Fatal(err)
	}
	assertRetention(t, inbox, budget, retainedCount)
	if _, err := inbox.Send(t.Context(), fits); err != nil {
		t.Fatal(err)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+first.ID).Code; got != http.StatusNotFound {
		t.Fatalf("custom budget eviction status = %d", got)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+second.ID).Code; got != http.StatusOK {
		t.Fatalf("survivor status = %d", got)
	}
	assertRetention(t, inbox, budget, retainedCount)
	oversized := localTestEmail("oversized")
	oversized.Subject = strings.Repeat("x", budget+1)
	if _, err := inbox.Send(t.Context(), oversized); err == nil {
		t.Fatal("oversized message accepted")
	}
	assertRetention(t, inbox, budget, retainedCount)
	exact := localTestEmail("exact")
	exact.Text = "" // pad against the empty-text retained size so the charge lands on budget
	exact.Text = strings.Repeat("x", budget-localRetainedSize(exact))
	if _, err := inbox.Send(t.Context(), exact); err != nil {
		t.Fatalf("exact-fit message rejected: %v", err)
	}
	assertRetention(t, inbox, budget, 1)
}

func TestLocalSnapshotOwnsStrings(t *testing.T) {
	inbox := localTestInbox(t, 1)
	email := localTestEmail("subject")
	sent, err := inbox.Send(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	page := localTestRequest(inbox, http.MethodGet, "/"+sent.ID)
	if page.Code != http.StatusOK ||
		!strings.Contains(page.Body.String(), `<h1 id="c-subject">subject</h1>`) {
		t.Fatalf("snapshot detail: status=%d body=%s", page.Code, page.Body.String())
	}
	email.From.Name = "changed"
	email.From.Email = "changed@example.com"
	email.To[0].Name = "changed"
	email.Subject = "changed"
	email.Text = "changed"
	email.HTML = "changed"
	if page := localTestRequest(inbox, http.MethodGet, "/"+sent.ID); page.Code != http.StatusOK ||
		strings.Contains(page.Body.String(), "changed") {
		t.Fatalf("snapshot mutated: body=%s", page.Body.String())
	}
}

func TestLocalConcurrentSendAndRead(t *testing.T) {
	inbox := localTestInbox(t, 20)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			sendAndReadLocal(t, inbox, worker)
		})
	}
	wg.Wait()
	if inbox.count != 20 || inbox.bytes > localDefaultMaxBytes || inbox.next != 400 {
		t.Fatalf("retention after concurrency: count=%d bytes=%d next=%d", inbox.count, inbox.bytes, inbox.next)
	}
}

func sendAndReadLocal(t *testing.T, inbox *LocalInbox, worker int) {
	t.Helper()
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
}

func TestLocalErrors(t *testing.T) {
	if _, err := New(&provider.Config{LocalCapacity: -1}); err == nil {
		t.Fatal("negative capacity accepted")
	}
	inbox := localTestInbox(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := inbox.Send(ctx, localTestEmail("cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled send: %v", err)
	}
	empty := localTestRequest(inbox, http.MethodGet, "/")
	if empty.Code != http.StatusOK || strings.Contains(empty.Body.String(), `<iframe`) {
		t.Fatalf("empty inbox: status=%d body=%s", empty.Code, empty.Body.String())
	}
	if got := localTestRequest(inbox, http.MethodGet, "/1").Code; got != http.StatusNotFound {
		t.Fatalf("empty inbox detail status = %d, want 404", got)
	}
}

func TestLocalHugeCapacityIsLazy(t *testing.T) {
	inbox, err := New(&provider.Config{LocalCapacity: math.MaxInt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.Send(t.Context(), localTestEmail("one")); err != nil {
		t.Fatal(err)
	}
	if len(inbox.messages) != 1 {
		t.Fatalf("ring holds %d slots, want 1", len(inbox.messages))
	}
}

func TestLocalRingUnwrapsAfterByteEviction(t *testing.T) {
	// Each email is padded to a retained size of exactly 2u or 1u, with a 3u
	// budget.
	const unit = 1000
	inbox, err := New(&provider.Config{LocalCapacity: 10, LocalMaxBytes: 3 * unit})
	if err != nil {
		t.Fatal(err)
	}
	// The third send evicts the 2u email by bytes, moving start below capacity;
	// the fourth fills that ring and must unwrap it before growing.
	for _, size := range []int{2 * unit, unit, unit, unit} {
		if _, err := inbox.Send(t.Context(), localPaddedEmail(size)); err != nil {
			t.Fatal(err)
		}
	}
	page := inbox.pageSnapshot("")
	var ids []string
	for _, summary := range page.Messages {
		ids = append(ids, summary.ID)
	}
	if strings.Join(ids, ",") != "4,3,2" || len(inbox.messages) != 3 || inbox.start != 0 {
		t.Fatalf(
			"ids = %v, slots = %d, start = %d; want 4,3,2 in 3 slots from 0",
			ids,
			len(inbox.messages),
			inbox.start,
		)
	}
}

func TestLocalRotateUnwrapsRing(t *testing.T) {
	for _, tc := range []struct {
		start int
		want  string
	}{
		{start: 0, want: "1,2,3,4"},
		{start: 1, want: "2,3,4,1"},
		{start: 2, want: "3,4,1,2"},
		{start: 3, want: "4,1,2,3"},
	} {
		ring := []localMessage{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}}
		localRotate(ring, tc.start)
		ids := make([]string, len(ring))
		for i, message := range ring {
			ids[i] = message.ID
		}
		if got := strings.Join(ids, ","); got != tc.want {
			t.Errorf("rotate from %d = %s, want %s", tc.start, got, tc.want)
		}
	}
}

// The ring unwraps in place: rotating a large ring must not allocate, no
// matter how many messages it holds.
func TestLocalRotateAllocatesNothing(t *testing.T) {
	const ringSize = 10000
	ring := make([]localMessage, ringSize)
	for i := range ring {
		ring[i] = localMessage{ID: strconv.Itoa(i)}
	}
	if allocs := testing.AllocsPerRun(1, func() { localRotate(ring, ringSize/2) }); allocs != 0 {
		t.Fatalf("localRotate allocated %v times", allocs)
	}
}

// localSendAllocs reports the heap allocations of one Send, measured in
// isolation after a GC.
func localSendAllocs(t *testing.T, inbox *LocalInbox, email *message.Email) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := inbox.Send(t.Context(), email); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// The unwrap may cost no more than the inherent slot growth of a plain
// append: a send through the unwrap path must not allocate extra buffers,
// for example a cloned copy of the ring taken before rotating.
func TestLocalUnwrapAvoidsExtraRingAllocation(t *testing.T) {
	const unit = 1000
	// The wrapped inbox reaches the unwrap path because byte eviction moved
	// start; the plain inbox never evicts and takes the append path. Both
	// rings are exactly full (length equals capacity), so both measured
	// sends grow the ring by one slot.
	wrapped := sendLocalPadded(t, 3*unit, []int{2 * unit, unit, unit})
	plain := sendLocalPadded(t, 30*unit, []int{2 * unit, unit, unit, unit})
	unwrap := localSendAllocs(t, wrapped, localPaddedEmail(unit))
	insert := localSendAllocs(t, plain, localPaddedEmail(unit))
	if unwrap > insert {
		t.Fatalf("unwrap send allocated %d, plain insert %d; unwrap must not copy the ring", unwrap, insert)
	}
}

func TestLocalRejectsForeignHost(t *testing.T) {
	inbox := localTestInbox(t, 1)
	for host, want := range map[string]int{
		"evil.example:8025": http.StatusForbidden, "127.0.0.1.nip.io": http.StatusForbidden,
		"localhost.evil.com": http.StatusForbidden, "evillocalhost": http.StatusForbidden, "": http.StatusForbidden,
		"127.0.0.1:8025": http.StatusOK, "[::1]:8025": http.StatusOK,
		"localhost:8025": http.StatusOK, "app.localhost": http.StatusOK, "192.168.1.10": http.StatusOK,
	} {
		request := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		request.Host = host
		recorder := httptest.NewRecorder()
		inbox.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("host %q: status = %d, want %d", host, recorder.Code, want)
		}
	}
}

func BenchmarkLocalSend(b *testing.B) {
	inbox, err := New(&provider.Config{})
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
