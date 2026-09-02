// Package mail holds the domain model shared by every layer: what a mailbox,
// message and outgoing email look like, independent of IMAP/SMTP or HTTP.
package mail

import "time"

// Address is an RFC 5322 mailbox: an optional display name plus an email address.
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Well-known mailbox roles, resolved from IMAP SPECIAL-USE attributes or names.
const (
	RoleInbox   = "inbox"
	RoleSent    = "sent"
	RoleDrafts  = "drafts"
	RoleTrash   = "trash"
	RoleArchive = "archive"
	RoleSpam    = "spam"
	RoleStarred = "starred"
	RoleAll     = "all"
)

// Mailbox kinds: Proton folders hold a message exclusively, labels are additive tags.
const (
	KindSystem = "system"
	KindFolder = "folder"
	KindLabel  = "label"
)

// Mailbox is an IMAP folder or label. Counts are nil when the server did not report them.
type Mailbox struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Role   string  `json:"role,omitempty"`
	Total  *uint32 `json:"total_messages,omitempty"`
	Unread *uint32 `json:"unread_messages,omitempty"`
}

// Attachment describes a non-inline MIME part without carrying its content.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size_bytes"`
}

// Summary is the envelope-level view of a message, cheap enough to list in bulk.
type Summary struct {
	Mailbox        string    `json:"mailbox"`
	UID            uint32    `json:"uid"`
	Date           time.Time `json:"date"`
	From           []Address `json:"from"`
	To             []Address `json:"to"`
	Subject        string    `json:"subject"`
	MessageID      string    `json:"message_id,omitempty"`
	Read           bool      `json:"read"`
	Flagged        bool      `json:"flagged"`
	Answered       bool      `json:"answered"`
	HasAttachments bool      `json:"has_attachments"`
	Size           int64     `json:"size_bytes"`
}

// Link is a hyperlink found in the HTML body.
type Link struct {
	Text string `json:"text,omitempty"`
	URL  string `json:"url"`
}

// Message is a fully fetched message: summary plus threading headers and text body.
type Message struct {
	Summary
	Cc              []Address    `json:"cc,omitempty"`
	ReplyTo         []Address    `json:"reply_to,omitempty"`
	InReplyTo       string       `json:"in_reply_to,omitempty"`
	References      []string     `json:"references,omitempty"`
	ListUnsubscribe []string     `json:"list_unsubscribe,omitempty"`
	Body            string       `json:"body"`
	BodyTruncated   bool         `json:"body_truncated"`
	Links           []Link       `json:"links"`
	Attachments     []Attachment `json:"attachments"`
}

// Query selects messages within one mailbox. Zero-valued fields are ignored.
type Query struct {
	Mailbox    string
	UIDs       []uint32
	MessageID  string
	Text       string
	From       string
	To         string
	Subject    string
	Since      time.Time
	Before     time.Time
	UnreadOnly bool
	Limit      int
}

// Outgoing is a plain-text email ready to be submitted for delivery.
type Outgoing struct {
	From       Address
	To         []Address
	Cc         []Address
	Bcc        []Address
	Subject    string
	Body       string
	InReplyTo  string
	References []string
}

// Recipients returns every delivery address (To, Cc and Bcc).
func (o Outgoing) Recipients() []Address {
	all := make([]Address, 0, len(o.To)+len(o.Cc)+len(o.Bcc))
	all = append(all, o.To...)
	all = append(all, o.Cc...)
	return append(all, o.Bcc...)
}

// FlagUpdate changes message flags; nil fields are left untouched.
type FlagUpdate struct {
	Read     *bool
	Flagged  *bool
	Answered *bool
}

// IsEmpty reports whether the update would change nothing.
func (u FlagUpdate) IsEmpty() bool {
	return u.Read == nil && u.Flagged == nil && u.Answered == nil
}
