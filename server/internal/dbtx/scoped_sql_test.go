package dbtx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tenantTables = []string{
	"users",
	"groups_",
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
}

func TestTenantTableSQLUsesScopedHelper(t *testing.T) {
	t.Parallel()
	internalRoot := filepath.Clean("..")
	err := filepath.WalkDir(internalRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "testutil" || d.Name() == "testpg" || d.Name() == "testvm" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, "openapi_gen.go") ||
			strings.Contains(path, string(filepath.Separator)+"dbtx"+string(filepath.Separator)) {
			return nil
		}

		srcBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(srcBytes)
		if !containsAny(src, tenantTables) || !containsAny(src, []string{"ExecContext", "QueryContext", "QueryRowContext"}) {
			return nil
		}
		if !strings.Contains(src, "dbtx.Scoped(") {
			t.Errorf("%s issues SQL against tenant tables without dbtx.Scoped", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
