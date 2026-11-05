package character

// otp_test.go - signing in with a code, using a fake sender that captures what would be sent.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	knownName = "name-" + testOwner
	knownID   = "discord-" + testOwner
)

type fakeSender struct {
	mu    sync.Mutex
	codes []string
	ids   []string
	err   error
	block chan struct{} // when set, SendCode waits for it
}

func (f *fakeSender) SendCode(ctx context.Context, discordID, code string) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.codes = append(f.codes, code)
	f.ids = append(f.ids, discordID)
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.codes)
}

func (f *fakeSender) last(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.codes) == 0 {
		t.Fatal("no code was sent")
	}
	return f.codes[len(f.codes)-1]
}

type otpEnv struct {
	srv    *Server
	auth   *Auth
	sender *fakeSender
	clock  *fakeClock
	ips    atomic.Int64
}

func newOTPEnv(t *testing.T) *otpEnv {
	t.Helper()
	cfg, _ := testConfig(t)
	sender := &fakeSender{}
	clock := newFakeClock()
	cfg.Auth = NewAuth(cfg.Auth.db, sender)
	cfg.Auth.now = clock.now
	t.Cleanup(cfg.Auth.Wait) // runs before the database closes
	return &otpEnv{srv: NewServer(cfg), auth: cfg.Auth, sender: sender, clock: clock}
}

// post sends a request from a new address each time, so per-IP limits stay out of the way.
func (e *otpEnv) post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	n := e.ips.Add(1)
	return e.postFrom(t, fmt.Sprintf("10.0.%d.%d:1234", n/250, n%250), path, body)
}

func (e *otpEnv) postFrom(t *testing.T, remoteAddr, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	return serve(e.srv, req)
}

func (e *otpEnv) request(t *testing.T, username string) *httptest.ResponseRecorder {
	t.Helper()
	rec := e.post(t, "/api/auth/otp/request", fmt.Sprintf(`{"username":%q}`, username))
	e.auth.Wait()
	return rec
}

func (e *otpEnv) verify(t *testing.T, username, code string) *httptest.ResponseRecorder {
	t.Helper()
	return e.post(t, "/api/auth/otp/verify", fmt.Sprintf(`{"username":%q,"code":%q}`, username, code))
}

// wrong returns a well-formed code that isn't the right one.
func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

func TestOTPSignIn(t *testing.T) {
	e := newOTPEnv(t)
	wantStatus(t, e.request(t, knownName), http.StatusAccepted)

	code := e.sender.last(t)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Errorf("code %q should be 6 digits", code)
	}
	if e.sender.ids[0] != knownID {
		t.Errorf("sent to %q, want %q", e.sender.ids[0], knownID)
	}

	rec := e.verify(t, knownName, code)
	wantStatus(t, rec, http.StatusOK)
	var sess SessionResponse
	into(t, rec, &sess)
	if sess.Token == "" || sess.ExpiresAt.Before(e.clock.now().Add(sessionLifetime-time.Minute)) {
		t.Errorf("session = %+v", sess)
	}

	rec = callAs(t, e.srv, sess.Token, http.MethodGet, "/api/me", nil)
	wantStatus(t, rec, http.StatusOK)
	var u User
	into(t, rec, &u)
	if u.ID != testOwner {
		t.Errorf("signed in as %q, want %q", u.ID, testOwner)
	}
}

func TestOTPOnlyTheHashIsStored(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	var stored []byte
	if err := e.auth.db.QueryRow(`SELECT code_hash FROM otp_codes WHERE user_id = ?`, testOwner).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(code)) || !bytes.Equal(stored, hashCode(testOwner, code)) {
		t.Errorf("stored %x, want the hash of the code", stored)
	}
}

func TestOTPUsernameIgnoresCaseAndSpaces(t *testing.T) {
	e := newOTPEnv(t)
	wantStatus(t, e.request(t, "  "+strings.ToUpper(knownName)+" "), http.StatusAccepted)
	wantStatus(t, e.verify(t, strings.ToUpper(knownName), e.sender.last(t)), http.StatusOK)
}

func TestOTPIsSingleUse(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	wantStatus(t, e.verify(t, knownName, code), http.StatusOK)
	wantError(t, e.verify(t, knownName, code), http.StatusUnauthorized, "unauthorized")
}

func TestOTPConcurrentUseCreatesOneSession(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	var before int
	e.auth.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&before)

	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.verify(t, knownName, code).Code == http.StatusOK {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Errorf("%d requests signed in with one code, want 1", ok.Load())
	}
	var after int
	e.auth.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&after)
	if after != before+1 {
		t.Errorf("sessions went from %d to %d, want one more", before, after)
	}
}

func TestOTPExpires(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)

	e.clock.advance(codeLifetime)
	wantError(t, e.verify(t, knownName, code), http.StatusUnauthorized, "unauthorized")

	e.clock.advance(time.Hour) // past the request limits
	e.request(t, knownName)
	e.clock.advance(codeLifetime - time.Second)
	wantStatus(t, e.verify(t, knownName, e.sender.last(t)), http.StatusOK)
}

func TestOTPInvalidatedAfterFiveFailures(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)

	for i := 0; i < maxCodeAttempts; i++ {
		wantError(t, e.verify(t, knownName, wrong(code)), http.StatusUnauthorized, "unauthorized")
	}
	wantError(t, e.verify(t, knownName, code), http.StatusUnauthorized, "unauthorized")

	var n int
	e.auth.db.QueryRow(`SELECT COUNT(*) FROM otp_codes`).Scan(&n)
	if n != 0 {
		t.Error("the code should be gone after 5 failures")
	}
}

// The row is normally deleted at the limit, but the limit must hold without that: the delete can
// fail, and parallel requests can arrive before it runs.
func TestOTPCodeAtTheLimitIsRefusedEvenIfStillStored(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	if _, err := e.auth.db.Exec(`UPDATE otp_codes SET attempts = ?`, maxCodeAttempts); err != nil {
		t.Fatal(err)
	}
	wantError(t, e.verify(t, knownName, code), http.StatusUnauthorized, "unauthorized")
}

func TestOTPFifthAttemptCanStillSucceed(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	for i := 0; i < maxCodeAttempts-1; i++ {
		wantError(t, e.verify(t, knownName, wrong(code)), http.StatusUnauthorized, "unauthorized")
	}
	wantStatus(t, e.verify(t, knownName, code), http.StatusOK)
}

func TestOTPParallelGuessesCantBeatTheAttemptLimit(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := e.auth.VerifyCode(context.Background(), knownName, wrong(code))
			if !errors.Is(err, errBadCode) {
				t.Errorf("err = %v, want errBadCode", err)
			}
		}()
	}
	wg.Wait()
	if _, _, err := e.auth.VerifyCode(context.Background(), knownName, code); !errors.Is(err, errBadCode) {
		t.Errorf("after 20 failures: err = %v, want the code unusable", err)
	}
}

func TestOTPNewCodeReplacesTheOld(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	first := e.sender.last(t)
	wantError(t, e.verify(t, knownName, wrong(first)), http.StatusUnauthorized, "unauthorized")

	e.clock.advance(time.Minute)
	e.request(t, knownName)
	second := e.sender.last(t)

	var attempts, rows int
	e.auth.db.QueryRow(`SELECT COUNT(*), attempts FROM otp_codes`).Scan(&rows, &attempts)
	if rows != 1 || attempts != 0 {
		t.Errorf("%d rows at %d attempts, want one fresh row", rows, attempts)
	}
	if first != second {
		wantError(t, e.verify(t, knownName, first), http.StatusUnauthorized, "unauthorized")
	}
	wantStatus(t, e.verify(t, knownName, second), http.StatusOK)
}

// ---------------------------------------------------------------------------
// Giving nothing away
// ---------------------------------------------------------------------------

func TestOTPRequestLooksTheSameForEveryone(t *testing.T) {
	e := newOTPEnv(t)
	known := e.request(t, knownName)
	unknown := e.request(t, "nobody-here")

	failing := newOTPEnv(t)
	failing.sender.err = errors.New("cannot send messages to this user")
	failed := failing.request(t, knownName)

	wantStatus(t, known, http.StatusAccepted)
	for name, rec := range map[string]*httptest.ResponseRecorder{"unknown": unknown, "failed send": failed} {
		if rec.Code != known.Code || rec.Body.String() != known.Body.String() {
			t.Errorf("%s: %d %s, want the same as a known user: %d %s", name, rec.Code, rec.Body, known.Code, known.Body)
		}
		if fmt.Sprint(rec.Header()) != fmt.Sprint(known.Header()) {
			t.Errorf("%s: headers %v differ from %v", name, rec.Header(), known.Header())
		}
	}
	if e.sender.count() != 1 {
		t.Errorf("%d codes sent, want 1: only the known user gets one", e.sender.count())
	}
}

func TestOTPResponseDoesntWaitForTheSend(t *testing.T) {
	e := newOTPEnv(t)
	e.sender.block = make(chan struct{})
	rec := e.post(t, "/api/auth/otp/request", fmt.Sprintf(`{"username":%q}`, knownName))
	wantStatus(t, rec, http.StatusAccepted) // would hang here if the handler waited for Discord
	if e.sender.count() != 0 {
		t.Error("the send should still be in progress")
	}
	close(e.sender.block)
	e.auth.Wait()
	if e.sender.count() != 1 {
		t.Error("the code should have been sent")
	}
}

func TestOTPVerifyFailuresLookTheSame(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)

	wrongCode := e.verify(t, knownName, wrong(code))
	unknownUser := e.verify(t, "nobody-here", code)

	expired := newOTPEnv(t)
	expired.request(t, knownName)
	expiredCode := expired.sender.last(t)
	expired.clock.advance(codeLifetime + time.Second)
	expiredRec := expired.verify(t, knownName, expiredCode)

	noCode := newOTPEnv(t).verify(t, knownName, "123456")

	wantError(t, wrongCode, http.StatusUnauthorized, "unauthorized")
	for name, rec := range map[string]*httptest.ResponseRecorder{"unknown user": unknownUser, "expired": expiredRec, "no code issued": noCode} {
		if rec.Code != wrongCode.Code || rec.Body.String() != wrongCode.Body.String() {
			t.Errorf("%s: %d %s, want the same as a wrong code: %d %s", name, rec.Code, rec.Body, wrongCode.Code, wrongCode.Body)
		}
	}
}

func TestOTPBadBodies(t *testing.T) {
	e := newOTPEnv(t)
	for _, body := range []string{`{}`, `{"username":""}`, `{"username":"   "}`, `{"name":"x"}`, `not json`,
		fmt.Sprintf(`{"username":%q}`, strings.Repeat("a", maxUsernameLen+1))} {
		wantError(t, e.post(t, "/api/auth/otp/request", body), http.StatusBadRequest, "bad_request")
		wantError(t, e.post(t, "/api/auth/otp/verify", body), http.StatusBadRequest, "bad_request")
	}
	wantError(t, callAs(t, e.srv, "", http.MethodGet, "/api/auth/otp/request", nil), http.StatusMethodNotAllowed, "method_not_allowed")
	wantError(t, callAs(t, e.srv, "", http.MethodGet, "/api/auth/otp/verify", nil), http.StatusMethodNotAllowed, "method_not_allowed")
	wantError(t, callAs(t, e.srv, "", http.MethodPost, "/api/auth/otp/nope", nil), http.StatusNotFound, "not_found")
}

func TestOTPLogsNeverContainTheCode(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	e.verify(t, knownName, wrong(code))
	e.verify(t, knownName, code)

	e.sender.err = errors.New("nope")
	e.clock.advance(time.Minute)
	e.request(t, knownName)

	if !strings.Contains(logged.String(), "nope") {
		t.Errorf("a failed send should be logged: %q", logged.String())
	}
	if strings.Contains(logged.String(), code) {
		t.Errorf("the log contains the code: %q", logged.String())
	}
}

// ---------------------------------------------------------------------------
// Rate limits
// ---------------------------------------------------------------------------

func TestOTPRequestLimitsPerUser(t *testing.T) {
	// Unknown names must hit exactly the same limits, or a 429 would reveal who exists.
	for _, name := range []string{knownName, "nobody-here"} {
		e := newOTPEnv(t)
		wantStatus(t, e.request(t, name), http.StatusAccepted)

		rec := e.request(t, name)
		wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
		if rec.Header().Get("Retry-After") != "60" {
			t.Errorf("%s: Retry-After = %q, want 60", name, rec.Header().Get("Retry-After"))
		}
		wantError(t, e.request(t, strings.ToUpper(name)), http.StatusTooManyRequests, "rate_limited")

		for i := 0; i < 4; i++ {
			e.clock.advance(time.Minute)
			wantStatus(t, e.request(t, name), http.StatusAccepted)
		}
		e.clock.advance(time.Minute)
		wantError(t, e.request(t, name), http.StatusTooManyRequests, "rate_limited")

		e.clock.advance(time.Hour)
		wantStatus(t, e.request(t, name), http.StatusAccepted)
	}
}

func TestOTPRefusedRequestDoesntReplaceTheCode(t *testing.T) {
	e := newOTPEnv(t)
	e.request(t, knownName)
	code := e.sender.last(t)
	e.request(t, knownName)
	if e.sender.count() != 1 {
		t.Errorf("%d codes sent, want 1", e.sender.count())
	}
	wantStatus(t, e.verify(t, knownName, code), http.StatusOK)
}

func TestOTPFailedVerifiesAreCappedAcrossCodes(t *testing.T) {
	for _, name := range []string{knownName, "nobody-here"} {
		e := newOTPEnv(t)
		for i := 0; i < verifyRules[0].limit; i++ {
			wantError(t, e.verify(t, name, "abcdef"), http.StatusUnauthorized, "unauthorized")
		}
		wantError(t, e.verify(t, name, "abcdef"), http.StatusTooManyRequests, "rate_limited")
		e.clock.advance(time.Hour)
		wantError(t, e.verify(t, name, "abcdef"), http.StatusUnauthorized, "unauthorized")
	}
}

func TestOTPLimitsPerIPCoverBothEndpoints(t *testing.T) {
	e := newOTPEnv(t)
	const addr = "203.0.113.9:555"
	for i := 0; i < ipRules[0].limit; i++ {
		path, body := "/api/auth/otp/request", fmt.Sprintf(`{"username":"user%d"}`, i)
		if i%2 == 0 {
			path, body = "/api/auth/otp/verify", fmt.Sprintf(`{"username":"user%d","code":"1"}`, i)
		}
		if rec := e.postFrom(t, addr, path, body); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited too early", i)
		}
	}
	e.auth.Wait()
	wantError(t, e.postFrom(t, addr, "/api/auth/otp/request", `{"username":"another"}`), http.StatusTooManyRequests, "rate_limited")
	wantError(t, e.postFrom(t, addr, "/api/auth/otp/verify", `{"username":"another","code":"1"}`), http.StatusTooManyRequests, "rate_limited")

	// A different port is the same client; a different address isn't.
	wantError(t, e.postFrom(t, "203.0.113.9:556", "/api/auth/otp/verify", `{"username":"another","code":"1"}`), http.StatusTooManyRequests, "rate_limited")
	wantError(t, e.postFrom(t, "203.0.113.10:555", "/api/auth/otp/verify", `{"username":"another","code":"1"}`), http.StatusUnauthorized, "unauthorized")
}

func TestClientIP(t *testing.T) {
	plain, _, _ := authServer(t)
	cfg, _ := testConfig(t)
	cfg.TrustedProxyHeader = "X-Forwarded-For"
	proxied := NewServer(cfg)

	req := func(remote, forwarded string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = remote
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		return r
	}
	cases := []struct {
		name string
		srv  *Server
		req  *http.Request
		want string
	}{
		{"remote address", plain, req("198.51.100.1:99", ""), "198.51.100.1"},
		{"the header is ignored unless configured", plain, req("198.51.100.1:99", "1.2.3.4"), "198.51.100.1"},
		{"configured header", proxied, req("10.0.0.1:99", "1.2.3.4"), "1.2.3.4"},
		{"the proxy adds to the end, so earlier values can be forged", proxied, req("10.0.0.1:99", "6.6.6.6, 1.2.3.4"), "1.2.3.4"},
		{"no header falls back", proxied, req("10.0.0.1:99", ""), "10.0.0.1"},
		{"mapped IPv4", plain, req("[::ffff:198.51.100.1]:99", ""), "198.51.100.1"},
		{"IPv6 by /64", plain, req("[2001:db8:1:2:3:4:5:6]:99", ""), "2001:db8:1:2::/64"},
	}
	for _, c := range cases {
		if got := c.srv.clientIP(c.req); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRateLimiterForgetsOldKeys(t *testing.T) {
	clock := newFakeClock()
	l := newRateLimiter(clock.now, rateRule{1, time.Minute})
	l.allow("a")
	l.allow("b")
	clock.advance(2 * time.Minute)
	l.allow("c")
	if len(l.hits) != 1 {
		t.Errorf("%d keys remembered, want only the live one: %v", len(l.hits), l.hits)
	}
}

// ---------------------------------------------------------------------------
// Senders
// ---------------------------------------------------------------------------

func TestConsoleSenderNeedsDevMode(t *testing.T) {
	if _, err := NewCodeSender("", false); err == nil {
		t.Error("with no token and no dev mode, there should be no sender")
	}
	s, err := NewCodeSender("", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(ConsoleSender); !ok {
		t.Errorf("dev mode with no token = %T, want ConsoleSender", s)
	}
	if err := (ConsoleSender{}).SendCode(context.Background(), "1", "123456"); err == nil {
		t.Error("a ConsoleSender built without dev mode must refuse to send")
	}
	for _, dev := range []bool{false, true} {
		s, err := NewCodeSender("placeholder-token", dev)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.(*DiscordSender); !ok {
			t.Errorf("a token with dev=%v = %T, want DiscordSender", dev, s)
		}
	}
}
