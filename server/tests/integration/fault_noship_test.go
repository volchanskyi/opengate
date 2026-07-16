package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFaulttestNotShipped is the binding "no fault code in the shipped binary"
// guarantee. `go list -deps` reports the real (non-test) build dependency graph
// of the production binary; the faulttest package is imported only from _test.go
// files, so it must never appear. This is stronger than an architecture-lint
// rule: it inspects the actual transitive dependencies the linker would include.
func TestFaulttestNotShipped(t *testing.T) {
	t.Parallel()
	const (
		binaryPkg    = "github.com/volchanskyi/opengate/server/cmd/meshserver"
		faulttestPkg = "github.com/volchanskyi/opengate/server/internal/faulttest"
	)

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", binaryPkg).CombinedOutput()
	require.NoErrorf(t, err, "go list -deps failed: %s", out)

	for _, dep := range strings.Fields(string(out)) {
		require.NotEqualf(t, faulttestPkg, dep,
			"the shipped binary %s must not depend on the fault-injection package %s", binaryPkg, faulttestPkg)
	}
}
