package service

import (
	"context"
	"errors"
	"testing"

	"proton-mail-tools/internal/mail"
)

type fakeStore struct {
	mail.Store
	msg      *mail.Message
	flagged  []mail.FlagUpdate
	moved    []string
	movedUID []uint32
	boxes    []mail.Mailbox
	// contents maps mailbox name to the summaries it holds, for Search.
	contents map[string][]mail.Summary
}

func (f *fakeStore) Get(context.Context, string, uint32) (*mail.Message, error) { return f.msg, nil }
func (f *fakeStore) UpdateFlags(_ context.Context, _ string, _ []uint32, u mail.FlagUpdate) error {
	f.flagged = append(f.flagged, u)
	return nil
}
func (f *fakeStore) Move(_ context.Context, _ string, uids []uint32, dest string) error {
	f.moved = append(f.moved, dest)
	f.movedUID = append(f.movedUID, uids...)
	return nil
}

func (f *fakeStore) Search(_ context.Context, q mail.Query) ([]mail.Summary, error) {
	var out []mail.Summary
	for _, sum := range f.contents[q.Mailbox] {
		if q.MessageID != "" && sum.MessageID != q.MessageID {
			continue
		}
		if len(q.UIDs) > 0 && !containsUID(q.UIDs, sum.UID) {
			continue
		}
		out = append(out, sum)
	}
	return out, nil
}

func containsUID(uids []uint32, uid uint32) bool {
	for _, u := range uids {
		if u == uid {
			return true
		}
	}
	return false
}
func (f *fakeStore) ListMailboxes(context.Context) ([]mail.Mailbox, error) { return f.boxes, nil }

type fakeSender struct{ sent []mail.Outgoing }

func (f *fakeSender) Send(_ context.Context, m mail.Outgoing) error {
	f.sent = append(f.sent, m)
	return nil
}

var me = mail.Address{Name: "Me", Email: "me@example.com"}

func newService(store *fakeStore, sender *fakeSender, allowSend, allowDelete bool) *Service {
	return New(store, sender, Options{From: me, AllowSend: allowSend, AllowDelete: allowDelete, MaxBodyChars: 10, DefaultLimit: 5, MaxLimit: 10}, nil)
}

func TestBuildReply(t *testing.T) {
	orig := &mail.Message{
		Summary: mail.Summary{
			Subject:   "Hello",
			From:      []mail.Address{{Email: "alice@example.com"}},
			To:        []mail.Address{me, {Email: "bob@example.com"}},
			MessageID: "orig@example.com",
		},
		Cc:         []mail.Address{{Email: "carol@example.com"}, {Email: "ALICE@example.com"}},
		References: []string{"root@example.com"},
	}

	single := BuildReply(orig, me, "thanks", false)
	if single.Subject != "Re: Hello" || len(single.To) != 1 || single.To[0].Email != "alice@example.com" || len(single.Cc) != 0 {
		t.Fatalf("unexpected single reply: %+v", single)
	}
	if single.InReplyTo != "orig@example.com" || len(single.References) != 2 || single.References[1] != "orig@example.com" {
		t.Fatalf("threading headers wrong: %+v", single)
	}

	all := BuildReply(orig, me, "thanks", true)
	if len(all.Cc) != 2 || all.Cc[0].Email != "bob@example.com" || all.Cc[1].Email != "carol@example.com" {
		t.Fatalf("reply-all should cc bob and carol only, got %+v", all.Cc)
	}

	orig.Subject = "RE: Hello"
	if got := BuildReply(orig, me, "", false).Subject; got != "RE: Hello" {
		t.Fatalf("existing Re: prefix should be kept, got %q", got)
	}
}

func TestReplyMarksAnswered(t *testing.T) {
	store := &fakeStore{msg: &mail.Message{Summary: mail.Summary{From: []mail.Address{{Email: "a@x.com"}}}}}
	sender := &fakeSender{}
	svc := newService(store, sender, true, true)

	if _, err := svc.Reply(context.Background(), ReplyRequest{Mailbox: "INBOX", UID: 7, Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || len(store.flagged) != 1 || store.flagged[0].Answered == nil || !*store.flagged[0].Answered {
		t.Fatalf("expected one send and an answered flag update, got sent=%d flagged=%+v", len(sender.sent), store.flagged)
	}
}

func TestPermissionGates(t *testing.T) {
	svc := newService(&fakeStore{}, &fakeSender{}, false, false)
	ctx := context.Background()

	if _, err := svc.Send(ctx, SendRequest{To: []mail.Address{{Email: "a@x.com"}}}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("send should be disabled, got %v", err)
	}
	if _, err := svc.Trash(ctx, "INBOX", []uint32{1}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("trash should be disabled, got %v", err)
	}
}

var systemBoxes = []mail.Mailbox{{Name: "INBOX", Role: mail.RoleInbox}, {Name: "Trash", Role: mail.RoleTrash}, {Name: "Spam", Role: mail.RoleSpam}, {Name: "Archive", Role: mail.RoleArchive}}

func TestTrashResolvesRole(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"INBOX": {{UID: 1, MessageID: "a@x"}, {UID: 2, MessageID: "b@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, true)

	res, err := svc.Trash(context.Background(), "INBOX", []uint32{1, 2})
	if err != nil || res.Destination != "Trash" || len(store.moved) != 1 || store.moved[0] != "Trash" || len(res.Moved) != 2 {
		t.Fatalf("expected move to Trash, got res=%+v err=%v moved=%v", res, err, store.moved)
	}
}

func TestTrashSkipsMessagesAlreadyInTrash(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"Sent":  {{UID: 10, MessageID: "already@x"}, {UID: 11, MessageID: "fresh@x"}},
		"Trash": {{UID: 500, MessageID: "already@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, true)

	res, err := svc.Trash(context.Background(), "Sent", []uint32{10, 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 1 || res.Moved[0] != 11 || len(res.Skipped) != 1 || res.Skipped[0] != 10 {
		t.Fatalf("expected uid 11 moved and 10 skipped, got %+v", res)
	}
	if len(store.movedUID) != 1 || store.movedUID[0] != 11 {
		t.Fatalf("store should only move uid 11, got %v", store.movedUID)
	}
}

func TestMoveGuardsOnlyPurgeMailboxes(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"INBOX":   {{UID: 1, MessageID: "m@x"}},
		"Spam":    {{UID: 7, MessageID: "m@x"}},
		"Archive": {{UID: 8, MessageID: "m@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, true)
	ctx := context.Background()

	res, err := svc.Move(ctx, "INBOX", []uint32{1}, "Spam")
	if err != nil || len(res.Skipped) != 1 || len(store.movedUID) != 0 {
		t.Fatalf("move into Spam should be skipped when already there, got %+v err=%v moved=%v", res, err, store.movedUID)
	}
	res, err = svc.Move(ctx, "INBOX", []uint32{1}, "Archive")
	if err != nil || len(res.Moved) != 1 || len(store.movedUID) != 1 {
		t.Fatalf("move into Archive should not be guarded, got %+v err=%v moved=%v", res, err, store.movedUID)
	}
}

func TestReadTruncatesBody(t *testing.T) {
	store := &fakeStore{msg: &mail.Message{Body: "0123456789abcdef"}}
	svc := newService(store, &fakeSender{}, true, true)

	msg, err := svc.Read(context.Background(), "INBOX", 1)
	if err != nil || !msg.BodyTruncated || msg.Body[:10] != "0123456789" {
		t.Fatalf("expected truncated body, got %+v err=%v", msg, err)
	}
}

func TestValidation(t *testing.T) {
	svc := newService(&fakeStore{}, &fakeSender{}, true, true)
	ctx := context.Background()

	move := func(uids []uint32, dest string) error {
		_, err := svc.Move(ctx, "INBOX", uids, dest)
		return err
	}
	cases := map[string]error{
		"empty mailbox": svc.UpdateFlags(ctx, "", []uint32{1}, mail.FlagUpdate{}),
		"no uids":       move(nil, "Archive"),
		"zero uid":      move([]uint32{0}, "Archive"),
		"no dest":       move([]uint32{1}, ""),
		"empty flags":   svc.UpdateFlags(ctx, "INBOX", []uint32{1}, mail.FlagUpdate{}),
	}
	for name, err := range cases {
		if !errors.Is(err, mail.ErrInvalidInput) {
			t.Errorf("%s: expected ErrInvalidInput, got %v", name, err)
		}
	}
}

func TestBuildReplyToOwnMessage(t *testing.T) {
	orig := &mail.Message{Summary: mail.Summary{
		From: []mail.Address{me},
		To:   []mail.Address{{Email: "alice@example.com"}, {Email: "bob@example.com"}},
	}}
	got := BuildReply(orig, me, "", false)
	if len(got.To) != 2 || got.To[0].Email != "alice@example.com" {
		t.Fatalf("reply to own message should go to its recipients, got %+v", got.To)
	}
}
