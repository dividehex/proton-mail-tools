package mail

import "context"

// Store reads and mutates messages in a mailbox (the IMAP side).
type Store interface {
	ListMailboxes(ctx context.Context) ([]Mailbox, error)
	Search(ctx context.Context, q Query) ([]Summary, error)
	Get(ctx context.Context, mailbox string, uid uint32) (*Message, error)
	UpdateFlags(ctx context.Context, mailbox string, uids []uint32, update FlagUpdate) error
	Move(ctx context.Context, mailbox string, uids []uint32, destination string) error
}

// Sender submits an outgoing message for delivery (the SMTP side).
type Sender interface {
	Send(ctx context.Context, msg Outgoing) error
}
