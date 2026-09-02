package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"proton-mail-tools/internal/mail"
	"proton-mail-tools/internal/service"
)

const dateLayout = "2006-01-02"

type searchRequest struct {
	Mailbox    string `json:"mailbox"`
	Query      string `json:"query"`
	From       string `json:"from"`
	To         string `json:"to"`
	Subject    string `json:"subject"`
	Since      string `json:"since"`
	Before     string `json:"before"`
	UnreadOnly bool   `json:"unread_only"`
	Limit      int    `json:"limit"`
}

type messageRef struct {
	Mailbox string `json:"mailbox"`
	UID     uint32 `json:"uid"`
}

type messagesRef struct {
	Mailbox string   `json:"mailbox"`
	UIDs    []uint32 `json:"uids"`
}

type sendRequest struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

type replyRequest struct {
	messageRef
	Body     string `json:"body"`
	ReplyAll bool   `json:"reply_all"`
}

type flagsRequest struct {
	messagesRef
	Read    *bool `json:"read"`
	Flagged *bool `json:"flagged"`
}

type moveRequest struct {
	messagesRef
	Destination string `json:"destination"`
}

type labelRequest struct {
	messagesRef
	Label string `json:"label"`
}

func (s *server) listMailboxes(w http.ResponseWriter, r *http.Request) {
	boxes, err := s.svc.ListMailboxes(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": boxes})
}

func (s *server) searchMessages(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	since, err := parseDate("since", req.Since)
	if err != nil {
		writeFailure(w, err)
		return
	}
	before, err := parseDate("before", req.Before)
	if err != nil {
		writeFailure(w, err)
		return
	}
	q := mail.Query{
		Mailbox: req.Mailbox, Text: req.Query, From: req.From, To: req.To, Subject: req.Subject,
		Since: since, Before: before, UnreadOnly: req.UnreadOnly, Limit: req.Limit,
	}
	msgs, err := s.svc.Search(r.Context(), q)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(msgs), "messages": msgs})
}

func (s *server) readMessage(w http.ResponseWriter, r *http.Request) {
	var req messageRef
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	msg, err := s.svc.Read(r.Context(), req.Mailbox, req.UID)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	to, cc, bcc, err := parseRecipients(req.To, req.Cc, req.Bcc)
	if err != nil {
		writeFailure(w, err)
		return
	}
	out, err := s.svc.Send(r.Context(), service.SendRequest{To: to, Cc: cc, Bcc: bcc, Subject: req.Subject, Body: req.Body})
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sentResponse(out))
}

func (s *server) replyToMessage(w http.ResponseWriter, r *http.Request) {
	var req replyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	out, err := s.svc.Reply(r.Context(), service.ReplyRequest{Mailbox: req.Mailbox, UID: req.UID, Body: req.Body, ReplyAll: req.ReplyAll})
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sentResponse(out))
}

func (s *server) updateFlags(w http.ResponseWriter, r *http.Request) {
	var req flagsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	if err := s.svc.UpdateFlags(r.Context(), req.Mailbox, req.UIDs, mail.FlagUpdate{Read: req.Read, Flagged: req.Flagged}); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "updated": len(req.UIDs)})
}

func (s *server) moveMessages(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	res, err := s.svc.Move(r.Context(), req.Mailbox, req.UIDs, req.Destination)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, moveResponse(res))
}

func (s *server) trashMessages(w http.ResponseWriter, r *http.Request) {
	var req messagesRef
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	res, err := s.svc.Trash(r.Context(), req.Mailbox, req.UIDs)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, moveResponse(res))
}

func (s *server) archiveMessages(w http.ResponseWriter, r *http.Request) {
	var req messagesRef
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	res, err := s.svc.Archive(r.Context(), req.Mailbox, req.UIDs)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, moveResponse(res))
}

func (s *server) labelMessages(w http.ResponseWriter, r *http.Request) {
	s.handleLabel(w, r, s.svc.Label, "labeled")
}

func (s *server) unlabelMessages(w http.ResponseWriter, r *http.Request) {
	s.handleLabel(w, r, s.svc.Unlabel, "unlabeled")
}

type labelOp func(ctx context.Context, mailbox string, uids []uint32, label string) (service.LabelResult, error)

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request, op labelOp, verb string) {
	var req labelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	res, err := op(r.Context(), req.Mailbox, req.UIDs, req.Label)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"label":        res.Label,
		verb:           len(res.Affected),
		"skipped_uids": nonNilUIDs(res.Skipped),
	})
}

func nonNilUIDs(uids []uint32) []uint32 {
	if uids == nil {
		return []uint32{}
	}
	return uids
}

func moveResponse(res service.MoveResult) map[string]any {
	return map[string]any{
		"status":       "ok",
		"destination":  res.Destination,
		"moved":        len(res.Moved),
		"skipped_uids": nonNilUIDs(res.Skipped),
	}
}

func sentResponse(out mail.Outgoing) map[string]any {
	return map[string]any{
		"status":  "sent",
		"from":    out.From,
		"to":      nonNil(out.To),
		"cc":      nonNil(out.Cc),
		"subject": out.Subject,
	}
}

// nonNil makes empty address lists serialise as [] rather than null.
func nonNil(addrs []mail.Address) []mail.Address {
	if addrs == nil {
		return []mail.Address{}
	}
	return addrs
}

func parseRecipients(to, cc, bcc []string) (t, c, b []mail.Address, err error) {
	if t, err = mail.ParseAddresses(to); err != nil {
		return
	}
	if c, err = mail.ParseAddresses(cc); err != nil {
		return
	}
	b, err = mail.ParseAddresses(bcc)
	return
}

func parseDate(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s must be YYYY-MM-DD", mail.ErrInvalidInput, field)
	}
	return t, nil
}
