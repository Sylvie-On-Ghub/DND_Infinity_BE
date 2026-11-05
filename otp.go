package character

// otp.go - signing in with a one-time code sent by Discord DM.
//
// A caller must not be able to find out who has an account, so:
//   - Rate limits count the username as submitted, known or not. Otherwise only real users could
//     ever get a 429.
//   - The code is issued and sent after the 202 goes out. Otherwise real users would get a slower
//     response, because only they cause a Discord call.
//   - Unknown user, wrong code, expired code and used code are all the same 401.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	codeLifetime    = 10 * time.Minute
	maxCodeAttempts = 5
	sendTimeout     = 30 * time.Second
	maxSending      = 16 // codes being issued at once; beyond this, requests are dropped and logged
	maxUsernameLen  = 64 // Discord's limit is 32; this only stops huge keys reaching the rate limiter
)

var errBadCode = errors.New("invalid username or code")

// Rate limits. Failed verifies are capped per username as well as per code, because a new code
// resets the attempt count: without the cap, requesting codes repeatedly would allow 25 guesses an hour.
var (
	requestRules = []rateRule{{1, time.Minute}, {5, time.Hour}}
	verifyRules  = []rateRule{{10, time.Hour}}
	ipRules      = []rateRule{{10, time.Minute}, {60, time.Hour}}
)

func hashCode(userID, code string) []byte {
	sum := sha256.Sum256([]byte(userID + ":" + code))
	return sum[:]
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// RequestCode issues and sends a code in the background, if the username belongs to someone.
func (a *Auth) RequestCode(username string) {
	select {
	case a.sending <- struct{}{}:
	default:
		log.Printf("sign-in code: too many being sent at once, dropping a request")
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() { <-a.sending }()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if err := a.issueCode(ctx, username); err != nil {
			log.Printf("sign-in code: %v", err)
		}
	}()
}

// Wait blocks until codes being sent in the background have finished.
func (a *Auth) Wait() {
	a.wg.Wait()
}

func (a *Auth) issueCode(ctx context.Context, username string) error {
	u, err := UserByUsername(ctx, a.db, username)
	if errors.Is(err, ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.sender == nil {
		return errors.New("no code sender is configured")
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	now := a.now()
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO otp_codes (user_id, code_hash, expires_at, attempts, created_at)
		VALUES (?, ?, ?, 0, ?)
		ON CONFLICT (user_id) DO UPDATE
		SET code_hash = excluded.code_hash, expires_at = excluded.expires_at,
		    attempts = 0, created_at = excluded.created_at`,
		u.ID, hashCode(u.ID, code), now.Add(codeLifetime).Unix(), now.Unix()); err != nil {
		return fmt.Errorf("store code for user %s: %w", u.ID, err)
	}
	if err := a.sender.SendCode(ctx, u.DiscordID, code); err != nil {
		return fmt.Errorf("send code to user %s: %w", u.ID, err)
	}
	return nil
}

// VerifyCode exchanges a correct code for a new session token, using the code up. Anything else
// is errBadCode.
func (a *Auth) VerifyCode(ctx context.Context, username, code string) (string, time.Time, error) {
	// Count the attempt before comparing, in one statement, so parallel guesses can't get past the limit.
	var userID string
	var stored []byte
	err := a.db.QueryRowContext(ctx, `
		UPDATE otp_codes SET attempts = attempts + 1
		 WHERE user_id = (SELECT id FROM users WHERE discord_username = ?)
		   AND attempts < ? AND expires_at > ?
		RETURNING user_id, code_hash`, username, maxCodeAttempts, a.now().Unix()).Scan(&userID, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, errBadCode
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("check code: %w", err)
	}

	if subtle.ConstantTimeCompare(stored, hashCode(userID, code)) != 1 {
		if _, err := a.db.ExecContext(ctx, `DELETE FROM otp_codes WHERE user_id = ? AND attempts >= ?`, userID, maxCodeAttempts); err != nil {
			log.Printf("invalidate code for user %s: %v", userID, err) // already unusable: attempts is at the limit
		}
		return "", time.Time{}, errBadCode
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("use code: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM otp_codes WHERE user_id = ? AND code_hash = ?`, userID, stored)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("use code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("use code: %w", err)
	}
	if n != 1 { // a parallel request with the same code got there first
		return "", time.Time{}, errBadCode
	}
	token, expires, err := a.createSession(ctx, tx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, fmt.Errorf("use code: %w", err)
	}
	return token, expires, nil
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

type otpRequest struct {
	Username string `json:"username"`
}

type otpVerifyRequest struct {
	Username string `json:"username"`
	Code     string `json:"code"`
}

// OTPRequested is the one response to every accepted code request.
type OTPRequested struct {
	Message string `json:"message"`
}

// SessionResponse is the result of signing in. The token is only ever returned here.
type SessionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func writeRateLimited(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
}

func (s *Server) allowIP(w http.ResponseWriter, r *http.Request) bool {
	ok, wait := s.auth.ipLimit.allow(s.clientIP(r))
	if !ok {
		writeRateLimited(w, wait)
	}
	return ok
}

func cleanUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if username == "" || len(username) > maxUsernameLen {
		return "", badReq("username must be between 1 and %d characters", maxUsernameLen)
	}
	return username, nil
}

func (s *Server) requestCode(w http.ResponseWriter, r *http.Request) {
	if !s.allowIP(w, r) {
		return
	}
	var req otpRequest
	if err := readJSON(w, r, &req); err != nil {
		s.fail(w, err)
		return
	}
	username, err := cleanUsername(req.Username)
	if err != nil {
		s.fail(w, err)
		return
	}
	if ok, wait := s.auth.requestLimit.allow(strings.ToLower(username)); !ok {
		writeRateLimited(w, wait)
		return
	}
	s.auth.RequestCode(username)
	writeJSON(w, http.StatusAccepted, OTPRequested{Message: "If that username is approved, a login code is on its way by Discord DM."})
}

func (s *Server) verifyCode(w http.ResponseWriter, r *http.Request) {
	if !s.allowIP(w, r) {
		return
	}
	var req otpVerifyRequest
	if err := readJSON(w, r, &req); err != nil {
		s.fail(w, err)
		return
	}
	username, err := cleanUsername(req.Username)
	if err != nil {
		s.fail(w, err)
		return
	}
	key := strings.ToLower(username)
	if ok, wait := s.auth.verifyLimit.peek(key); !ok {
		writeRateLimited(w, wait)
		return
	}
	token, expires, err := s.auth.VerifyCode(r.Context(), username, strings.TrimSpace(req.Code))
	if errors.Is(err, errBadCode) {
		s.auth.verifyLimit.record(key)
		writeUnauthorized(w, errBadCode.Error())
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SessionResponse{Token: token, ExpiresAt: expires})
}
