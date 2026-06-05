package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

func serve(t *testing.T, cfg *config.BasicAuthConfig, setAuth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	h := BasicAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/.services", nil)
	if setAuth != nil {
		setAuth(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBasicAuth_PassthroughWhenUnconfigured(t *testing.T) {
	for _, cfg := range []*config.BasicAuthConfig{nil, {}, {User: "u"}} {
		rec := serve(t, cfg, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("unconfigured auth should pass through, got %d", rec.Code)
		}
	}
}

func TestBasicAuth_PlaintextPassword(t *testing.T) {
	cfg := &config.BasicAuthConfig{User: "explorer", Password: "s3cret"}

	rec := serve(t, cfg, func(r *http.Request) { r.SetBasicAuth("explorer", "s3cret") })
	if rec.Code != http.StatusOK {
		t.Fatalf("correct creds should pass, got %d", rec.Code)
	}

	rec = serve(t, cfg, func(r *http.Request) { r.SetBasicAuth("explorer", "wrong") })
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password should 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 must carry a WWW-Authenticate challenge")
	}
}

func TestBasicAuth_PasswordHashWinsOverPlaintext(t *testing.T) {
	cfg := &config.BasicAuthConfig{
		User:         "explorer",
		Password:     "ignored",
		PasswordHash: hashSecret("s3cret"),
	}

	rec := serve(t, cfg, func(r *http.Request) { r.SetBasicAuth("explorer", "s3cret") })
	if rec.Code != http.StatusOK {
		t.Fatalf("hash path should accept the matching password, got %d", rec.Code)
	}

	rec = serve(t, cfg, func(r *http.Request) { r.SetBasicAuth("explorer", "ignored") })
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("plaintext must be ignored when a hash is set, got %d", rec.Code)
	}
}

func TestBasicAuth_MissingCredentialsChallenges(t *testing.T) {
	cfg := &config.BasicAuthConfig{User: "explorer", PasswordHash: hashSecret("s3cret")}
	rec := serve(t, cfg, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing creds should 401, got %d", rec.Code)
	}
}
