package character

// auth_test.go - sessions, the auth check, and ownership as seen over HTTP.

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const otherToken = "test-token-b" // a valid session for otherUser, from authServer

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }

// authServer is a server with testToken and otherToken signed in as testOwner and otherUser.
func authServer(t *testing.T) (*Server, *SQLiteStore, *Auth) {
	t.Helper()
	cfg, store := testConfig(t)
	seedSession(t, cfg.Auth.db, otherUser, otherToken)
	return NewServer(cfg), store, cfg.Auth
}

func sessionExpiry(t *testing.T, a *Auth, token string) int64 {
	t.Helper()
	var exp int64
	if err := a.db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	return exp
}

func TestSessionTokenIsRandomAndOnlyItsHashIsStored(t *testing.T) {
	a := NewAuth(newTestDB(t), nil)
	ctx := context.Background()
	token, expires, err := a.CreateSession(ctx, testOwner)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Errorf("token %q should be 32 base64url bytes (err %v)", token, err)
	}
	if time.Until(expires) < sessionLifetime-time.Minute {
		t.Errorf("expires %v, want about 30 days away", expires)
	}
	second, _, _ := a.CreateSession(ctx, testOwner)
	if token == second {
		t.Error("two sessions got the same token")
	}

	var stored []byte
	a.db.QueryRow(`SELECT token_hash FROM sessions LIMIT 1`).Scan(&stored)
	if len(stored) != 32 || strings.Contains(string(stored), token) {
		t.Errorf("stored %x: want only the 32-byte hash", stored)
	}
	if got, err := a.Authenticate(ctx, token); err != nil || got != testOwner {
		t.Errorf("Authenticate = %q, %v", got, err)
	}
}

func TestSessionExpires(t *testing.T) {
	a := NewAuth(newTestDB(t), nil)
	clock := newFakeClock()
	a.now = clock.now
	ctx := context.Background()
	token, _, _ := a.CreateSession(ctx, testOwner)

	clock.advance(sessionLifetime + time.Second)
	if _, err := a.Authenticate(ctx, token); !errors.Is(err, errNoSession) {
		t.Errorf("err = %v, want errNoSession", err)
	}
}

func TestSessionSlidesOnlyAfterHalfItsLifetime(t *testing.T) {
	a := NewAuth(newTestDB(t), nil)
	clock := newFakeClock()
	a.now = clock.now
	ctx := context.Background()
	token, _, _ := a.CreateSession(ctx, testOwner)
	original := sessionExpiry(t, a, token)

	clock.advance(sessionLifetime/2 - time.Hour)
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if got := sessionExpiry(t, a, token); got != original {
		t.Errorf("expiry moved from %d to %d: more than half is left, so nothing should be written", original, got)
	}

	clock.advance(2 * time.Hour)
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if got, want := sessionExpiry(t, a, token), clock.now().Add(sessionLifetime).Unix(); got != want {
		t.Errorf("expiry = %d, want %d: under half is left, so it should extend", got, want)
	}

	clock.advance(sessionLifetime - time.Hour) // would have expired without the extension
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Errorf("the extended session should still work: %v", err)
	}
}

func TestDeleteExpiredKeepsLiveRows(t *testing.T) {
	a := NewAuth(newTestDB(t), nil)
	ctx := context.Background()
	now := time.Now().Unix()

	for _, q := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (x'01', ?, ?, 1)`, []any{testOwner, now - 1}},
		{`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (x'02', ?, ?, 1)`, []any{testOwner, now + 60}},
		{`INSERT INTO otp_codes (user_id, code_hash, expires_at, created_at) VALUES (?, x'00', ?, 1)`, []any{testOwner, now - 1}},
		{`INSERT INTO otp_codes (user_id, code_hash, expires_at, created_at) VALUES (?, x'00', ?, 1)`, []any{otherUser, now + 60}},
	} {
		if _, err := a.db.Exec(q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := a.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var liveSession []byte
	var liveCodeUser string
	var sessions, codes int
	a.db.QueryRow(`SELECT COUNT(*), MAX(token_hash) FROM sessions`).Scan(&sessions, &liveSession)
	a.db.QueryRow(`SELECT COUNT(*), MAX(user_id) FROM otp_codes`).Scan(&codes, &liveCodeUser)
	if sessions != 1 || liveSession[0] != 2 || codes != 1 || liveCodeUser != otherUser {
		t.Errorf("sessions %d (%x), codes %d (%s); want only the live one of each", sessions, liveSession, codes, liveCodeUser)
	}
}

func TestRunCleanupRunsAtStartup(t *testing.T) {
	a := NewAuth(newTestDB(t), nil)
	if _, err := a.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (x'01', ?, 1, 1)`, testOwner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.RunCleanup(ctx, time.Hour); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		a.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the expired session was not removed at startup")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// The middleware
// ---------------------------------------------------------------------------

func TestAuthRejectsBadTokens(t *testing.T) {
	srv, _, a := authServer(t)
	if _, err := a.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, 1, 1)`,
		hashToken("expired-token"), testOwner); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	a.db.QueryRow(`SELECT token_hash FROM sessions WHERE user_id = ?`, testOwner).Scan(&stored)

	headers := map[string]string{
		"missing":       "",
		"unknown":       "Bearer not-a-real-token",
		"expired":       "Bearer expired-token",
		"wrong scheme":  "Basic " + testToken,
		"no scheme":     testToken,
		"empty token":   "Bearer ",
		"scheme only":   "Bearer",
		"stored hash":   "Bearer " + base64.RawURLEncoding.EncodeToString(stored),
		"token + extra": "Bearer " + testToken + "x",
	}
	for name, h := range headers {
		req := newRequest(http.MethodGet, "/api/characters", h)
		rec := serve(srv, req)
		wantError(t, rec, http.StatusUnauthorized, "unauthorized")
		if rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%s: no WWW-Authenticate header", name)
		}
	}
	// The scheme isn't case sensitive.
	wantStatus(t, serve(srv, newRequest(http.MethodGet, "/api/characters", "bearer "+testToken)), http.StatusOK)
}

func TestServerWithoutAuthAcceptsNobody(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Auth = nil
	wantError(t, call(t, NewServer(cfg), http.MethodGet, "/api/characters", nil), http.StatusUnauthorized, "unauthorized")
}

func TestPublicAndProtectedRoutes(t *testing.T) {
	cfg, store := testConfig(t)
	saveFixture(t, store, Character{ID: "c", Name: "C"})
	cfg.Reload = func() (*ContentLibrary, error) { return newContentLibrary(), nil }
	srv := NewServer(cfg)

	public := [][2]string{
		{"GET", "/api/health"}, {"GET", "/api/ruleset"}, {"GET", "/api/openapi.json"}, {"GET", "/api/docs"},
		{"GET", "/api/content"}, {"GET", "/api/content/races"}, {"GET", "/api/content/classes"},
	}
	for _, rt := range public {
		wantStatus(t, callAs(t, srv, "", rt[0], rt[1], nil), http.StatusOK)
	}

	protected := [][2]string{
		{"GET", "/api/characters"}, {"POST", "/api/characters"}, {"GET", "/api/characters/c"},
		{"PUT", "/api/characters/c"}, {"PATCH", "/api/characters/c"}, {"DELETE", "/api/characters/c"},
		{"GET", "/api/characters/c/sheet"}, {"POST", "/api/characters/c/hp"}, {"POST", "/api/characters/c/inventory"},
		{"GET", "/api/characters/c/choice-options"}, {"POST", "/api/characters/c/level-up"},
		{"POST", "/api/sheet-preview"}, {"POST", "/api/content/reload"},
		{"GET", "/api/me"}, {"POST", "/api/auth/logout"},
	}
	for _, rt := range protected {
		rec := callAs(t, srv, "", rt[0], rt[1], "{}")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", rt[0], rt[1], rec.Code)
		}
	}
	// The check comes first, so an invalid id or unknown sub-route says nothing to a stranger.
	wantError(t, callAs(t, srv, "", http.MethodGet, "/api/characters/bad%20id", nil), http.StatusUnauthorized, "unauthorized")
	if got := getChar(t, srv, "c"); got.Name != "C" {
		t.Errorf("anonymous requests changed the character: %+v", got)
	}
}

func TestLogoutEndsOnlyThisSession(t *testing.T) {
	srv, _, a := authServer(t)
	seedSession(t, a.db, testOwner, "second-device")

	wantStatus(t, call(t, srv, http.MethodPost, "/api/auth/logout", nil), http.StatusNoContent)
	wantError(t, call(t, srv, http.MethodGet, "/api/me", nil), http.StatusUnauthorized, "unauthorized")
	wantStatus(t, callAs(t, srv, "second-device", http.MethodGet, "/api/me", nil), http.StatusOK)
	wantStatus(t, callAs(t, srv, otherToken, http.MethodGet, "/api/me", nil), http.StatusOK)
}

func TestMe(t *testing.T) {
	srv, _, _ := authServer(t)
	rec := callAs(t, srv, otherToken, http.MethodGet, "/api/me", nil)
	wantStatus(t, rec, http.StatusOK)
	var u User
	into(t, rec, &u)
	if u.ID != otherUser || u.DiscordUsername != "name-"+otherUser || u.CreatedAt.IsZero() {
		t.Errorf("me = %+v", u)
	}
	if strings.Contains(rec.Body.String(), "discord-") {
		t.Errorf("the Discord id shouldn't be sent: %s", rec.Body.String())
	}
	wantError(t, call(t, srv, http.MethodPost, "/api/me", nil), http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestDeletingAUserEndsTheirSessions(t *testing.T) {
	srv, _, a := authServer(t)
	if _, err := a.db.Exec(`DELETE FROM users WHERE id = ?`, otherUser); err != nil {
		t.Fatal(err)
	}
	wantError(t, callAs(t, srv, otherToken, http.MethodGet, "/api/me", nil), http.StatusUnauthorized, "unauthorized")
}

// ---------------------------------------------------------------------------
// Ownership over HTTP
// ---------------------------------------------------------------------------

func TestOtherUsersCharactersLookMissing(t *testing.T) {
	srv, store, _ := authServer(t)
	saveFixture(t, store, Character{ID: "mine", Name: "Mine"})

	// Whatever user B tries, the answer must be the one for a character that doesn't exist.
	requests := []struct {
		method, suffix string
		body           any
	}{
		{"GET", "", nil},
		{"PUT", "", `{"name":"Stolen","version":1}`},
		{"PATCH", "", `{"name":"Stolen","version":1}`},
		{"DELETE", "", nil},
		{"GET", "/sheet", nil},
		{"POST", "/hp", `{"damage":1}`},
		{"POST", "/coins", `{"gain":{"gp":1}}`},
		{"POST", "/rest", `{"type":"long"}`},
		{"POST", "/inventory", `{"item_id":"x"}`},
		{"GET", "/choice-options?feature_id=a&choice_id=b", nil},
	}
	for _, q := range requests {
		foreign := callAs(t, srv, otherToken, q.method, "/api/characters/mine"+q.suffix, q.body)
		missing := callAs(t, srv, otherToken, q.method, "/api/characters/nope"+q.suffix, q.body)
		wantError(t, foreign, http.StatusNotFound, "not_found")
		want := strings.ReplaceAll(missing.Body.String(), "nope", "mine")
		if foreign.Body.String() != want {
			t.Errorf("%s %s: someone else's character and a missing one differ:\n %s\n %s", q.method, q.suffix, foreign.Body, want)
		}
	}

	var list []CharacterSummary
	into(t, callAs(t, srv, otherToken, http.MethodGet, "/api/characters", nil), &list)
	if len(list) != 0 {
		t.Errorf("B sees %+v", list)
	}
	if got := getChar(t, srv, "mine"); got.Name != "Mine" || got.Version != 1 {
		t.Errorf("B changed A's character: %+v", got)
	}
}

func TestTwoUsersCanUseTheSameID(t *testing.T) {
	srv, _, _ := authServer(t)
	wantStatus(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"temi","name":"A's"}`), http.StatusCreated)
	wantStatus(t, callAs(t, srv, otherToken, http.MethodPost, "/api/characters", `{"id":"temi","name":"B's"}`), http.StatusCreated)

	var c Character
	into(t, callAs(t, srv, otherToken, http.MethodGet, "/api/characters/temi", nil), &c)
	if c.Name != "B's" {
		t.Errorf("B reads %q", c.Name)
	}
	if got := getChar(t, srv, "temi"); got.Name != "A's" {
		t.Errorf("A reads %q", got.Name)
	}
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"temi"}`), http.StatusConflict, "already_exists")
}
