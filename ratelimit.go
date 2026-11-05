package character

// ratelimit.go - in-memory rate limits for the sign-in endpoints. They reset when the server restarts.

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type rateRule struct {
	limit  int
	window time.Duration
}

// rateLimiter counts events per key, against every one of its rules at once.
type rateLimiter struct {
	rules   []rateRule
	longest time.Duration
	now     func() time.Time

	mu        sync.Mutex
	hits      map[string][]time.Time // oldest first
	lastSweep time.Time
}

func newRateLimiter(now func() time.Time, rules ...rateRule) *rateLimiter {
	l := &rateLimiter{rules: rules, now: now, hits: map[string][]time.Time{}}
	for _, r := range rules {
		l.longest = max(l.longest, r.window)
	}
	return l
}

// allow records an event for key unless that would break a rule, in which case it records nothing
// and returns how long to wait.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if wait := l.wait(key); wait > 0 {
		return false, wait
	}
	l.hits[key] = append(l.hits[key], l.now())
	return true, 0
}

// peek says whether another event would be allowed, without recording one.
func (l *rateLimiter) peek(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	wait := l.wait(key)
	return wait == 0, wait
}

// record counts an event whether or not it was allowed.
func (l *rateLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.hits[key], l.now())
}

func (l *rateLimiter) wait(key string) time.Duration {
	now := l.now()
	l.sweep(now)

	hits := l.hits[key]
	for len(hits) > 0 && now.Sub(hits[0]) >= l.longest {
		hits = hits[1:]
	}
	if len(hits) > 0 {
		l.hits[key] = hits
	} else {
		delete(l.hits, key)
	}

	var wait time.Duration
	for _, r := range l.rules {
		if len(hits) >= r.limit { // the limit-th newest hit has to leave the window
			wait = max(wait, r.window-now.Sub(hits[len(hits)-r.limit]))
		}
	}
	return wait
}

// sweep forgets keys with nothing left in any window, so the map can't grow without bound.
func (l *rateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for key, hits := range l.hits {
		if now.Sub(hits[len(hits)-1]) >= l.longest {
			delete(l.hits, key)
		}
	}
}

// clientIP returns the address to rate-limit by. Clients can set any header they like, so one is
// only believed when the operator names it, because the server sits behind a proxy that sets it.
// The last value in a list is the one that proxy added. IPv6 clients are limited by their /64,
// since one household or server typically controls all of it.
func (s *Server) clientIP(r *http.Request) string {
	raw := r.RemoteAddr
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	if s.proxyHeader != "" {
		values := strings.Split(r.Header.Get(s.proxyHeader), ",")
		if last := strings.TrimSpace(values[len(values)-1]); last != "" {
			raw = last
		}
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return raw
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}
