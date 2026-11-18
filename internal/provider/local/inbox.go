package local

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.alvadorn.org/pigeon/internal/message"
	"go.alvadorn.org/pigeon/internal/provider"
)

const localDefaultMaxBytes = 16 << 20

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
	capacity            int
	start, count, bytes int
	maxBytes            int
	next                uint64
}

// New creates a bounded development inbox. A zero capacity defaults to
// 100 messages; a negative capacity is invalid. Retained content is also
// limited: a zero LocalMaxBytes defaults to 16 MiB and negative values are
// invalid. The oldest messages are evicted first.
func New(cfg *provider.Config) (*LocalInbox, error) {
	capacity := cfg.LocalCapacity
	if capacity < 0 {
		return nil, fmt.Errorf("local capacity must not be negative")
	}
	if capacity == 0 {
		capacity = 100
	}
	maxBytes := cfg.LocalMaxBytes
	if maxBytes < 0 {
		return nil, fmt.Errorf("local max bytes must not be negative")
	}
	if maxBytes == 0 {
		maxBytes = localDefaultMaxBytes
	}
	// The ring grows on demand, so a large capacity costs nothing until used.
	return &LocalInbox{capacity: capacity, maxBytes: maxBytes}, nil
}

// Send stores a snapshot of the email and returns its local, monotonic ID.
// The caller must not mutate the email until Send returns.
func (inbox *LocalInbox) Send(ctx context.Context, email *message.Email) (message.Receipt, error) {
	size := message.Size(email)
	if size > inbox.maxBytes {
		return message.Receipt{}, fmt.Errorf("local email exceeds %d byte retention limit", inbox.maxBytes)
	}
	copyEmail := *email
	copyEmail.From = localCloneAddress(email.From)
	copyEmail.ReplyTo = localCloneAddress(email.ReplyTo)
	copyEmail.To = localCloneAddresses(email.To)
	copyEmail.Cc = localCloneAddresses(email.Cc)
	copyEmail.Bcc = localCloneAddresses(email.Bcc)
	copyEmail.Subject = strings.Clone(email.Subject)
	copyEmail.Text = strings.Clone(email.Text)
	copyEmail.HTML = strings.Clone(email.HTML)

	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return message.Receipt{}, err
	}
	for inbox.count > 0 && (inbox.count == inbox.capacity || size > inbox.maxBytes-inbox.bytes) {
		old := &inbox.messages[inbox.start]
		inbox.bytes -= old.size
		*old = localMessage{}
		inbox.start = (inbox.start + 1) % len(inbox.messages)
		inbox.count--
	}
	inbox.next++
	id := strconv.FormatUint(inbox.next, 10)
	stored := localMessage{ID: id, Email: copyEmail, size: size}
	switch {
	case inbox.count < len(inbox.messages):
		inbox.messages[(inbox.start+inbox.count)%len(inbox.messages)] = stored
	case inbox.start == 0:
		inbox.messages = append(inbox.messages, stored)
	default:
		// Byte eviction moved start before the ring reached capacity: unwrap it.
		inbox.messages = slices.Concat(
			inbox.messages[inbox.start:],
			inbox.messages[:inbox.start],
			[]localMessage{stored},
		)
		inbox.start = 0
	}
	inbox.count++
	inbox.bytes += size
	return message.Receipt{ID: id}, nil
}

// localCloneAddress owns the bytes of an address snapshot; empty strings are
// shared, not copied.
func localCloneAddress(a message.Address) message.Address {
	a.Name = strings.Clone(a.Name)
	a.Email = strings.Clone(a.Email)
	return a
}

func localCloneAddresses(addresses []message.Address) []message.Address {
	if addresses == nil {
		return nil
	}
	clones := make([]message.Address, len(addresses))
	for i, a := range addresses {
		clones[i] = localCloneAddress(a)
	}
	return clones
}
