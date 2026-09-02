package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proton-mail-tools/internal/mail"
	"proton-mail-tools/internal/service"
)

type fakeStore struct {
	mail.Store
	lastQuery mail.Query
}

func (f *fakeStore) Search(_ context.Context, q mail.Query) ([]mail.Summary, error) {
	f.lastQuery = q
	return []mail.Summary{{Mailbox: q.Mailbox, UID: 42, Subject: "hi"}}, nil
}

func (f *fakeStore) Get(_ context.Context, mailbox string, uid uint32) (*mail.Message, error) {
	if uid != 42 {
		return nil, mail.ErrNotFound
	}
	return &mail.Message{Summary: mail.Summary{Mailbox: mailbox, UID: uid}, Body: "body"}, nil
}

type fakeSender struct{ sent []mail.Outgoing }

func (f *fakeSender) Send(_ context.Context, m mail.Outgoing) error {
	f.sent = append(f.sent, m)
	return nil
}

func newTestServer(t *testing.T, apiKey string, allowSend bool) (http.Handler, *fakeStore, *fakeSender) {
	t.Helper()
	store, sender := &fakeStore{}, &fakeSender{}
	svc := service.New(store, sender, service.Options{
		From: mail.Address{Email: "me@example.com"}, AllowSend: allowSend, AllowDelete: true,
		MaxBodyChars: 1000, DefaultLimit: 20, MaxLimit: 100,
	}, nil)
	return New(svc, apiKey), store, sender
}

func do(h http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal(err)
	}

	s := &server{}
	registered := map[string]bool{}
	for _, r := range s.routes() {
		if !r.open {
			registered[r.method+" "+r.path] = true
		}
	}
	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for method, op := range ops {
			key := strings.ToUpper(method) + " " + path
			documented[key] = true
			if op.OperationID == "" {
				t.Errorf("%s has no operationId", key)
			}
			if !registered[key] {
				t.Errorf("%s is documented but not routed", key)
			}
		}
	}
	for key := range registered {
		if !documented[key] {
			t.Errorf("%s is routed but not documented in openapi.json", key)
		}
	}
}

func TestAuth(t *testing.T) {
	h, _, _ := newTestServer(t, "secret", true)

	if rec := do(h, "GET", "/health", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("health should be open, got %d", rec.Code)
	}
	if rec := do(h, "POST", "/messages/search", "", "{}"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key should be 401, got %d", rec.Code)
	}
	if rec := do(h, "POST", "/messages/search", "wrong", "{}"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key should be 401, got %d", rec.Code)
	}
	if rec := do(h, "POST", "/messages/search", "secret", "{}"); rec.Code != http.StatusOK {
		t.Fatalf("right key should be 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestSearchParsesFilters(t *testing.T) {
	h, store, _ := newTestServer(t, "", true)

	rec := do(h, "POST", "/messages/search", "", `{"mailbox":"Archive","from":"alice","since":"2026-01-15","unread_only":true,"limit":500}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	q := store.lastQuery
	if q.Mailbox != "Archive" || q.From != "alice" || !q.UnreadOnly || q.Limit != 100 || q.Since.Format("2006-01-02") != "2026-01-15" {
		t.Fatalf("query not mapped: %+v", q)
	}

	if rec := do(h, "POST", "/messages/search", "", `{"since":"yesterday"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date should be 400, got %d", rec.Code)
	}
	if rec := do(h, "POST", "/messages/search", "", ""); rec.Code != http.StatusOK || store.lastQuery.Mailbox != "INBOX" {
		t.Fatalf("empty body should default to INBOX, got %d mailbox=%q", rec.Code, store.lastQuery.Mailbox)
	}
}

func TestReadMessageErrors(t *testing.T) {
	h, _, _ := newTestServer(t, "", true)

	if rec := do(h, "POST", "/messages/read", "", `{"mailbox":"INBOX","uid":42}`); rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/messages/read", "", `{"mailbox":"INBOX","uid":7}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown uid should be 404, got %d", rec.Code)
	}
	if rec := do(h, "POST", "/messages/read", "", `{"uid":7}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing mailbox should be 400, got %d", rec.Code)
	}
}

func TestSend(t *testing.T) {
	h, _, sender := newTestServer(t, "", true)

	rec := do(h, "POST", "/messages/send", "", `{"to":["Alice <alice@example.com>"],"cc":["bob@example.com"],"subject":"s","body":"b"}`)
	if rec.Code != http.StatusOK || len(sender.sent) != 1 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got := sender.sent[0]; got.To[0].Email != "alice@example.com" || got.To[0].Name != "Alice" || got.Cc[0].Email != "bob@example.com" || got.From.Email != "me@example.com" {
		t.Fatalf("outgoing not mapped: %+v", got)
	}

	rec = do(h, "POST", "/messages/send", "", `{"to":["a@b.com"],"subject":"s","body":"b"}`)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"cc":[]`) {
		t.Fatalf("empty cc should serialise as [], got %d %s", rec.Code, body)
	}
	if rec := do(h, "POST", "/messages/send", "", `{"to":["not an address"],"subject":"s","body":"b"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid address should be 400, got %d", rec.Code)
	}

	disabled, _, _ := newTestServer(t, "", false)
	if rec := do(disabled, "POST", "/messages/send", "", `{"to":["a@b.com"],"subject":"s","body":"b"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("disabled send should be 403, got %d", rec.Code)
	}
}
