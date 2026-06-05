package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

// BasicAuth gates the wrapped handler with HTTP Basic Auth using the explorer
// credentials. It is a passthrough when no credentials are configured. The
// password may be supplied pre-hashed (PasswordHash, compared by SHA-256) or as
// plaintext (Password); the hash wins so the platform never has to hold a raw
// password. Apply it to the explorer UI routes only, never the mock mounts.
func BasicAuth(cfg *config.BasicAuthConfig) func(http.Handler) http.Handler {
	enabled := cfg != nil && cfg.User != "" && (cfg.PasswordHash != "" || cfg.Password != "")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || authorized(cfg, r) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="Mockzilla API Explorer", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}

func authorized(cfg *config.BasicAuthConfig, r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(cfg.User)) != 1 {
		return false
	}

	if cfg.PasswordHash != "" {
		return subtle.ConstantTimeCompare([]byte(hashSecret(pass)), []byte(cfg.PasswordHash)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(pass), []byte(cfg.Password)) == 1
}

// hashSecret matches the lambdas' org.HashSecret so a hash minted by the control
// plane verifies here.
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
