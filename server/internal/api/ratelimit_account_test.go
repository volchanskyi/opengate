package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// neverRefills is a refill rate no test outlives, so a spent one-request allowance stays spent.
const neverRefills = 1e-9

// healthAs sends one API request from addr, carrying token when it is set, and returns the status.
func healthAs(srv *Server, addr, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.RemoteAddr = addr
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code
}

// sendUntilRefused spends the allowance of whatever bucket addr and token fall in.
func sendUntilRefused(t *testing.T, srv *Server, addr, token string) {
	t.Helper()
	for range 2000 {
		if healthAs(srv, addr, token) == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("2000 requests from %s were never refused", addr)
}

// consoleHealthAs sends one API request from addr with the console's session cookie.
func consoleHealthAs(c console, addr string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.RemoteAddr = addr
	req.AddCookie(c.cookie)
	rec := httptest.NewRecorder()
	c.srv.ServeHTTP(rec, req)
	return rec.Code
}

// bucketProbe is the server's account limiter with one request per bucket that never refills,
// so the second request into a bucket is refused however slowly the test runs.
type bucketProbe struct{ limited http.Handler }

func newBucketProbe(srv *Server, cfg *auth.JWTConfig) bucketProbe {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return bucketProbe{AccountRateLimiter(neverRefills, 1, nil, cfg, srv.consoleSessionKeyFor)(ok)}
}

// send answers one request from addr carrying token or cookie, whichever is set.
func (p bucketProbe) send(addr, token string, cookie *http.Cookie) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.RemoteAddr = addr
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	p.limited.ServeHTTP(rec, req)
	return rec.Code
}

// refusalsAtOnce runs every sender at once, each sending n requests, and counts the 429s.
func refusalsAtOnce(senders []func() int, n int) int {
	var (
		wg      sync.WaitGroup
		refused atomic.Int64
	)
	for _, send := range senders {
		wg.Go(func() {
			for range n {
				if send() == http.StatusTooManyRequests {
					refused.Add(1)
				}
			}
		})
	}
	wg.Wait()
	return int(refused.Load())
}

func accountToken(t *testing.T, cfg *auth.JWTConfig) string {
	t.Helper()
	token, err := cfg.GenerateToken(uuid.New(), "tech@northwind.test", false, uuid.New())
	require.NoError(t, err)
	return token
}

func TestAPIRequestsAreCountedUnderTheAccountThatMadeThem(t *testing.T) {
	srv, cfg := newTestServer(t)
	forger := &auth.JWTConfig{Secret: "another-secret-key-of-32-bytes-or-more", Issuer: cfg.Issuer, Duration: time.Hour}
	const tooMany = http.StatusTooManyRequests

	t.Run("forty technicians behind one office address open a machine at once", func(t *testing.T) {
		const office = "203.0.113.10:41000"
		senders := make([]func() int, 0, 40)
		for range 40 {
			token := accountToken(t, cfg)
			senders = append(senders, func() int { return healthAs(srv, office, token) })
		}
		assert.Zero(t, refusalsAtOnce(senders, 6), "240 requests from forty accounts share no allowance")
	})

	t.Run("forty technicians signed in to the console from one office share no allowance", func(t *testing.T) {
		const office = "203.0.113.50:41000"
		senders := make([]func() int, 0, 40)
		for range 40 {
			user, _ := seedTestUser(t, srv, cfg, "office-"+uuid.NewString()[:8]+"@northwind.test", false)
			c, _ := signIn(t, srv, user.Email)
			senders = append(senders, func() int { return consoleHealthAs(c, office) })
		}
		assert.Zero(t, refusalsAtOnce(senders, 6), "240 requests from forty console sessions share no allowance")
	})

	t.Run("the API refuses an address once its allowance is spent", func(t *testing.T) {
		sendUntilRefused(t, srv, "203.0.113.30:41000", "")
	})

	t.Run("one account past its allowance leaves another on the same address alone", func(t *testing.T) {
		const office = "203.0.113.20:41000"
		probe := newBucketProbe(srv, cfg)
		spent := accountToken(t, cfg)
		require.Equal(t, http.StatusOK, probe.send(office, spent, nil))
		assert.Equal(t, tooMany, probe.send(office, spent, nil))
		assert.Equal(t, http.StatusOK, probe.send(office, accountToken(t, cfg), nil))
	})

	t.Run("a request without a token is counted under its address", func(t *testing.T) {
		const office = "203.0.113.30:41000"
		probe := newBucketProbe(srv, cfg)
		require.Equal(t, http.StatusOK, probe.send(office, "", nil))
		assert.Equal(t, tooMany, probe.send(office, "", nil), "the address's allowance stays spent")
		assert.Equal(t, http.StatusOK, probe.send(office, accountToken(t, cfg), nil),
			"a signed-in technician keeps an allowance of their own")
	})

	t.Run("one console session past its allowance leaves another alone", func(t *testing.T) {
		const office = "203.0.113.70:41000"
		probe := newBucketProbe(srv, cfg)
		first, _ := seedTestUser(t, srv, cfg, "first-"+uuid.NewString()[:8]+"@northwind.test", false)
		second, _ := seedTestUser(t, srv, cfg, "second-"+uuid.NewString()[:8]+"@northwind.test", false)
		spent, _ := signIn(t, srv, first.Email)
		other, _ := signIn(t, srv, second.Email)
		require.Equal(t, http.StatusOK, probe.send(office, "", spent.cookie))
		assert.Equal(t, tooMany, probe.send(office, "", spent.cookie))
		assert.Equal(t, http.StatusOK, probe.send(office, "", other.cookie))
		assert.Equal(t, http.StatusOK, probe.send(office, "", nil), "the address keeps its own allowance")
	})

	t.Run("a forged session cookie is counted under its address", func(t *testing.T) {
		const office = "203.0.113.60:41000"
		probe := newBucketProbe(srv, cfg)
		forged := &http.Cookie{Name: "og_session", Value: "AAAA.BBBB"}
		require.Equal(t, http.StatusOK, probe.send(office, "", forged))
		assert.Equal(t, tooMany, probe.send(office, "", nil), "a forged cookie spends the address's allowance")
	})

	t.Run("a token that fails validation is counted under its address", func(t *testing.T) {
		const office = "203.0.113.40:41000"
		probe := newBucketProbe(srv, cfg)
		require.Equal(t, http.StatusOK, probe.send(office, accountToken(t, forger), nil))
		assert.Equal(t, tooMany, probe.send(office, "", nil), "a forged token spends the address's allowance")
		assert.Equal(t, tooMany, probe.send(office, accountToken(t, forger), nil),
			"a fresh forged token mints no fresh allowance")
		assert.Equal(t, http.StatusOK, probe.send(office, accountToken(t, cfg), nil))
	})
}
