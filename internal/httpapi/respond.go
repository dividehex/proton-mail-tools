package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"proton-mail-tools/internal/mail"
	"proton-mail-tools/internal/service"
)

const maxRequestBytes = 1 << 20

func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", mail.ErrInvalidInput, err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("%w: malformed JSON: %v", mail.ErrInvalidInput, err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeFailure maps domain errors to HTTP statuses; anything unrecognised is a
// bridge/upstream failure.
func writeFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mail.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, mail.ErrNotFound), errors.Is(err, mail.ErrMailboxNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrDisabled):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("request failed", "err", err)
		writeError(w, http.StatusBadGateway, "mail bridge error: "+err.Error())
	}
}
