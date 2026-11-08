package character

// auth.go - sessions and the check that protects routes.
//
// A session token is 32 random bytes, handed to the client once. Only its sha256 is stored, so a
// copy of the database can't be used to sign in. Sessions last 30 days and slide: using one
// extends it, but only once less than half its lifetime remains, so most requests don't write.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionLifetime = 30 * 24 * time.Hour

var errNoSession = errors.New("no such session")

// Auth manages sessions and, through otp.go, the sign-in codes that create them.
type Auth struct {
	db     *sql.DB
	sender CodeSender
	now    func() time.Time

	requestLimit *rateLimiter // code requests per username
	verifyLimit  *rateLimiter // failed verifies per username
	ipLimit      *rateLimiter // both endpoints together, per client address

	sending chan struct{} // slots for codes being sent in the background
	wg      sync.WaitGroup
}

func NewAuth(db *sql.DB, sender CodeSender) *Auth {
	a := &Auth{db: db, sender: sender, now: time.Now, sending: make(chan struct{}, maxSending)}
	clock := func() time.Time { return a.now() }
	a.requestLimit = newRateLimiter(clock, requestRules...)
	a.verifyLimit = newRateLimiter(clock, verifyRules...)
	a.ipLimit = newRateLimiter(clock, ipRules...)
	return a
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateSession starts a session and returns its token, which can't be recovered afterwards.
func (a *Auth) CreateSession(ctx context.Context, userID string) (string, time.Time, error) {
	return a.createSession(ctx, a.db, userID)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (a *Auth) createSession(ctx context.Context, db execer, userID string) (string, time.Time, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := a.now()
	expires := now.Add(sessionLifetime)
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, expires.Unix(), now.Unix()); err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires.UTC(), nil
}

// Authenticate returns the user a token belongs to, or errNoSession if it is unknown or expired.
func (a *Auth) Authenticate(ctx context.Context, token string) (string, error) {
	hash := hashToken(token)
	var userID string
	var expires int64
	err := a.db.QueryRowContext(ctx, `SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, hash).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNoSession
	}
	if err != nil {
		return "", fmt.Errorf("read session: %w", err)
	}
	now := a.now()
	if expires <= now.Unix() {
		return "", errNoSession
	}
	if time.Unix(expires, 0).Sub(now) < sessionLifetime/2 {
		if _, err := a.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token_hash = ?`,
			now.Add(sessionLifetime).Unix(), hash); err != nil {
			log.Printf("extend session: %v", err) // the session is still valid, so carry on
		}
	}
	return userID, nil
}

// EndSession deletes the session a token belongs to, if there is one.
func (a *Auth) EndSession(ctx context.Context, token string) error {
	if _, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes expired sessions and sign-in codes.
func (a *Auth) DeleteExpired(ctx context.Context) error {
	now := a.now().Unix()
	if _, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM otp_codes WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("delete expired codes: %w", err)
	}
	return nil
}

// RunCleanup calls DeleteExpired now and then every interval, until ctx is cancelled.
func (a *Auth) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.DeleteExpired(ctx); err != nil && ctx.Err() == nil {
			log.Printf("cleanup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "unauthorized", msg)
}

// requireAuth runs next with the signed-in user's id in the request context, or answers 401. A
// server built without Auth accepts nobody.
func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter, *http.Request)) {
	token := bearerToken(r)
	if token == "" || s.auth == nil {
		writeUnauthorized(w, "sign in required")
		return
	}
	id, err := s.auth.Authenticate(r.Context(), token)
	if errors.Is(err, errNoSession) {
		writeUnauthorized(w, "sign in required")
		return
	}
	if err != nil { // the database failing doesn't mean the token is bad, so don't sign the client out
		s.fail(w, err)
		return
	}
	if al, ok := r.Context().Value(accessLogKey{}).(*accessLog); ok {
		al.user = id
	}
	next(w, r.WithContext(withUserID(r.Context(), id)))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.EndSession(r.Context(), bearerToken(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := userByID(r.Context(), s.auth.db, userID(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
