package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

const (
	testPathConsoleSession  = "/api/v1/auth/session"
	testPathConsoleRegister = "/api/v1/auth/session/register"
)

// serverClock is a settable clock a test moves forward.
type serverClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *serverClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *serverClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newClockedServer(t *testing.T) (*Server, *serverClock) {
	t.Helper()
	clock := &serverClock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	cfg := testServerConfig(t)
	cfg.Clock = clock.Now
	return NewServer(cfg), clock
}

// console is a browser signed in to the console: the cookie the server set, and the
// forged-request value the sign-in returned.
type console struct {
	srv    *Server
	cookie *http.Cookie
	csrf   string
}

func (c console) do(method, path string, body any, sendCSRF bool) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if sendCSRF {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	w := httptest.NewRecorder()
	c.srv.ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "og_session" {
			return cookie
		}
	}
	t.Fatalf("no og_session cookie in %v", w.Result().Header.Values("Set-Cookie"))
	return nil
}

func signIn(t *testing.T, srv *Server, email string) (console, map[string]any) {
	t.Helper()
	w := doRequest(srv, http.MethodPost, testPathConsoleSession, "",
		map[string]string{"email": email, "password": "password123"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	csrf, _ := body["csrf_token"].(string)
	return console{srv: srv, cookie: sessionCookie(t, w), csrf: csrf}, body
}

func TestConsoleSignInSetsAStrictCookieAndReturnsNoPass(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "signin-"+uuid.NewString()[:8]+"@example.com", false)

	w := doRequestWithHeaders(srv, http.MethodPost, testPathConsoleSession, "",
		map[string]string{"email": user.Email, "password": "password123"},
		map[string]string{"X-Forwarded-Proto": "https"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookie := sessionCookie(t, w)
	assert.True(t, cookie.HttpOnly, "the page cannot read the session id")
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)
	assert.Empty(t, cookie.Domain, "no sibling host receives it")
	assert.Len(t, w.Result().Cookies(), 1, "no cookie carries the forged-request value")

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotContains(t, body, "token", "the body carries no pass")
	assert.Equal(t, user.TenantID.String(), body["tenant_id"])
	assert.NotEmpty(t, body["csrf_token"])
	assert.NotContains(t, cookie.Value, body["csrf_token"])
	assert.EqualValues(t, 1800, body["idle_timeout_seconds"])
}

func TestTheSessionCookieIsSecureWheneverTheConsoleIsReachedOverTLS(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "scheme-"+uuid.NewString()[:8]+"@example.com", false)
	tests := []struct {
		name    string
		target  string
		headers map[string]string
		secure  bool
	}{
		{name: "tls to the server", target: "https://console.example.com" + testPathConsoleSession, secure: true},
		{name: "tls ended at the edge", target: testPathConsoleSession,
			headers: map[string]string{"X-Forwarded-Proto": "https"}, secure: true},
		{name: "edge reached over http", target: "https://console.example.com" + testPathConsoleSession,
			headers: map[string]string{"X-Forwarded-Proto": "http"}, secure: false},
		{name: "plain http test stack", target: testPathConsoleSession, secure: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signedIn := doRequestWithHeaders(srv, http.MethodPost, tt.target, "",
				map[string]string{"email": user.Email, "password": "password123"}, tt.headers)
			require.Equal(t, http.StatusOK, signedIn.Code, signedIn.Body.String())
			cookie := sessionCookie(t, signedIn)
			assert.Equal(t, tt.secure, cookie.Secure)

			c := console{srv: srv, cookie: cookie}
			c.csrf, _ = decodeBody(t, signedIn)["csrf_token"].(string)
			req := httptest.NewRequest(http.MethodDelete, tt.target, nil)
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", c.csrf)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			signedOut := httptest.NewRecorder()
			srv.ServeHTTP(signedOut, req)
			require.Equal(t, http.StatusNoContent, signedOut.Code, signedOut.Body.String())
			assert.Equal(t, tt.secure, sessionCookie(t, signedOut).Secure,
				"the clearing cookie matches the one it replaces")
		})
	}
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestTheConsoleSessionAnswersAPageThatReloads(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "reload-"+uuid.NewString()[:8]+"@example.com", false)
	c, signedIn := signIn(t, srv, user.Email)

	w := c.do(http.MethodGet, testPathConsoleSession, nil, false)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, signedIn["csrf_token"], body["csrf_token"])
	assert.Equal(t, user.Email, body["user"].(map[string]any)["email"])
}

func TestAConsoleChangeNeedsTheForgedRequestValue(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "csrf-"+uuid.NewString()[:8]+"@example.com", true)
	require.NoError(t, srv.securityGroups.AddMember(testTenantContext(t), auth.AdminGroupID, user.ID))
	c, _ := signIn(t, srv, user.Email)
	site := map[string]string{"name": "north-" + uuid.NewString()[:8]}

	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, testPathSites, site, false).Code)
	forged := c
	forged.csrf = "not-the-value"
	assert.Equal(t, http.StatusForbidden, forged.do(http.MethodPost, testPathSites, site, true).Code)
	assert.Equal(t, http.StatusCreated, c.do(http.MethodPost, testPathSites, site, true).Code)
	assert.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code,
		"a read needs no forged-request value")
}

func TestABearerChangeNeedsNoForgedRequestValue(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "bearer-"+uuid.NewString()[:8]+"@example.com", true)

	w := doRequest(srv, http.MethodPost, testPathSites, token, map[string]string{"name": "south"})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestAForgedSessionCookieIsRefused(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "forged-"+uuid.NewString()[:8]+"@example.com", false)
	c, _ := signIn(t, srv, user.Email)

	tampered := *c.cookie
	first := "A"
	if tampered.Value[:1] == first {
		first = "B"
	}
	tampered.Value = first + tampered.Value[1:]
	c.cookie = &tampered
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code)
}

func TestThirtyIdleMinutesSignTheConsoleOut(t *testing.T) {
	t.Parallel()
	srv, clock := newClockedServer(t)
	user, _ := seedTestUser(t, srv, srv.jwt, "idle-"+uuid.NewString()[:8]+"@example.com", false)
	c, _ := signIn(t, srv, user.Email)

	clock.advance(29 * time.Minute)
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code,
		"a request inside the idle limit is answered and renews it")
	clock.advance(29 * time.Minute)
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code)

	clock.advance(31 * time.Minute)
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code)
}

func TestEightHoursSignTheConsoleOutHoweverActive(t *testing.T) {
	t.Parallel()
	srv, clock := newClockedServer(t)
	user, _ := seedTestUser(t, srv, srv.jwt, "total-"+uuid.NewString()[:8]+"@example.com", false)
	c, _ := signIn(t, srv, user.Email)

	for elapsed := 20 * time.Minute; elapsed < 8*time.Hour; elapsed += 20 * time.Minute {
		clock.advance(20 * time.Minute)
		require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code,
			"at %s", elapsed)
	}
	clock.advance(20 * time.Minute)
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code)
}

func TestSigningOutMakesTheOldCookieUseless(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, _ := seedTestUser(t, srv, cfg, "signout-"+uuid.NewString()[:8]+"@example.com", false)
	c, _ := signIn(t, srv, user.Email)

	w := c.do(http.MethodDelete, testPathConsoleSession, nil, true)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	cleared := sessionCookie(t, w)
	assert.Negative(t, cleared.MaxAge, "the browser is told to drop the cookie")

	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/v1/users/me", nil, false).Code)
}

func TestConsoleRegistrationSignsIn(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	email := "joins-" + uuid.NewString()[:8] + "@example.com"

	w := doRequest(srv, http.MethodPost, testPathConsoleRegister, "",
		map[string]string{"email": email, "password": "password123", "display_name": "Joins"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotContains(t, body, "token")

	c := console{srv: srv, cookie: sessionCookie(t, w)}
	me := c.do(http.MethodGet, "/api/v1/users/me", nil, false)
	require.Equal(t, http.StatusOK, me.Code)
	assert.Contains(t, me.Body.String(), email)
}
