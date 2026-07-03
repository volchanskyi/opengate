// Package testvm supplies a shared VictoriaMetrics base URL to the test suite.
// When VICTORIAMETRICS_TEST_URL is set (CI, or an external VM) it is used
// as-is; otherwise a throwaway victoriametrics/victoria-metrics container is
// started so telemetry integration tests always run deterministically and
// never silently skip.
//
// The image tag is pinned to the same version the monitoring stack deploys
// (deploy/helm/monitoring/values.yaml) so the test harness mirrors production.
// Like testpg, this is a leaf package —
// it imports no internal/* package — so any test package can depend on it
// without risking an import cycle.
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
)

// URLEnv names the environment variable that, when set, supplies an external
// VictoriaMetrics base URL and bypasses container auto-provisioning.
const URLEnv = "VICTORIAMETRICS_TEST_URL"

// image pins the VictoriaMetrics tag deployed by the monitoring stack so the
// test harness matches production. Keep in sync with the deploy manifests.
const image = "victoriametrics/victoria-metrics:v1.114.0"

// httpPort is VictoriaMetrics' default single-node HTTP listen port.
const httpPort = "8428/tcp"

var (
	once     sync.Once
	baseURL  string
	setupErr error
)

// URL returns the base VictoriaMetrics URL (e.g. http://127.0.0.1:32769),
// provisioning a throwaway container on first use when URLEnv is unset. It is
// memoized, so a single VM backs the whole test binary. Intended for TestMain,
// which has no testing.TB; tests should prefer BaseURL.
func URL() (string, error) {
	once.Do(func() { baseURL, setupErr = resolveBaseURL(os.Getenv, startContainer) })
	return baseURL, setupErr
}

// BaseURL returns the base VictoriaMetrics URL (see URL). It never skips: a
// provisioning failure fails the test via t.Fatalf so a missing VM is loud,
// not a silent green.
func BaseURL(t testing.TB) string {
	t.Helper()
	url, err := URL()
	if err != nil {
		t.Fatalf("testvm: provision VictoriaMetrics (set %s for an external VM): %v", URLEnv, err)
	}
	return url
}

// resolveBaseURL uses an external URL from the environment when URLEnv is set,
// otherwise provisions one via start. It is split out from URL so both branches
// are unit-testable without the sync.Once memoization and without Docker.
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

	// The container is not retained: it keeps running independently of this Go
	// handle and is reaped by the testcontainers Ryuk reaper when the test
	// process exits, so no explicit Terminate is needed (matches testpg).
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
