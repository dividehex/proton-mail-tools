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
	// Remove expunges messages from mailbox only (on Proton: removes that label).
	Remove(ctx context.Context, mailbox string, uids []uint32) error
}

// Sender submits an outgoing message for delivery (the SMTP side).
type Sender interface {
	Send(ctx context.Context, msg Outgoing) error
}
