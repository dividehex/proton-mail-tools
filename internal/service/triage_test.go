package service

import (
	"context"
	"errors"
	"testing"

	"proton-mail-tools/internal/mail"
)

func TestMarkSpam(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"INBOX": {{UID: 1, MessageID: "a@x"}, {UID: 2, MessageID: "b@x"}},
		"Spam":  {{UID: 9, MessageID: "b@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, true)
	ctx := context.Background()

	res, err := svc.MarkSpam(ctx, "INBOX", []uint32{1, 2})
	if err != nil || res.Destination != "Spam" || len(res.Moved) != 1 || res.Moved[0] != 1 || len(res.Skipped) != 1 || res.Skipped[0] != 2 {
		t.Fatalf("expected uid 1 moved to Spam and 2 skipped, got %+v err=%v", res, err)
	}
	if _, err := svc.MarkSpam(ctx, "Spam", []uint32{9}); !errors.Is(err, mail.ErrInvalidInput) {
		t.Errorf("already in Spam: want invalid input, got %v", err)
	}
	gated := newService(store, &fakeSender{}, true, false)
	if _, err := gated.MarkSpam(ctx, "INBOX", []uint32{1}); !errors.Is(err, ErrDisabled) {
		t.Errorf("ALLOW_DELETE=false must gate mark_spam, got %v", err)
	}
}

func TestMarkNotSpam(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"Spam":  {{UID: 5, MessageID: "a@x"}, {UID: 6, MessageID: "b@x"}},
		"INBOX": {{UID: 1, MessageID: "b@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, false)
	ctx := context.Background()

	res, err := svc.MarkNotSpam(ctx, "Spam", []uint32{5, 6})
	if err != nil || res.Destination != "INBOX" || len(res.Moved) != 1 || res.Moved[0] != 5 || len(res.Skipped) != 1 || res.Skipped[0] != 6 {
		t.Fatalf("expected uid 5 moved to INBOX and 6 skipped, got %+v err=%v", res, err)
	}
	if _, err := svc.MarkNotSpam(ctx, "INBOX", []uint32{1}); !errors.Is(err, mail.ErrInvalidInput) {
		t.Errorf("source must be Spam: want invalid input, got %v", err)
	}
}

func TestRestore(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"Trash": {{UID: 3, MessageID: "a@x"}, {UID: 4, MessageID: "b@x"}},
	}}
	svc := newService(store, &fakeSender{}, true, false)
	ctx := context.Background()

	res, err := svc.Restore(ctx, "Trash", []uint32{3}, "")
	if err != nil || res.Destination != "INBOX" || len(res.Moved) != 1 {
		t.Fatalf("default destination should be INBOX, got %+v err=%v", res, err)
	}
	res, err = svc.Restore(ctx, "Trash", []uint32{4}, "Folders/Work")
	if err != nil || res.Destination != "Folders/Work" || store.moved[1] != "Folders/Work" {
		t.Fatalf("explicit folder destination: %+v err=%v moved=%v", res, err, store.moved)
	}

	if _, err := svc.Restore(ctx, "INBOX", []uint32{1}, ""); !errors.Is(err, mail.ErrInvalidInput) {
		t.Errorf("source must be Trash: want invalid input, got %v", err)
	}
	for name, dest := range map[string]string{"label": "Labels/Receipts", "spam": "Spam", "trash": "Trash"} {
		if _, err := svc.Restore(ctx, "Trash", []uint32{3}, dest); !errors.Is(err, mail.ErrInvalidInput) {
			t.Errorf("%s destination: want invalid input, got %v", name, err)
		}
	}
	if _, err := svc.Restore(ctx, "Trash", []uint32{3}, "Folders/Nope"); !errors.Is(err, mail.ErrMailboxNotFound) {
		t.Errorf("unknown destination: want not found, got %v", err)
	}
	if len(store.moved) != 2 {
		t.Fatalf("rejected requests must not reach the store: %v", store.moved)
	}
}
