package character

// accesslog_test.go - tests for the request, sign-in and reload lines written to the server log.

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// logBuffer is safe to read while codes are still being sent, and logged, in the background.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog sends the standard logger to a buffer until the test ends.
func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	buf := &logBuffer{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

func TestAccessLog(t *testing.T) {
	srv, _ := newTestServer(t)
	logged := captureLog(t)

	call(t, srv, http.MethodGet, "/api/me", nil)
	callAs(t, srv, "", http.MethodGet, "/api/me", nil)
	callAs(t, srv, "", http.MethodGet, "/api/content/classes?secret=hunter2", nil)
	callAs(t, srv, "", http.MethodGet, "/api/nope%0Afake", nil)

	out := logged.String()
	for _, want := range []string{
		`GET "/api/me" 200 `, "user=" + testOwner,
		`GET "/api/me" 401 `, "user=-",
		`GET "/api/content/classes" 200 `,
		`GET "/api/nope\nfake" 404 `,
		"ip=192.0.2.1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log is missing %q:\n%s", want, out)
		}
	}
	for _, never := range []string{"hunter2", testToken} {
		if strings.Contains(out, never) {
			t.Errorf("the log contains %q:\n%s", never, out)
		}
	}
	if n := strings.Count(out, "\n"); n != 4 {
		t.Errorf("want one line per request, got %d:\n%s", n, out)
	}
}

func TestSignInEventsAreLogged(t *testing.T) {
	e := newOTPEnv(t)
	logged := captureLog(t)

	e.request(t, knownName)
	code := e.sender.last(t)
	e.verify(t, knownName, wrong(code))
	e.verify(t, knownName, code)
	e.request(t, "stranger")

	out := logged.String()
	for _, want := range []string{
		`sign-in: code requested for "` + knownName + `" from 10.0.`,
		`sign-in: wrong or expired code for "` + knownName + `"`,
		`sign-in: "` + knownName + `" signed in from 10.0.`,
		`sign-in code: "stranger" isn't an approved user`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, code) {
		t.Errorf("the log contains the code:\n%s", out)
	}
}

func TestSignInRateLimitIsLogged(t *testing.T) {
	e := newOTPEnv(t)
	logged := captureLog(t)
	for i := 0; i < 50 && !strings.Contains(logged.String(), "sign-in: rate limited 10.9.9.9"); i++ {
		e.postFrom(t, "10.9.9.9:1234", "/api/auth/otp/request", `{"username":"x"}`)
	}
	if !strings.Contains(logged.String(), "sign-in: rate limited 10.9.9.9 on /api/auth/otp/request") {
		t.Errorf("an IP hitting the limit should be logged:\n%s", logged.String())
	}
	e.auth.Wait()
}

func TestReloadIsLogged(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Library = loadExample(t)
	fail := false
	cfg.Reload = func() (*ContentLibrary, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return cfg.Library, nil
	}
	srv := NewServer(cfg)
	logged := captureLog(t)

	call(t, srv, http.MethodPost, "/api/content/reload", nil)
	fail = true
	call(t, srv, http.MethodPost, "/api/content/reload", nil)

	out := logged.String()
	if !strings.Contains(out, "content reloaded by user "+testOwner+":") {
		t.Errorf("a reload should be logged with who did it:\n%s", out)
	}
	if !strings.Contains(out, "content reload by user "+testOwner+" failed: boom") {
		t.Errorf("a failed reload should be logged:\n%s", out)
	}
}
