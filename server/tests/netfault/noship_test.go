package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
