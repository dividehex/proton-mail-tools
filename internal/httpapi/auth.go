package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireAPIKey enforces "Authorization: Bearer <key>"; an empty key disables the check.
func requireAPIKey(key string, next http.HandlerFunc) http.HandlerFunc {
	if key == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(key)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next(w, r)
	}
}
