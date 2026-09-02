package mail

import "errors"

// Sentinel errors that callers map to user-facing outcomes.
var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrNotFound        = errors.New("message not found")
	ErrMailboxNotFound = errors.New("mailbox not found")
)
