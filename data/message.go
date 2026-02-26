package data

import "time"

// Flag represents an IMAP message flag.
type Flag string

const (
	FlagSeen     Flag = `\Seen`
	FlagFlagged  Flag = `\Flagged` // "starred"
	FlagAnswered Flag = `\Answered`
	FlagDeleted  Flag = `\Deleted`
	FlagDraft    Flag = `\Draft`
)

// Address represents an email address with optional display name.
type Address struct {
	Name    string
	Address string
}

// String returns a human-readable representation of the address.
func (a Address) String() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Address
}

// Message represents a single email message.
type Message struct {
	ID          int64
	UID         uint32
	SeqNum      uint32
	MessageID   string
	InReplyTo   string
	References  []string
	Subject     string
	From        []Address
	To          []Address
	CC          []Address
	Date        time.Time
	Flags       []Flag
	Size        uint32
	Snippet     string // first ~200 chars plain text
	Body        string // lazy: populated on FetchBody
	HTMLBody    string // lazy: populated on FetchBody
	FolderName  string
	AccountName string
	Tags        []string // local only (SQLite)
	ThreadID    string   // computed by thread.BuildThreads()
}

// HasFlag returns true if the message has the given flag.
func (m *Message) HasFlag(f Flag) bool {
	for _, flag := range m.Flags {
		if flag == f {
			return true
		}
	}
	return false
}

// IsRead returns true if the message has the \Seen flag.
func (m *Message) IsRead() bool {
	return m.HasFlag(FlagSeen)
}

// IsStarred returns true if the message has the \Flagged flag.
func (m *Message) IsStarred() bool {
	return m.HasFlag(FlagFlagged)
}

// FromString returns a string representation of From addresses.
func (m *Message) FromString() string {
	if len(m.From) == 0 {
		return ""
	}
	return m.From[0].String()
}

// Thread represents a group of related messages.
type Thread struct {
	ID        string
	Subject   string     // normalized (Re:/Fwd: stripped)
	Messages  []*Message // chronological
	LastDate  time.Time
	HasUnread bool
	Starred   bool
	Tags      []string
}

// MessageCount returns the number of messages in the thread.
func (t *Thread) MessageCount() int {
	return len(t.Messages)
}

// Latest returns the most recent message in the thread.
func (t *Thread) Latest() *Message {
	if len(t.Messages) == 0 {
		return nil
	}
	return t.Messages[len(t.Messages)-1]
}
