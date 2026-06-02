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
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for the ping
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// URLEnv names the environment variable that, when set, supplies an external
// test database and bypasses container auto-provisioning.
const URLEnv = "POSTGRES_TEST_URL"

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

// startContainer launches a throwaway postgres:17-alpine container and returns
// its connection string. max_connections matches the Makefile postgres-test-up
// target so the test suite's concurrency budget holds.
func startContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The container object is not retained: the Docker container keeps running
	// independently of this Go handle and is reaped by the testcontainers Ryuk
	// reaper when the test process exits, so no explicit Terminate is needed.
	c, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("opengate_test"),
		postgres.WithUsername("opengate"),
		postgres.WithPassword("opengate"),
		postgres.BasicWaitStrategies(),
		testcontainers.WithCmd("postgres", "-c", "max_connections=400"),
	)
	if err != nil {
		return "", err
	}

	return c.ConnectionString(ctx, "sslmode=disable")
}
