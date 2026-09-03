package dbtx

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The database layer already has a completeness gate in both directions:
// TestTenantIsolationCoversEveryTenantTable probes every table carrying a
// tenant_id, and TestEveryTenantTableIsProbed reads the live schema and fails
// when a table appears without a probe. So a table cannot ship unproven.
//
// A repository could. Every package below already proves its scoped queries
// refuse another customer's rows, and no two of those proofs are named alike —
// which is why nothing was checking that the set was complete. A new repository
// package shipping without one would have failed nothing at all.
//
// This is that gate, at the layer above the schema. A repository is where a
// query's tenant clause is written, so it is where a missing one has to be
// caught: the row-level policy underneath is the second wall, not the first.

// The test in each package that proves its scoped queries refuse another
// customer's rows. Hand-written like the probe list it is modelled on, and for
// the same reason — the proofs are prose, not a pattern, and a gate that
// recognised them by name would be asking every author to name a test after
// the gate rather than after what it proves.
//
// Held against the tree in both directions below, so it cannot drift: a
// package that starts issuing scoped SQL without an entry fails, an entry
// naming a test that is not there fails, and an entry for a package that has
// stopped issuing scoped SQL fails.
var tenantDenyProofs = map[string][]string{
	"alerts":        {"TestCrossTenantReadIsDeniedByACraftedKey"},
	"amt":           {"TestPostgresAMTDevices_TenantDeny"},
	"audit":         {"TestPostgresAudit_TenantDeny"},
	"auth":          {"TestPostgresUsers_TenantDeny", "TestPostgresSecurityGroups_TenantDeny"},
	"device":        {"TestPostgresDeviceRepos_TenantDeny"},
	"inventory":     {"TestPostgresInventoryRepositoryTenantDeny"},
	"lifecycle":     {"TestOrchestratorPurgeTenantLeavesOtherTenantsUntouched"},
	"notifications": {"TestPostgresWebPush_TenantDeny"},
	"organization":  {"TestOrganizationBelongsToExactlyOneTenant"},
	"rules":         {"TestStoreDeniesCrossTenantAccess", "TestTagsDenyCrossTenantAccess"},
	"session":       {"TestPostgresSessions_TenantDeny"},
	"settings":      {"TestScopeForAnotherTenantsDeviceIsNotFound"},
	"telemetry":     {"TestPostgresProcessRepositoryTenantDeny"},
	"updater":       {"TestPostgresDeviceUpdates_TenantDeny", "TestPostgresEnrollment_TenantDeny"},
}

// TestEveryScopedRepositoryProvesItRefusesAnotherCustomer fails when a package
// opens a tenant-scoped transaction and nothing beside it is named as the proof
// that the scope holds.
func TestEveryScopedRepositoryProvesItRefusesAnotherCustomer(t *testing.T) {
	t.Parallel()

	scoped, err := packagesIssuingScopedSQL()
	require.NoError(t, err)
	require.NotEmpty(t, scoped,
		"no package was found issuing tenant-scoped SQL — the walk has drifted, not the tree")

	claimed := make([]string, 0, len(tenantDenyProofs))
	for pkg := range tenantDenyProofs {
		claimed = append(claimed, pkg)
	}
	sort.Strings(claimed)
	sort.Strings(scoped)

	assert.ElementsMatch(t, scoped, claimed,
		"every package opening a tenant-scoped transaction names the test that proves the scope "+
			"refuses another customer's rows, and only such a package appears here. For a new one, "+
			"write the test — internal/notifications/webpush_test.go's TestPostgresWebPush_TenantDeny "+
			"is the shape: seed a row under a second tenant, read and write it through the first "+
			"tenant's context, and require the row to be invisible and the write refused — then add "+
			"it to tenantDenyProofs")
}

// TestEveryNamedTenantDenyProofExists is the other half. A proof named here and
// deleted, renamed or moved would leave the gate above satisfied by a name and
// nothing else.
func TestEveryNamedTenantDenyProofExists(t *testing.T) {
	t.Parallel()

	for pkg, proofs := range tenantDenyProofs {
		names, err := testFunctionNames(filepath.Join("..", pkg))
		require.NoError(t, err, "reading the tests of internal/%s", pkg)
		for _, proof := range proofs {
			assert.Contains(t, names, proof,
				"internal/%s names %s as its proof that a scoped query refuses another customer, "+
					"and no such test is declared there", pkg, proof)
		}
	}
}

// packagesIssuingScopedSQL lists, by name under internal/, every package whose
// production code opens a tenant-scoped transaction.
func packagesIssuingScopedSQL() ([]string, error) {
	found := map[string]struct{}{}
	err := filepath.WalkDir(filepath.Clean(".."), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return skipTestHarnessDirs(d)
		}
		src, ok, err := productionSource(path)
		if err != nil || !ok {
			return err
		}
		if strings.Contains(src, "dbtx.Scoped(") {
			found[strings.TrimPrefix(filepath.ToSlash(filepath.Dir(path)), "../")] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	pkgs := make([]string, 0, len(found))
	for pkg := range found {
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

// testFunctionNames lists the test functions declared directly in a package
// directory. A sub-package carries its own production code and is reached by
// the walk in its own right.
func testFunctionNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for line := range strings.SplitSeq(string(src), "\n") {
			const decl = "func Test"
			if !strings.HasPrefix(line, decl) {
				continue
			}
			name := line[len("func "):]
			if idx := strings.IndexAny(name, "("); idx >= 0 {
				name = name[:idx]
			}
			names = append(names, name)
		}
	}
	return names, nil
}
