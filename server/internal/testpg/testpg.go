// Package testpg supplies a shared Postgres connection string to the test
// suite. When POSTGRES_TEST_URL is set (CI, or `make postgres-test-up`) it is
// used as-is; otherwise a throwaway postgres:17-alpine container is started so
// integration tests always run deterministically and never silently skip.
//
// This is a leaf package — it imports no internal/* package — so any test
// package (including internal `package foo` tests that cannot import testutil
// without an import cycle) can depend on it.
package testpg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for the ping
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	// URLEnv names the environment variable that, when set, supplies an external
	// test database and bypasses container auto-provisioning.
	URLEnv = "POSTGRES_TEST_URL"
	// PostgresImage is the pinned test database image and client-binary source.
	PostgresImage = "postgres:17-alpine"

	// ryukTimeoutEnv and ryukTimeout are read by the container reaper itself:
	// they set how long it waits for a client to connect before it shuts down
	// and reaps everything it holds. Its own default is 60s, and a workstation
	// running the gauntlet — a Rust build, a Go suite, a browser stack and
	// several databases at once — takes longer than that to reach the reaper.
	// It then tears down the databases the suite is still using. Waiting longer
	// costs nothing when the machine is idle.
	ryukTimeoutEnv = "TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"
	ryukTimeout    = "180s"

	// sessionEnv decides which reaper container a process belongs to. Left
	// alone, testcontainers derives it from the parent process, so every package
	// process under one `go test ./...` shares a single reaper: the first to
	// need one creates it and every other waits on that container to report
	// ready. That wait is a fixed sixty seconds with no setting behind it. On a
	// machine busy enough for the container to be slow — or one where it has
	// already reaped itself and gone — the wait runs out, and the package left
	// holding it fails on a reaper it never started, naming a container nobody
	// wrote. A session of this process's own means it creates its own reaper and
	// waits on nothing anybody else owns.
	sessionEnv = "TESTCONTAINERS_SESSION_ID"
)

var (
	once     sync.Once
	baseURL  string
	setupErr error
)

// URL returns the base test-database connection string, provisioning a
// throwaway container on first use when URLEnv is unset. It is memoized, so a
// single database backs the whole test binary. Intended for TestMain, which has
// no testing.TB; tests should prefer BaseURL.
func URL() (string, error) {
	once.Do(initBaseURL)
	return baseURL, setupErr
}

// BaseURL returns the base test-database connection string (see URL). It never
// skips: a provisioning failure fails the test via t.Fatalf so a missing
// database is loud, not a silent green.
func BaseURL(t testing.TB) string {
	t.Helper()
	url, err := URL()
	if err != nil {
		t.Fatalf("testpg: provision base database: %v", err)
	}
	return url
}

func initBaseURL() {
	if url := os.Getenv(URLEnv); url != "" {
		baseURL = url
	} else {
		url, err := startContainer()
		if err != nil {
			setupErr = fmt.Errorf("auto-provision container (set %s for an external DB): %w", URLEnv, err)
			return
		}
		baseURL = url
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := sql.Open("pgx", baseURL)
	if err != nil {
		setupErr = fmt.Errorf("open base database: %w", err)
		return
	}
	defer func() { _ = d.Close() }()
	d.SetMaxOpenConns(1)
	if err := d.PingContext(ctx); err != nil {
		setupErr = fmt.Errorf("ping base database: %w", err)
	}
}

// init settles both reaper settings before anything can create one. It cannot
// wait for startContainer: when POSTGRES_TEST_URL is set this package provisions
// nothing, and the first container of the process is then started by somebody
// else — the migration rehearsal in internal/db, which needs a database of its
// own — which would take the defaults.
func init() {
	widenReaperWait()
	isolateSession()
}

// widenReaperWait gives the container reaper room to see a slow suite out. An
// operator who has set the variable themselves keeps their value.
func widenReaperWait() {
	if os.Getenv(ryukTimeoutEnv) == "" {
		_ = os.Setenv(ryukTimeoutEnv, ryukTimeout)
	}
}

// isolateSession gives this process a reaper of its own, so it never waits on a
// container another process created and owns. An operator who has set the
// variable themselves keeps their value.
func isolateSession() {
	if os.Getenv(sessionEnv) == "" {
		_ = os.Setenv(sessionEnv, newSessionID())
	}
}

// newSessionID returns a value no other process produces, in the 64-character
// hex shape testcontainers builds the reaper's container name out of.
func newSessionID() string {
	return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
}

// startContainer launches a throwaway postgres:17-alpine container and returns
// its connection string. max_connections matches the Makefile postgres-test-up
// target so the test suite's concurrency budget holds, and the per-transaction
// lock ceiling rises with it: the lock table is sized once at startup as
// max_locks_per_transaction × max_connections, and a migration builds an entire
// schema in one transaction, so enough of them at once exhaust the default 64
// and fail with "out of shared memory" rather than anything about the schema.
func startContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The container object is not retained: the Docker container keeps running
	// independently of this Go handle and is reaped by the testcontainers Ryuk
	// reaper when the test process exits, so no explicit Terminate is needed.
	c, err := postgres.Run(ctx, PostgresImage,
		postgres.WithDatabase("opengate_test"),
		postgres.WithUsername("opengate"),
		postgres.WithPassword("opengate"),
		postgres.BasicWaitStrategies(),
		testcontainers.WithCmd("postgres",
			"-c", "max_connections=400",
			"-c", "max_locks_per_transaction=256",
		),
	)
	if err != nil {
		return "", err
	}

	return c.ConnectionString(ctx, "sslmode=disable")
}
