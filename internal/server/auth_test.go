package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// TestBasicAuth covers the CalDAV sign-in that runs when there is no bearer
// token: an API token as the password (any given name must be the person's),
// or a local account's CalDAV name and password.
func TestBasicAuth(t *testing.T) {
	s, h := testServer(t)
	ctx := context.Background()
	alice := newPerson(t, s, h, "alice@example.com")
	u, _ := s.store.GetUserByEmail(ctx, "alice@example.com")
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	u.PasswordHash = string(hash)
	if err := s.store.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	get := func(user, pass string) int {
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		req.SetBasicAuth(user, pass)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	for _, c := range []struct {
		user, pass string
		want       int
	}{
		{"", alice.token, 200},
		{"alice", alice.token, 200},
		{"alice@example.com", alice.token, 200},
		{"bob", alice.token, 401},
		{u.Username, "correct horse", 200},
		{u.Username, "wrong", 401},
	} {
		if got := get(c.user, c.pass); got != c.want {
			t.Errorf("basic %q / %q: %d, want %d", c.user, c.pass, got, c.want)
		}
	}
}

// TestOIDCSettingsNeedBaseURL: Core's client is used only with a BASE_URL
// (the redirect URI), explicit OIDC_* always wins.
func TestOIDCSettingsNeedBaseURL(t *testing.T) {
	s, _ := testServer(t)
	if s.rp.Enabled() || s.rp.Name() != "" {
		t.Fatalf("enabled without any settings: %q", s.rp.Name())
	}

	cfg := *s.cfg
	cfg.DataDir = t.TempDir()
	cfg.CoreURL, cfg.BaseURL = "http://core.invalid", ""
	if err := os.WriteFile(cfg.DataDir+"/core.token.oidc.json", []byte(`{"issuer":"http://core.invalid/","client_id":"pco_tasks","client_secret":"s3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DataDir+"/tasks.db", log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s2 := New(&cfg, st, log, nil)
	if s2.rp.Enabled() {
		t.Errorf("Core's client used without a BASE_URL: %+v", s2.rp.Settings())
	}
	cfg.BaseURL = "http://tasks.invalid"
	if got := s2.rp.Settings(); got.Issuer != "http://core.invalid" || got.ClientID != "pco_tasks" || got.Name != "Pocket Core" || !got.AutoCreate {
		t.Errorf("core-issued client: %+v", got)
	}
	cfg.BaseURL = ""
	cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCName, cfg.OIDCAutoCreate = "http://idp.invalid", "me", "PocketID", false
	if got := s2.rp.Settings(); got.Issuer != "http://idp.invalid" || got.ClientID != "me" || got.Name != "PocketID" || got.AutoCreate {
		t.Errorf("env settings: %+v", got)
	}
}
