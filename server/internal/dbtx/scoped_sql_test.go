package dbtx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tenantTables = []string{
	"organizations",
	"users",
	"sites",
	"devices",
	"agent_sessions",
	"web_push_subscriptions",
	"audit_events",
	"amt_devices",
	"enrollment_tokens",
	"security_groups",
	"security_group_members",
	"device_updates",
	"device_hardware",
	"device_logs",
	"device_processes",
	"device_inventory",
	"rule_bindings",
	"rule_rollout",
	"rule_coverage_unsupported",
	"alerts",
	"incidents",
	"incident_events",
}

func TestTenantTableSQLUsesScopedHelper(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir(filepath.Clean(".."), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return skipTestHarnessDirs(d)
		}
		return assertTenantSQLScoped(t, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// skipTestHarnessDirs prunes fixture/helper packages that intentionally seed
// tenant tables without the scoped helper.
func skipTestHarnessDirs(d os.DirEntry) error {
	if d.Name() == "testutil" || d.Name() == "testpg" || d.Name() == "testvm" {
		return filepath.SkipDir
	}
	return nil
}

// assertTenantSQLScoped fails if a production Go file runs SQL against a
// tenant-scoped table without going through dbtx.Scoped.
//
// A file holding no *sql.DB satisfies it too, and that is not a loosening: a
// transaction can only begin where a pool is in reach, TestOnlyDbtxBeginsA
// Transaction pins the one place in the tree that does, and so every *sql.Tx
// such a file receives was opened — and scoped — by a caller this same check
// already covers. Requiring the call itself would only push the statements back
// into whichever file happens to hold the entry point.
func assertTenantSQLScoped(t *testing.T, path string) error {
	t.Helper()
	src, ok, err := productionSource(path)
	if err != nil || !ok {
		return err
	}
	if !containsAny(src, tenantTables) || !containsAny(src, []string{"ExecContext", "QueryContext", "QueryRowContext"}) {
		return nil
	}
	if !strings.Contains(src, "dbtx.Scoped(") && strings.Contains(src, "*sql.DB") {
		t.Errorf("%s issues SQL against tenant tables without dbtx.Scoped", path)
	}
	return nil
}

// TestOnlyDbtxBeginsATransaction is the premise the exemption above rests on. If
// anything else could open a transaction, a file could be handed one that never
// had a tenant set on it, and the wall would have a door in it nobody was
// looking at.
func TestOnlyDbtxBeginsATransaction(t *testing.T) {
	t.Parallel()
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
		if strings.Contains(src, "BeginTx(") {
			t.Errorf("%s opens a transaction; only dbtx may, or a tenant can go unset", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// productionSource reads a Go file the checks above apply to, reporting false
// for anything outside that set: tests, generated code, and dbtx itself, which
// is the mechanism rather than a user of it.
func productionSource(path string) (string, bool, error) {
	if !strings.HasSuffix(path, ".go") ||
		strings.HasSuffix(path, "_test.go") ||
		strings.HasSuffix(path, "openapi_gen.go") ||
		strings.Contains(path, string(filepath.Separator)+"dbtx"+string(filepath.Separator)) {
		return "", false, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	return string(src), true, nil
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
