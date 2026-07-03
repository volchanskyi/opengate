package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
	rps     rate.Limit
	burst   int
}

func newIPLimiter(rps float64, burst int) *ipLimiter {
	l := &ipLimiter{
		entries: make(map[string]*ipEntry),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
	go l.cleanup()
	return l
}

func (l *ipLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[ip]
	if !ok {
		e = &ipEntry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (l *ipLimiter) cleanup() {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-5 * time.Minute)
		for ip, e := range l.entries {
			if e.lastSeen.Before(cutoff) {
				delete(l.entries, ip)
			}
		}
		l.mu.Unlock()
	}
}

// RateLimiter returns middleware that applies per-IP token bucket rate limiting.
// Requests exceeding the limit receive a 429 response.
func RateLimiter(rps float64, burst int) func(http.Handler) http.Handler {
	limiter := newIPLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !limiter.get(ip).Allow() {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type emailEntry struct {
	failures    int
	windowStart time.Time
}

// emailLimiter throttles failed logins per (normalized) email address,
// independently of source IP. It complements the per-IP AuthRateLimiter so a
// distributed credential-stuffing attack spread across many IPs against a
// single account still trips a lockout. A successful login resets the counter.
type emailLimiter struct {
	mu      sync.Mutex
	entries map[string]*emailEntry
	max     int
	window  time.Duration
}

func newEmailLimiter(maxFailures int, window time.Duration) *emailLimiter {
	l := &emailLimiter{
		entries: make(map[string]*emailEntry),
		max:     maxFailures,
		window:  window,
	}
	go l.cleanup()
	return l
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// allowed reports whether a login attempt for the email may proceed. It returns
// false once max failures have accumulated within the active window.
func (l *emailLimiter) allowed(email string) bool {
	key := normalizeEmail(email)
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if time.Since(e.windowStart) > l.window {
		delete(l.entries, key)
		return true
	}
	return e.failures < l.max
}

// recordFailure registers a failed login for the email, starting a fresh window
// if none is active or the previous one has expired.
func (l *emailLimiter) recordFailure(email string) {
	key := normalizeEmail(email)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowStart) > l.window {
		e = &emailEntry{windowStart: now}
		l.entries[key] = e
	}
	e.failures++
}

// reset clears any accumulated failures for the email (call on success).
func (l *emailLimiter) reset(email string) {
	key := normalizeEmail(email)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *emailLimiter) cleanup() {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for key, e := range l.entries {
			if time.Since(e.windowStart) > l.window {
				delete(l.entries, key)
			}
		}
		l.mu.Unlock()
	}
}

func extractIP(r *http.Request) string {
	// Trust X-Forwarded-For when behind the ingress reverse proxy.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
