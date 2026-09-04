package service

import (
	"context"
	"fmt"
	"strings"

	"proton-mail-tools/internal/mail"
)

// CreateMailbox creates a folder or label. name may be bare ("Receipts") or
// already carry the Bridge prefix for kind ("Labels/Receipts").
func (s *Service) CreateMailbox(ctx context.Context, kind, name string) (mail.Mailbox, error) {
	full, err := qualifiedName(kind, name)
	if err != nil {
		return mail.Mailbox{}, err
	}
	if err := s.requireAbsent(ctx, full); err != nil {
		return mail.Mailbox{}, err
	}
	if err := s.store.CreateMailbox(ctx, full); err != nil {
		return mail.Mailbox{}, err
	}
	return mail.Mailbox{Name: full, Kind: kind}, nil
}

// RenameMailbox renames a folder or label. mailbox is the exact existing name;
// newName may be bare or prefixed, and keeps the mailbox's kind.
func (s *Service) RenameMailbox(ctx context.Context, mailbox, newName string) (mail.Mailbox, error) {
	mb, err := s.requireUserMailbox(ctx, mailbox)
	if err != nil {
		return mail.Mailbox{}, err
	}
	full, err := qualifiedName(mb.Kind, newName)
	if err != nil {
		return mail.Mailbox{}, err
	}
	if full == mb.Name {
		return mail.Mailbox{}, fmt.Errorf("%w: new name is the same as the current name", mail.ErrInvalidInput)
	}
	if err := s.requireAbsent(ctx, full); err != nil {
		return mail.Mailbox{}, err
	}
	if err := s.store.RenameMailbox(ctx, mb.Name, full); err != nil {
		return mail.Mailbox{}, err
	}
	return mail.Mailbox{Name: full, Kind: mb.Kind}, nil
}

// DeleteMailbox removes a label, or an empty folder. Folders that still hold
// messages are refused so nothing is deleted or relocated implicitly.
func (s *Service) DeleteMailbox(ctx context.Context, mailbox string) (mail.Mailbox, error) {
	if !s.opts.AllowDelete {
		return mail.Mailbox{}, fmt.Errorf("%w: deleting mailboxes (ALLOW_DELETE)", ErrDisabled)
	}
	mb, err := s.requireUserMailbox(ctx, mailbox)
	if err != nil {
		return mail.Mailbox{}, err
	}
	if mb.Kind == mail.KindFolder {
		uids, err := s.store.AllUIDs(ctx, mb.Name)
		if err != nil {
			return mail.Mailbox{}, err
		}
		if len(uids) > 0 {
			return mail.Mailbox{}, fmt.Errorf("%w: folder %q still holds %d messages; move or trash them first", mail.ErrInvalidInput, mb.Name, len(uids))
		}
	}
	if err := s.store.DeleteMailbox(ctx, mb.Name); err != nil {
		return mail.Mailbox{}, err
	}
	return mb, nil
}

// qualifiedName validates kind and name and returns the Bridge mailbox name.
func qualifiedName(kind, name string) (string, error) {
	prefix := mail.KindPrefix(kind)
	if prefix == "" {
		return "", fmt.Errorf("%w: kind must be %q or %q", mail.ErrInvalidInput, mail.KindFolder, mail.KindLabel)
	}
	name = strings.TrimSpace(name)
	if other := mail.KindOf(name); other != mail.KindSystem && other != kind {
		return "", fmt.Errorf("%w: %q is a %s name, not a %s", mail.ErrInvalidInput, name, other, kind)
	}
	name = strings.Trim(strings.TrimPrefix(name, prefix), "/")
	if name == "" {
		return "", fmt.Errorf("%w: name is required", mail.ErrInvalidInput)
	}
	if kind == mail.KindLabel && strings.Contains(name, "/") {
		return "", fmt.Errorf("%w: labels cannot be nested; %q contains '/'", mail.ErrInvalidInput, name)
	}
	return prefix + name, nil
}

// requireUserMailbox finds mailbox by exact name and rejects system mailboxes.
func (s *Service) requireUserMailbox(ctx context.Context, name string) (mail.Mailbox, error) {
	if strings.TrimSpace(name) == "" {
		return mail.Mailbox{}, fmt.Errorf("%w: mailbox is required", mail.ErrInvalidInput)
	}
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return mail.Mailbox{}, err
	}
	for _, mb := range boxes {
		if mb.Name != name {
			continue
		}
		if mb.Kind == mail.KindSystem {
			return mail.Mailbox{}, fmt.Errorf("%w: %q is a system mailbox and cannot be renamed or deleted", mail.ErrInvalidInput, name)
		}
		return mb, nil
	}
	return mail.Mailbox{}, fmt.Errorf("%w: %q (use the exact name from list_mailboxes)", mail.ErrMailboxNotFound, name)
}

// requireAbsent fails when a mailbox with this name already exists.
func (s *Service) requireAbsent(ctx context.Context, name string) error {
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return err
	}
	for _, mb := range boxes {
		if mb.Name == name {
			return fmt.Errorf("%w: mailbox %q already exists", mail.ErrInvalidInput, name)
		}
	}
	return nil
}
