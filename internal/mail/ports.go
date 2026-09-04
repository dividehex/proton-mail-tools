package mail

import "context"

// Store reads and mutates messages in a mailbox (the IMAP side).
type Store interface {
	ListMailboxes(ctx context.Context) ([]Mailbox, error)
	Search(ctx context.Context, q Query) ([]Summary, error)
	Get(ctx context.Context, mailbox string, uid uint32) (*Message, error)
	UpdateFlags(ctx context.Context, mailbox string, uids []uint32, update FlagUpdate) error
	Move(ctx context.Context, mailbox string, uids []uint32, destination string) error
	// Copy adds messages to destination without removing them from mailbox
	// (on Proton this applies a label when destination is a label).
	Copy(ctx context.Context, mailbox string, uids []uint32, destination string) error
	// Remove expunges messages from mailbox only (on Proton: removes that label;
	// in Trash or Spam this deletes the message permanently).
	Remove(ctx context.Context, mailbox string, uids []uint32) error
	// AllUIDs lists every message uid in mailbox.
	AllUIDs(ctx context.Context, mailbox string) ([]uint32, error)
	// CreateMailbox creates a folder or label; name carries the Bridge prefix.
	CreateMailbox(ctx context.Context, name string) error
	// RenameMailbox renames a folder or label; both names carry the Bridge prefix.
	RenameMailbox(ctx context.Context, name, newName string) error
	// DeleteMailbox removes a folder or label.
	DeleteMailbox(ctx context.Context, name string) error
}

// Sender submits an outgoing message for delivery (the SMTP side).
type Sender interface {
	Send(ctx context.Context, msg Outgoing) error
}
