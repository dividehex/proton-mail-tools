// Package service implements the mailbox use cases exposed as tools. It owns
// policy (permissions, limits, reply semantics) and delegates I/O to mail.Store
// and mail.Sender.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"proton-mail-tools/internal/mail"
)

// ErrDisabled is returned when configuration forbids an operation.
var ErrDisabled = errors.New("operation disabled by configuration")

// Options tunes service policy.
type Options struct {
	From         mail.Address
	AllowSend    bool
	AllowDelete  bool
	AllowPurge   bool
	MaxBodyChars int
	DefaultLimit int
	MaxLimit     int
}

// Service is the application layer between HTTP handlers and the mail ports.
type Service struct {
	store  mail.Store
	sender mail.Sender
	opts   Options
	log    *slog.Logger
}

// New wires a Service. A nil logger falls back to slog.Default().
func New(store mail.Store, sender mail.Sender, opts Options, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, sender: sender, opts: opts, log: log}
}

// SendRequest is a new, non-reply message.
type SendRequest struct {
	To      []mail.Address
	Cc      []mail.Address
	Bcc     []mail.Address
	Subject string
	Body    string
}

// ReplyRequest answers an existing message.
type ReplyRequest struct {
	Mailbox  string
	UID      uint32
	Body     string
	ReplyAll bool
}

// MoveResult reports which messages were moved and which were left alone
// because they were already in the destination.
type MoveResult struct {
	Destination string
	Moved       []uint32
	Skipped     []uint32
}

// ListMailboxes returns every selectable folder and label.
func (s *Service) ListMailboxes(ctx context.Context) ([]mail.Mailbox, error) {
	return s.store.ListMailboxes(ctx)
}

// Search applies defaults and limits, then queries the store.
func (s *Service) Search(ctx context.Context, q mail.Query) ([]mail.Summary, error) {
	if q.Mailbox == "" {
		q.Mailbox = "INBOX"
	}
	switch {
	case q.Limit <= 0:
		q.Limit = s.opts.DefaultLimit
	case q.Limit > s.opts.MaxLimit:
		q.Limit = s.opts.MaxLimit
	}
	return s.store.Search(ctx, q)
}

// Read fetches one message with its body truncated to the configured size.
func (s *Service) Read(ctx context.Context, mailbox string, uid uint32) (*mail.Message, error) {
	if err := requireMessage(mailbox, uid); err != nil {
		return nil, err
	}
	msg, err := s.store.Get(ctx, mailbox, uid)
	if err != nil {
		return nil, err
	}
	if max := s.opts.MaxBodyChars; max > 0 && len(msg.Body) > max {
		msg.Body = truncateRunes(msg.Body, max)
		msg.BodyTruncated = true
	}
	return msg, nil
}

// Send composes and delivers a new message.
func (s *Service) Send(ctx context.Context, req SendRequest) (mail.Outgoing, error) {
	if !s.opts.AllowSend {
		return mail.Outgoing{}, fmt.Errorf("%w: sending mail", ErrDisabled)
	}
	if len(req.To) == 0 {
		return mail.Outgoing{}, fmt.Errorf("%w: at least one 'to' recipient is required", mail.ErrInvalidInput)
	}
	out := mail.Outgoing{
		From:    s.opts.From,
		To:      req.To,
		Cc:      req.Cc,
		Bcc:     req.Bcc,
		Subject: req.Subject,
		Body:    req.Body,
	}
	return out, s.sender.Send(ctx, out)
}

// Reply builds a threaded response to an existing message and delivers it.
func (s *Service) Reply(ctx context.Context, req ReplyRequest) (mail.Outgoing, error) {
	if !s.opts.AllowSend {
		return mail.Outgoing{}, fmt.Errorf("%w: sending mail", ErrDisabled)
	}
	if err := requireMessage(req.Mailbox, req.UID); err != nil {
		return mail.Outgoing{}, err
	}
	orig, err := s.store.Get(ctx, req.Mailbox, req.UID)
	if err != nil {
		return mail.Outgoing{}, err
	}
	out := BuildReply(orig, s.opts.From, req.Body, req.ReplyAll)
	if len(out.To) == 0 {
		return mail.Outgoing{}, fmt.Errorf("%w: original message has no reply address", mail.ErrInvalidInput)
	}
	if err := s.sender.Send(ctx, out); err != nil {
		return mail.Outgoing{}, err
	}
	answered := true
	if err := s.store.UpdateFlags(ctx, req.Mailbox, []uint32{req.UID}, mail.FlagUpdate{Answered: &answered}); err != nil {
		s.log.Warn("reply sent but failed to mark original as answered", "mailbox", req.Mailbox, "uid", req.UID, "err", err)
	}
	return out, nil
}

// UpdateFlags marks messages read/unread and flagged/unflagged.
func (s *Service) UpdateFlags(ctx context.Context, mailbox string, uids []uint32, update mail.FlagUpdate) error {
	if err := requireMessages(mailbox, uids); err != nil {
		return err
	}
	if update.IsEmpty() {
		return fmt.Errorf("%w: nothing to update; set 'read' and/or 'flagged'", mail.ErrInvalidInput)
	}
	return s.store.UpdateFlags(ctx, mailbox, uids, update)
}

// Move relocates messages into another mailbox.
func (s *Service) Move(ctx context.Context, mailbox string, uids []uint32, destination string) (MoveResult, error) {
	if err := requireMessages(mailbox, uids); err != nil {
		return MoveResult{}, err
	}
	if destination == "" {
		return MoveResult{}, fmt.Errorf("%w: destination mailbox is required", mail.ErrInvalidInput)
	}
	purge, err := s.isPurgeMailbox(ctx, destination)
	if err != nil {
		return MoveResult{}, err
	}
	if purge {
		return s.guardedMove(ctx, mailbox, uids, destination)
	}
	if err := s.store.Move(ctx, mailbox, uids, destination); err != nil {
		return MoveResult{}, err
	}
	return MoveResult{Destination: destination, Moved: uids}, nil
}

// Delete permanently removes messages. Only Spam and Trash allow it, matching
// Proton's own semantics; elsewhere callers must use Trash.
func (s *Service) Delete(ctx context.Context, mailbox string, uids []uint32) (int, error) {
	if err := requireMessages(mailbox, uids); err != nil {
		return 0, err
	}
	if err := s.requirePurgeable(ctx, mailbox); err != nil {
		return 0, err
	}
	return len(uids), s.store.Remove(ctx, mailbox, uids)
}

// Empty permanently removes every message in a Spam or Trash mailbox.
func (s *Service) Empty(ctx context.Context, mailbox string) (int, error) {
	if mailbox == "" {
		return 0, fmt.Errorf("%w: mailbox is required", mail.ErrInvalidInput)
	}
	if err := s.requirePurgeable(ctx, mailbox); err != nil {
		return 0, err
	}
	uids, err := s.store.AllUIDs(ctx, mailbox)
	if err != nil {
		return 0, err
	}
	if len(uids) == 0 {
		return 0, nil
	}
	return len(uids), s.store.Remove(ctx, mailbox, uids)
}

// requirePurgeable enforces the purge gate and restricts permanent deletion to
// the mailboxes where Proton itself deletes permanently.
func (s *Service) requirePurgeable(ctx context.Context, mailbox string) error {
	if !s.opts.AllowPurge {
		return fmt.Errorf("%w: permanent deletion (ALLOW_PURGE)", ErrDisabled)
	}
	purge, err := s.isPurgeMailbox(ctx, mailbox)
	if err != nil {
		return err
	}
	if !purge {
		return fmt.Errorf("%w: permanent deletion is only allowed in Spam or Trash; use trash_messages for %q", mail.ErrInvalidInput, mailbox)
	}
	return nil
}

// Archive moves messages into the archive mailbox.
func (s *Service) Archive(ctx context.Context, mailbox string, uids []uint32) (MoveResult, error) {
	if err := requireMessages(mailbox, uids); err != nil {
		return MoveResult{}, err
	}
	archive, err := s.mailboxByRole(ctx, mail.RoleArchive)
	if err != nil {
		return MoveResult{}, err
	}
	return s.guardedMove(ctx, mailbox, uids, archive)
}

// LabelResult reports which messages a label operation touched.
type LabelResult struct {
	Label    string
	Affected []uint32
	Skipped  []uint32
}

// Label applies a Proton label to messages; those that already carry it are skipped.
func (s *Service) Label(ctx context.Context, mailbox string, uids []uint32, label string) (LabelResult, error) {
	label, err := s.requireLabel(ctx, label)
	if err != nil {
		return LabelResult{}, err
	}
	if err := requireMessages(mailbox, uids); err != nil {
		return LabelResult{}, err
	}
	present, absent, err := s.partitionByPresence(ctx, mailbox, uids, label)
	if err != nil {
		return LabelResult{}, err
	}
	result := LabelResult{Label: label, Skipped: present.sourceUIDs()}
	if len(absent) > 0 {
		if err := s.store.Copy(ctx, mailbox, absent.sourceUIDs(), label); err != nil {
			return LabelResult{}, err
		}
		result.Affected = absent.sourceUIDs()
	}
	return result, nil
}

// Unlabel removes a Proton label from messages; those without it are skipped.
func (s *Service) Unlabel(ctx context.Context, mailbox string, uids []uint32, label string) (LabelResult, error) {
	label, err := s.requireLabel(ctx, label)
	if err != nil {
		return LabelResult{}, err
	}
	if err := requireMessages(mailbox, uids); err != nil {
		return LabelResult{}, err
	}
	present, absent, err := s.partitionByPresence(ctx, mailbox, uids, label)
	if err != nil {
		return LabelResult{}, err
	}
	result := LabelResult{Label: label, Skipped: absent.sourceUIDs()}
	if len(present) > 0 {
		if err := s.store.Remove(ctx, label, present.targetUIDs()); err != nil {
			return LabelResult{}, err
		}
		result.Affected = present.sourceUIDs()
	}
	return result, nil
}

// requireLabel normalises "Receipts" / "Labels/Receipts" and checks the label exists.
func (s *Service) requireLabel(ctx context.Context, label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("%w: label is required", mail.ErrInvalidInput)
	}
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return "", err
	}
	for _, mb := range boxes {
		if mb.Kind == mail.KindLabel && (mb.Name == label || strings.TrimPrefix(mb.Name, mail.LabelPrefix) == label) {
			return mb.Name, nil
		}
	}
	return "", fmt.Errorf("%w: no label named %q (use list_mailboxes; labels have kind \"label\")", mail.ErrMailboxNotFound, label)
}

// presence pairs a message's uid in the source mailbox with its uid in the target, if any.
type presence struct{ source, target uint32 }

type presences []presence

func (p presences) sourceUIDs() []uint32 {
	out := make([]uint32, 0, len(p))
	for _, x := range p {
		out = append(out, x.source)
	}
	return out
}

func (p presences) targetUIDs() []uint32 {
	out := make([]uint32, 0, len(p))
	for _, x := range p {
		out = append(out, x.target)
	}
	return out
}

// partitionByPresence looks each message up in target by Message-ID and splits
// the uids into those already present there and those absent.
func (s *Service) partitionByPresence(ctx context.Context, mailbox string, uids []uint32, target string) (present, absent presences, err error) {
	summaries, err := s.store.Search(ctx, mail.Query{Mailbox: mailbox, UIDs: uids, Limit: len(uids)})
	if err != nil {
		return nil, nil, err
	}
	for _, sum := range summaries {
		if sum.MessageID != "" {
			found, err := s.store.Search(ctx, mail.Query{Mailbox: target, MessageID: sum.MessageID, Limit: 1})
			if err != nil {
				return nil, nil, err
			}
			if len(found) > 0 {
				present = append(present, presence{source: sum.UID, target: found[0].UID})
				continue
			}
		}
		absent = append(absent, presence{source: sum.UID})
	}
	return present, absent, nil
}

// Trash moves messages into the trash mailbox.
func (s *Service) Trash(ctx context.Context, mailbox string, uids []uint32) (MoveResult, error) {
	if !s.opts.AllowDelete {
		return MoveResult{}, fmt.Errorf("%w: deleting mail", ErrDisabled)
	}
	if err := requireMessages(mailbox, uids); err != nil {
		return MoveResult{}, err
	}
	trash, err := s.mailboxByRole(ctx, mail.RoleTrash)
	if err != nil {
		return MoveResult{}, err
	}
	return s.guardedMove(ctx, mailbox, uids, trash)
}

// guardedMove skips messages that are already in the destination. Proton
// Bridge permanently deletes a message that is moved into Trash or Spam while
// it is already there (e.g. the Sent copy of a self-addressed email, or the
// same message reached through a label), so the skip prevents data loss.
func (s *Service) guardedMove(ctx context.Context, mailbox string, uids []uint32, destination string) (MoveResult, error) {
	present, absent, err := s.partitionByPresence(ctx, mailbox, uids, destination)
	if err != nil {
		return MoveResult{}, err
	}
	result := MoveResult{Destination: destination, Moved: absent.sourceUIDs(), Skipped: present.sourceUIDs()}
	if len(result.Moved) > 0 {
		if err := s.store.Move(ctx, mailbox, result.Moved, destination); err != nil {
			return MoveResult{}, err
		}
	}
	return result, nil
}

func (s *Service) mailboxByRole(ctx context.Context, role string) (string, error) {
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return "", err
	}
	if mb, ok := findByRole(boxes, role); ok {
		return mb.Name, nil
	}
	return "", fmt.Errorf("%w: no mailbox with role %q", mail.ErrMailboxNotFound, role)
}

// isPurgeMailbox reports whether name is a mailbox Bridge purges from (Trash or Spam).
func (s *Service) isPurgeMailbox(ctx context.Context, name string) (bool, error) {
	boxes, err := s.store.ListMailboxes(ctx)
	if err != nil {
		return false, err
	}
	mb, ok := findByName(boxes, name)
	return ok && (mb.Role == mail.RoleTrash || mb.Role == mail.RoleSpam), nil
}

// BuildReply derives recipients, subject and threading headers from the original.
func BuildReply(orig *mail.Message, from mail.Address, body string, replyAll bool) mail.Outgoing {
	to := orig.ReplyTo
	if len(to) == 0 {
		to = orig.From
	}
	recipients := withoutSelf(to, from)
	if len(recipients) == 0 { // replying to our own message: answer its recipients
		recipients = dedupe(orig.To, nil)
	}
	out := mail.Outgoing{
		From:      from,
		To:        recipients,
		Subject:   replySubject(orig.Subject),
		Body:      body,
		InReplyTo: orig.MessageID,
	}
	if replyAll {
		others := withoutSelf(append(append([]mail.Address{}, orig.To...), orig.Cc...), from)
		out.Cc = dedupe(others, out.To)
	}
	if orig.MessageID != "" {
		out.References = append(append([]string{}, orig.References...), orig.MessageID)
	}
	return out
}

func replySubject(subject string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		return subject
	}
	return "Re: " + subject
}

func withoutSelf(addrs []mail.Address, self mail.Address) []mail.Address {
	return dedupe(addrs, []mail.Address{self})
}

// dedupe returns addrs without duplicates and without any entry in exclude.
func dedupe(addrs, exclude []mail.Address) []mail.Address {
	seen := make(map[string]bool, len(exclude))
	for _, a := range exclude {
		seen[strings.ToLower(a.Email)] = true
	}
	out := make([]mail.Address, 0, len(addrs))
	for _, a := range addrs {
		key := strings.ToLower(a.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

func requireMessage(mailbox string, uid uint32) error {
	return requireMessages(mailbox, []uint32{uid})
}

func requireMessages(mailbox string, uids []uint32) error {
	if mailbox == "" {
		return fmt.Errorf("%w: mailbox is required", mail.ErrInvalidInput)
	}
	if len(uids) == 0 {
		return fmt.Errorf("%w: at least one uid is required", mail.ErrInvalidInput)
	}
	for _, uid := range uids {
		if uid == 0 {
			return fmt.Errorf("%w: uid must be a positive integer", mail.ErrInvalidInput)
		}
	}
	return nil
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "\n[... truncated ...]"
}
