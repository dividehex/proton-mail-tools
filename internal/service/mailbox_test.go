package service

import (
	"context"
	"errors"
	"testing"

	"proton-mail-tools/internal/mail"
)

func TestCreateMailbox(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes}
	svc := newService(store, &fakeSender{}, true, true)
	ctx := context.Background()

	mb, err := svc.CreateMailbox(ctx, "label", " Invoices ")
	if err != nil || mb.Name != "Labels/Invoices" || mb.Kind != mail.KindLabel || store.created[0] != "Labels/Invoices" {
		t.Fatalf("create label: %+v err=%v created=%v", mb, err, store.created)
	}
	mb, err = svc.CreateMailbox(ctx, "folder", "Folders/Work/Clients")
	if err != nil || mb.Name != "Folders/Work/Clients" || mb.Kind != mail.KindFolder {
		t.Fatalf("create nested folder with prefix: %+v err=%v", mb, err)
	}

	for name, args := range map[string][2]string{
		"unknown kind":     {"system", "X"},
		"empty name":       {"folder", "Folders/"},
		"kind mismatch":    {"folder", "Labels/X"},
		"nested label":     {"label", "A/B"},
		"already exists":   {"label", "Receipts"},
		"existing by full": {"folder", "Folders/Work"},
	} {
		if _, err := svc.CreateMailbox(ctx, args[0], args[1]); !errors.Is(err, mail.ErrInvalidInput) {
			t.Errorf("%s: want invalid input, got %v", name, err)
		}
	}
	if len(store.created) != 2 {
		t.Fatalf("rejected requests must not reach the store: %v", store.created)
	}
}

func TestRenameMailbox(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes}
	svc := newService(store, &fakeSender{}, true, true)
	ctx := context.Background()

	mb, err := svc.RenameMailbox(ctx, "Labels/Receipts", "Invoices")
	if err != nil || mb.Name != "Labels/Invoices" || mb.Kind != mail.KindLabel || store.renamed[0] != "Labels/Receipts>Labels/Invoices" {
		t.Fatalf("rename label: %+v err=%v renamed=%v", mb, err, store.renamed)
	}

	if _, err := svc.RenameMailbox(ctx, "Labels/Nope", "X"); !errors.Is(err, mail.ErrMailboxNotFound) {
		t.Errorf("unknown mailbox: want not found, got %v", err)
	}
	for name, args := range map[string][2]string{
		"system mailbox": {"INBOX", "Mail"},
		"kind change":    {"Folders/Work", "Labels/Work"},
		"same name":      {"Folders/Work", "Work"},
		"target exists":  {"Folders/Work", "Folders/Work"},
		"empty":          {"Folders/Work", ""},
	} {
		if _, err := svc.RenameMailbox(ctx, args[0], args[1]); !errors.Is(err, mail.ErrInvalidInput) {
			t.Errorf("%s: want invalid input, got %v", name, err)
		}
	}
	if len(store.renamed) != 1 {
		t.Fatalf("rejected requests must not reach the store: %v", store.renamed)
	}
}

func TestDeleteMailbox(t *testing.T) {
	store := &fakeStore{boxes: systemBoxes, contents: map[string][]mail.Summary{
		"Folders/Work":    {{UID: 1}},
		"Labels/Receipts": {{UID: 1}},
	}}
	svc := newService(store, &fakeSender{}, true, true)
	ctx := context.Background()

	if _, err := svc.DeleteMailbox(ctx, "Folders/Work"); !errors.Is(err, mail.ErrInvalidInput) {
		t.Fatalf("non-empty folder must be refused, got %v", err)
	}
	mb, err := svc.DeleteMailbox(ctx, "Labels/Receipts")
	if err != nil || mb.Kind != mail.KindLabel || len(store.deleted) != 1 || store.deleted[0] != "Labels/Receipts" {
		t.Fatalf("labels delete regardless of contents: %+v err=%v deleted=%v", mb, err, store.deleted)
	}
	delete(store.contents, "Folders/Work")
	if _, err := svc.DeleteMailbox(ctx, "Folders/Work"); err != nil || store.deleted[1] != "Folders/Work" {
		t.Fatalf("empty folder delete: err=%v deleted=%v", err, store.deleted)
	}
	if _, err := svc.DeleteMailbox(ctx, "Trash"); !errors.Is(err, mail.ErrInvalidInput) {
		t.Errorf("system mailbox: want invalid input, got %v", err)
	}
	if _, err := svc.DeleteMailbox(ctx, "Folders/Missing"); !errors.Is(err, mail.ErrMailboxNotFound) {
		t.Errorf("unknown mailbox: want not found, got %v", err)
	}

	gated := newService(&fakeStore{boxes: systemBoxes}, &fakeSender{}, true, false)
	if _, err := gated.DeleteMailbox(ctx, "Labels/Receipts"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("ALLOW_DELETE=false must gate mailbox deletion, got %v", err)
	}
}
