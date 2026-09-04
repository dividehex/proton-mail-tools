package service

import (
	"context"
	"fmt"

	"proton-mail-tools/internal/mail"
)

// MarkSpam moves messages into the Spam mailbox. Proton treats this as a
// report, so the sender's future mail is filtered too. Spam is purged like
// Trash, so the operation is gated with deletion.
func (s *Service) MarkSpam(ctx context.Context, mailbox string, uids []uint32) (MoveResult, error) {
	if !s.opts.AllowDelete {
		return MoveResult{}, fmt.Errorf("%w: marking spam (Spam is purged like Trash; ALLOW_DELETE)", ErrDisabled)
	}
	if err := requireMessages(mailbox, uids); err != nil {
		return MoveResult{}, err
	}
	spam, err := s.mailboxByRole(ctx, mail.RoleSpam)
	if err != nil {
		return MoveResult{}, err
	}
	if mailbox == spam {
		return MoveResult{}, fmt.Errorf("%w: messages in %q are already spam", mail.ErrInvalidInput, spam)
	}
	return s.guardedMove(ctx, mailbox, uids, spam)
}

// MarkNotSpam moves messages from Spam back to INBOX, which Proton records as
// "not spam" for the sender.
func (s *Service) MarkNotSpam(ctx context.Context, mailbox string, uids []uint32) (MoveResult, error) {
	return s.restoreFrom(ctx, mail.RoleSpam, mailbox, uids, "")
}

// Restore moves messages out of Trash into INBOX, or into the given folder.
// Proton does not remember where a trashed message came from.
func (s *Service) Restore(ctx context.Context, mailbox string, uids []uint32, destination string) (MoveResult, error) {
	return s.restoreFrom(ctx, mail.RoleTrash, mailbox, uids, destination)
}

// restoreFrom moves messages out of the mailbox with the given role. An empty
// destination means INBOX; otherwise it must be an existing folder that is not
// Trash or Spam.
func (s *Service) restoreFrom(ctx context.Context, role, mailbox string, uids []uint32, destination string) (MoveResult, error) {
	if err := requireMessages(mailbox, uids); err != nil {
		return MoveResult{}, err
	}
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return MoveResult{}, err
	}
	source, ok := findByRole(boxes, role)
	if !ok {
		return MoveResult{}, fmt.Errorf("%w: no mailbox with role %q", mail.ErrMailboxNotFound, role)
	}
	if mailbox != source.Name {
		return MoveResult{}, fmt.Errorf("%w: this tool only restores from %q, not %q; use move_messages instead", mail.ErrInvalidInput, source.Name, mailbox)
	}
	target, err := restoreTarget(boxes, destination)
	if err != nil {
		return MoveResult{}, err
	}
	return s.guardedMove(ctx, source.Name, uids, target.Name)
}

// restoreTarget resolves where restored messages go: INBOX by default, else a
// folder that can hold them.
func restoreTarget(boxes []mail.Mailbox, destination string) (mail.Mailbox, error) {
	if destination == "" {
		inbox, ok := findByRole(boxes, mail.RoleInbox)
		if !ok {
			return mail.Mailbox{}, fmt.Errorf("%w: no mailbox with role %q", mail.ErrMailboxNotFound, mail.RoleInbox)
		}
		return inbox, nil
	}
	mb, ok := findByName(boxes, destination)
	if !ok {
		return mail.Mailbox{}, fmt.Errorf("%w: %q", mail.ErrMailboxNotFound, destination)
	}
	switch {
	case mb.Kind == mail.KindLabel:
		return mail.Mailbox{}, fmt.Errorf("%w: %q is a label; restore into a folder, then use label_messages", mail.ErrInvalidInput, destination)
	case mb.Role == mail.RoleTrash || mb.Role == mail.RoleSpam:
		return mail.Mailbox{}, fmt.Errorf("%w: cannot restore into %q", mail.ErrInvalidInput, destination)
	}
	return mb, nil
}

func findByRole(boxes []mail.Mailbox, role string) (mail.Mailbox, bool) {
	for _, mb := range boxes {
		if mb.Role == role {
			return mb, true
		}
	}
	return mail.Mailbox{}, false
}

func findByName(boxes []mail.Mailbox, name string) (mail.Mailbox, bool) {
	for _, mb := range boxes {
		if mb.Name == name {
			return mb, true
		}
	}
	return mail.Mailbox{}, false
}
