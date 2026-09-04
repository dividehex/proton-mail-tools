// Package httpapi exposes the service as an OpenAPI tool server for OpenWebUI.
package httpapi

import (
	_ "embed"
	"log/slog"
	"net/http"
	"time"

	"proton-mail-tools/internal/service"
)

//go:embed openapi.json
var openAPISpec []byte

// route pairs a method+path with its handler; open routes skip API-key auth.
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
	open    bool
}

type server struct {
	svc    *service.Service
	apiKey string
}

// New builds the HTTP handler. An empty apiKey disables authentication.
func New(svc *service.Service, apiKey string) http.Handler {
	s := &server{svc: svc, apiKey: apiKey}
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		h := r.handler
		if !r.open {
			h = requireAPIKey(s.apiKey, h)
		}
		mux.Handle(r.method+" "+r.path, h)
	}
	return logRequests(mux)
}

// logRequests records one line per request so tool calls made by OpenWebUI are visible.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// routes is the single source of truth for the API surface; the OpenAPI spec
// test asserts that it and openapi.json agree.
func (s *server) routes() []route {
	return []route{
		{"GET", "/health", s.health, true},
		{"GET", "/openapi.json", s.openAPI, true},
		{"GET", "/mailboxes", s.listMailboxes, false},
		{"POST", "/messages/search", s.searchMessages, false},
		{"POST", "/messages/read", s.readMessage, false},
		{"POST", "/messages/send", s.sendMessage, false},
		{"POST", "/messages/reply", s.replyToMessage, false},
		{"POST", "/messages/flags", s.updateFlags, false},
		{"POST", "/messages/move", s.moveMessages, false},
		{"POST", "/messages/trash", s.trashMessages, false},
		{"POST", "/messages/archive", s.archiveMessages, false},
		{"POST", "/messages/spam", s.markSpam, false},
		{"POST", "/messages/unspam", s.markNotSpam, false},
		{"POST", "/messages/restore", s.restoreMessages, false},
		{"POST", "/messages/label", s.labelMessages, false},
		{"POST", "/messages/unlabel", s.unlabelMessages, false},
		{"POST", "/messages/delete", s.deleteMessages, false},
		{"POST", "/mailboxes/empty", s.emptyMailbox, false},
		{"POST", "/mailboxes/create", s.createMailbox, false},
		{"POST", "/mailboxes/rename", s.renameMailbox, false},
		{"POST", "/mailboxes/delete", s.deleteMailbox, false},
	}
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "auth_enabled": s.apiKey != ""})
}

func (s *server) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(openAPISpec)
}
