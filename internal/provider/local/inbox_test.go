package local

import (
	"context"
	"fmt"
	"net/http"
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

func TestLocalCustomMaxBytesBudget(t *testing.T) {
	const retainedCount = 2
	fits := localTestEmail("fits")
	budget := retainedCount * message.Size(fits)
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
	if inbox.bytes != budget {
		t.Fatalf("bytes = %d, want %d", inbox.bytes, budget)
	}
	if _, err := inbox.Send(t.Context(), fits); err != nil {
		t.Fatal(err)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+first.ID).Code; got != http.StatusNotFound {
		t.Fatalf("custom budget eviction status = %d", got)
	}
	if got := localTestRequest(inbox, http.MethodGet, "/"+second.ID).Code; got != http.StatusOK {
		t.Fatalf("survivor status = %d", got)
	}
	if inbox.bytes != budget || inbox.count != retainedCount {
		t.Fatalf("retention invariant: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
	oversized := localTestEmail("oversized")
	oversized.Subject = strings.Repeat("x", budget+1)
	if _, err := inbox.Send(t.Context(), oversized); err == nil {
		t.Fatal("oversized message accepted")
	}
	if inbox.bytes != budget || inbox.count != retainedCount {
		t.Fatalf("rejection changed retention: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
	exact := localTestEmail("exact")
	exact.Text = ""
	exact.Text = strings.Repeat("x", budget-message.Size(exact))
	if _, err := inbox.Send(t.Context(), exact); err != nil {
		t.Fatalf("exact-fit message rejected: %v", err)
	}
	if inbox.bytes != budget || inbox.count != 1 {
		t.Fatalf("exact-fit invariant: bytes=%d count=%d", inbox.bytes, inbox.count)
	}
}

func TestLocalSnapshotOwnsStrings(t *testing.T) {
	inbox := localTestInbox(t, 1)
	email := localTestEmail("subject")
	sent, err := inbox.Send(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	page := localTestRequest(inbox, http.MethodGet, "/"+sent.ID)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "subject") {
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
