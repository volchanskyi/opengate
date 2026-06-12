// Package main implements the Phase 13b PR-D multiserver e2e harness: a
// host-side wire driver that seeds sessions in the shared Postgres and drives raw
// relay WebSockets across two server replicas to prove the cross-server proxy
// the cross-server proxy and Redis-loss degraded-mode posture (ADR-023). It is invoked by
// `make e2e-multiserver` against the docker-compose.multiserver.yml topology and
// refuses to run unless OPENGATE_MULTISERVER_E2E=1 (see main.go).
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"nhooyr.io/websocket"
)

// exchangeTimeout bounds a single write→read round-trip across the relay. It is
// generous so the first exchange tolerates the cross-server splice establishing.
const exchangeTimeout = 10 * time.Second

// harness holds the shared seeding repos and the topology coordinates the
// scenarios drive. All fields are set once by newHarness; methods are safe to
// call sequentially (the scenarios run one at a time).
type harness struct {
	serverA string // public base URL of replica A, e.g. http://localhost:18081
	serverB string // public base URL of replica B

	store    *db.PostgresStore
	users    *auth.PostgresUsers
	groups   *device.PostgresGroups
	devices  *device.PostgresDevices
	sessions *session.PostgresSessions

	composeFile    string
	composeProject string

	logf func(format string, args ...any)
}

// newHarness connects to the shared Postgres and builds the seeding repos.
func newHarness(ctx context.Context, cfg config, logf func(string, ...any)) (*harness, error) {
	store, err := db.NewPostgresStore(ctx, cfg.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect shared postgres: %w", err)
	}
	pool := store.DB()
	return &harness{
		serverA:        cfg.serverAURL,
		serverB:        cfg.serverBURL,
		store:          store,
		users:          auth.NewPostgresUsers(pool),
		groups:         device.NewPostgresGroups(pool),
		devices:        device.NewPostgresDevices(pool),
		sessions:       session.NewPostgresSessions(pool),
		composeFile:    cfg.composeFile,
		composeProject: cfg.composeProject,
		logf:           logf,
	}, nil
}

func (h *harness) close() {
	if h.store != nil {
		_ = h.store.Close()
	}
}

// seedSession inserts the user→group→device→session chain required by the relay
// token check (validateRelayToken only verifies the session row exists) and
// returns the fresh session token. It bypasses the CreateSession API, which would
// require a live QUIC agent — this harness exercises the relay path, not agent
// control.
func (h *harness) seedSession(ctx context.Context) (string, error) {
	token := string(protocol.GenerateSessionToken())
	if err := h.seedSessionToken(ctx, token); err != nil {
		return "", err
	}
	return token, nil
}

// seedSessionToken inserts the FK chain for a session row with a caller-specified
// token. The owner-death scenario reuses a token after the proxied side's
// teardown deletes the original row (OnSessionEnd fires on B's splice teardown),
// so reclaim must re-create the row while the stale Redis affinity claim lives on.
func (h *harness) seedSessionToken(ctx context.Context, token string) error {
	u := &auth.User{ID: uuid.New(), Email: "e2e-" + short() + "@example.com", PasswordHash: "hash", DisplayName: "E2E"}
	if err := h.users.Upsert(ctx, u); err != nil {
		return fmt.Errorf("seed user: %w", err)
	}
	g := &device.Group{ID: uuid.New(), Name: "g-" + short(), OwnerID: u.ID}
	if err := h.groups.Create(ctx, g); err != nil {
		return fmt.Errorf("seed group: %w", err)
	}
	d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "h-" + short(), OS: "linux", Status: device.StatusOffline}
	if err := h.devices.Upsert(ctx, d); err != nil {
		return fmt.Errorf("seed device: %w", err)
	}
	if err := h.sessions.Create(ctx, &session.Session{Token: token, DeviceID: d.ID, UserID: u.ID}); err != nil {
		return fmt.Errorf("seed session: %w", err)
	}
	return nil
}

// ensureSessionRow re-creates the session row for token if it is missing (the
// proxied side's teardown on the non-owner replica deletes it). Idempotent: a
// present row is left untouched.
func (h *harness) ensureSessionRow(ctx context.Context, token string) error {
	if _, err := h.sessions.Get(ctx, token); err == nil {
		return nil
	}
	return h.seedSessionToken(ctx, token)
}

// short returns a short random suffix for unique seed identifiers.
func short() string { return uuid.New().String()[:8] }

// relayURL builds the relay WebSocket URL for a side. The token in the URL is the
// agent-side auth; the browser side additionally needs a non-empty ?auth= (the
// relay does not validate it — token-in-URL is the real auth).
func relayURL(baseURL, token, side string) string {
	wsBase := strings.Replace(baseURL, "http", "ws", 1)
	url := wsBase + "/ws/relay/" + token + "?side=" + side
	if side == "browser" {
		url += "&auth=e2e"
	}
	return url
}

// dialRelay opens a relay WebSocket to baseURL for the given side.
func (h *harness) dialRelay(ctx context.Context, baseURL, token, side string) (*websocket.Conn, error) {
	c, _, err := websocket.Dial(ctx, relayURL(baseURL, token, side), nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s %s: %w", side, baseURL, err)
	}
	return c, nil
}

// dialPair opens the agent side at agentURL and the browser side at browserURL
// for token, returning both conns and a closePair func that closes them. On a
// dial error it closes whatever it already opened and returns the error. This
// collapses the repeated dial-agent/dial-browser/defer-close boilerplate the
// scenarios would otherwise duplicate.
func (h *harness) dialPair(ctx context.Context, agentURL, browserURL, token string) (agent, browser *websocket.Conn, closePair func(), err error) {
	agent, err = h.dialRelay(ctx, agentURL, token, "agent")
	if err != nil {
		return nil, nil, nil, err
	}
	browser, err = h.dialRelay(ctx, browserURL, token, "browser")
	if err != nil {
		_ = agent.Close(websocket.StatusGoingAway, "")
		return nil, nil, nil, err
	}
	closePair = func() {
		_ = agent.Close(websocket.StatusNormalClosure, "")
		_ = browser.Close(websocket.StatusNormalClosure, "")
	}
	return agent, browser, closePair, nil
}

// exchange writes payload on src and asserts the same bytes arrive on dst within
// exchangeTimeout — the fundamental relay liveness check.
func exchange(ctx context.Context, src, dst *websocket.Conn, payload []byte) error {
	wctx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()
	if err := src.Write(wctx, websocket.MessageBinary, payload); err != nil {
		return fmt.Errorf("write %q: %w", payload, err)
	}
	_, got, err := dst.Read(wctx)
	if err != nil {
		return fmt.Errorf("read %q: %w", payload, err)
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("payload mismatch: got %q want %q", got, payload)
	}
	return nil
}

// registryUp scrapes baseURL/metrics and reports the opengate_registry_up gauge
// value (1 = reachable, 0 = down). It drives the degraded-mode assertions.
func (h *harness) registryUp(ctx context.Context, baseURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/metrics", nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if value, ok := strings.CutPrefix(line, "opengate_registry_up "); ok {
			return strings.TrimSpace(value) == "1", nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, fmt.Errorf("opengate_registry_up not found in %s/metrics", baseURL)
}

// waitRegistry polls until the registry_up gauge at baseURL equals want, or the
// timeout elapses.
func (h *harness) waitRegistry(ctx context.Context, baseURL string, want bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if up, err := h.registryUp(ctx, baseURL); err == nil && up == want {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("registry_up did not reach %v at %s within %s", want, baseURL, timeout)
}

// composeStop / composeStart drive individual services so the scenarios can kill
// and revive the owner replica and Redis.
func (h *harness) composeStop(svc string) error  { return h.compose("stop", svc) }
func (h *harness) composeStart(svc string) error { return h.compose("start", svc) }

// composeKill SIGKILLs a service so it cannot run graceful shutdown. The
// owner-death scenario needs this: a graceful stop would let the owner release
// its Redis affinity claim, defeating the TTL-reclaim path under test.
func (h *harness) composeKill(svc string) error { return h.compose("kill", svc) }

func (h *harness) compose(action, svc string) error {
	h.logf("compose %s %s", action, svc)
	// #nosec G204 — action/svc are fixed in-process constants ("stop"/"start" and
	// the compose service names), never external input; this is an internal e2e
	// harness driving its own docker-compose stack.
	cmd := exec.Command("docker", "compose", "-f", h.composeFile, "-p", h.composeProject, action, svc)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose %s %s: %w: %s", action, svc, err, strings.TrimSpace(string(out)))
	}
	return nil
}
