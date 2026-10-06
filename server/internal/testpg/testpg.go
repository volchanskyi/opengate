// Package testpg supplies a shared Postgres connection string to the test suite: the
// POSTGRES_TEST_URL value when set, otherwise a throwaway postgres:17-alpine container.
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

	"github.com/volchanskyi/opengate/server/internal/testreaper"
)

const (
	// URLEnv names the environment variable that, when set, supplies an external
	// test database and bypasses container auto-provisioning.
	URLEnv = "POSTGRES_TEST_URL"
	// PostgresImage is the pinned test database image and client-binary source.
	PostgresImage = "postgres:17-alpine"
)

var (
	once     sync.Once
	baseURL  string
	setupErr error
)

// URL returns the memoized base test-database connection string, provisioning a container
// when URLEnv is unset. TestMain uses it because it has no testing.TB.
func URL() (string, error) {
	once.Do(initBaseURL)
	return baseURL, setupErr
}

// BaseURL returns the base test-database connection string (see URL) and fails the test
// with t.Fatalf when provisioning fails.
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

// init settles the reaper settings before any container exists, including ones started by
// other packages when POSTGRES_TEST_URL is set.
func init() {
	testreaper.Settle()
}

// startContainer launches a throwaway postgres:17-alpine container and returns its
// connection string. The raised lock ceiling keeps concurrent migrations within the lock table.
func startContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The Ryuk reaper removes the container when the test process exits.
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
