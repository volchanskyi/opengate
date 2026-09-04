package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNetfaultNotShipped is the binding "no fault code in the shipped binary"
// guarantee for the link shaper, in the pattern
// server/internal/faulttest/noship_test.go already sets. `go list -deps`
// reports the real (non-test) build dependency graph of the production binary,
// so a shaper package that ever acquired an importer inside the server would
// show up here rather than in a reviewer's attention.
func TestNetfaultNotShipped(t *testing.T) {
	t.Parallel()
	const (
		binaryPkg   = "github.com/volchanskyi/opengate/server/cmd/meshserver"
		netfaultPkg = "github.com/volchanskyi/opengate/server/tests/netfault"
	)

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", binaryPkg).CombinedOutput()
	require.NoErrorf(t, err, "go list -deps failed: %s", out)

	for _, dep := range strings.Fields(string(out)) {
		require.NotEqualf(t, netfaultPkg, dep,
			"the shipped binary %s must not depend on the link shaper %s", binaryPkg, netfaultPkg)
	}
}
