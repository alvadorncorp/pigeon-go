package local

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

const localBudget = 16 << 20

type localMessage struct {
	ID    string
	Email message.Email
	size  int
}

// LocalInbox is a process-local, volatile development inbox. It is safe for
// concurrent sends and HTTP requests; it is not an SMTP server.
type LocalInbox struct {
	mu                  sync.RWMutex
	messages            []localMessage
	start, count, bytes int
	next                uint64
}

// New creates a bounded development inbox. A zero capacity defaults to
// 100 messages; a negative capacity is invalid. Retained content is also
// limited to 16 MiB, with the oldest messages evicted first.
func New(cfg provider.Config) (*LocalInbox, error) {
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
