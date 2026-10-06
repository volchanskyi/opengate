// Package testvm supplies a VictoriaMetrics base URL to tests, from VICTORIAMETRICS_TEST_URL
// or a throwaway container. It imports only internal/testreaper, so any test package can use it.
package testvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/volchanskyi/opengate/server/internal/testreaper"
)

// init settles the reaper at load, because another package may start the process's first container.
func init() {
	testreaper.Settle()
}

// URLEnv names the environment variable that, when set, supplies an external
// VictoriaMetrics base URL and bypasses container auto-provisioning.
const URLEnv = "VICTORIAMETRICS_TEST_URL"

// image pins the VictoriaMetrics tag the monitoring stack deploys.
const image = "victoriametrics/victoria-metrics:v1.114.0"

// httpPort is VictoriaMetrics' default single-node HTTP listen port.
const httpPort = "8428/tcp"

var (
	once     sync.Once
	baseURL  string
	setupErr error
)

// URL returns the base VictoriaMetrics URL, memoized so one instance backs the test binary.
// It provisions a throwaway container on first use when URLEnv is unset and suits TestMain.
func URL() (string, error) {
	once.Do(func() { baseURL, setupErr = resolveBaseURL(os.Getenv, startContainer) })
	return baseURL, setupErr
}

// BaseURL returns the base VictoriaMetrics URL from URL and fails the test on a provisioning error.
func BaseURL(t testing.TB) string {
	t.Helper()
	url, err := URL()
	if err != nil {
		t.Fatalf("testvm: provision VictoriaMetrics (set %s for an external VM): %v", URLEnv, err)
	}
	return url
}

// Dedicated provisions a VictoriaMetrics reserved for the calling test and removes it at cleanup.
// URLEnv is ignored so process-level measurements never include another test's series.
func Dedicated(t testing.TB, extraArgs ...string) string {
	t.Helper()
	url, terminate, err := startDedicated(extraArgs)
	if err != nil {
		t.Fatalf("testvm: provision a dedicated VictoriaMetrics: %v", err)
	}
	t.Cleanup(terminate)
	return url
}

// startDedicated launches a VictoriaMetrics container and returns its base URL and terminator.
func startDedicated(extraArgs []string) (string, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts(httpPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/health").
				WithPort(httpPort).
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }),
		),
	}
	if len(extraArgs) > 0 {
		opts = append(opts, testcontainers.WithCmd(extraArgs...))
	}

	c, err := testcontainers.Run(ctx, image, opts...)
	if err != nil {
		return "", nil, fmt.Errorf("start VictoriaMetrics container: %w", err)
	}
	terminate := func() {
		_ = testcontainers.TerminateContainer(c)
	}

	endpoint, err := c.Endpoint(ctx, "http")
	if err != nil {
		terminate()
		return "", nil, fmt.Errorf("resolve VictoriaMetrics endpoint: %w", err)
	}
	return endpoint, terminate, nil
}

// resolveBaseURL returns the external URL when URLEnv is set, otherwise the URL start provisions.
func resolveBaseURL(getenv func(string) string, start func() (string, error)) (string, error) {
	if url := getenv(URLEnv); url != "" {
		return url, nil
	}
	return start()
}

// startContainer launches a throwaway VictoriaMetrics container and returns its
// base URL once the /health endpoint reports ready.
func startContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The Ryuk reaper removes the container when the test process exits.
	c, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts(httpPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/health").
				WithPort(httpPort).
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }),
		),
	)
	if err != nil {
		return "", fmt.Errorf("start VictoriaMetrics container: %w", err)
	}

	endpoint, err := c.Endpoint(ctx, "http")
	if err != nil {
		return "", fmt.Errorf("resolve VictoriaMetrics endpoint: %w", err)
	}
	return endpoint, nil
}
