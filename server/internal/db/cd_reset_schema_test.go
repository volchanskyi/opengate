package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cdWorkflowPath is the deploy workflow, read from the repo rather than copied,
// so this test cannot drift from what CD actually runs.
const cdWorkflowPath = "../../../.github/workflows/cd.yml"

var (
	truncateStmtRE = regexp.MustCompile(`(?is)TRUNCATE\s+TABLE\s+(.*?)(?:RESTART\s+IDENTITY|CASCADE|;)`)
	insertStmtRE   = regexp.MustCompile(`(?i)INSERT\s+INTO\s+([a-z_][a-z0-9_]*)`)
)

// TestCDResetTargetsTablesThatExist pins the deploy workflow's inline SQL to the
// schema the migrations actually produce.
//
// The staging reset step names its tables by hand, and it runs in exactly one
// place: CD, after a merge to main. Nothing else reads those identifiers — a
// migration that renames a table leaves the workflow naming one that is gone,
// every gate stays green, and the failure arrives as a broken deployment. That
// is how `ALTER TABLE groups_ RENAME TO sites` took staging down one step after
// the same rename had already broken the smoke test.
//
// Reading the live post-migration schema, rather than parsing the migrations for
// renames, means this cannot be fooled by a DDL form the parser did not expect.
func TestCDResetTargetsTablesThatExist(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	workflow, err := os.ReadFile(filepath.Clean(cdWorkflowPath))
	require.NoError(t, err, "the deploy workflow must be readable — a missing file is a failure, not a pass")

	referenced := sqlTablesReferencedIn(string(workflow))
	require.NotEmpty(t, referenced,
		"found no SQL table references in the deploy workflow — the extraction has drifted and this test would pass vacuously")

	inSchema := baseTableNames(t, ctx, s.db)
	require.NotEmpty(t, inSchema, "the schema query found no tables")

	for _, table := range referenced {
		assert.Contains(t, inSchema, table,
			"the deploy workflow's SQL names table %q, which the migrations do not produce — "+
				"a rename left the workflow behind and the next deploy will fail", table)
	}
}

// sqlTablesReferencedIn pulls every table named by the workflow's inline SQL:
// the reset step's TRUNCATE list and the tables it re-seeds.
func sqlTablesReferencedIn(workflow string) []string {
	seen := map[string]struct{}{}
	for _, m := range truncateStmtRE.FindAllStringSubmatch(workflow, -1) {
		for name := range strings.SplitSeq(m[1], ",") {
			if clean := strings.TrimSpace(name); clean != "" {
				seen[clean] = struct{}{}
			}
		}
	}
	for _, m := range insertStmtRE.FindAllStringSubmatch(workflow, -1) {
		seen[strings.TrimSpace(m[1])] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

// baseTableNames lists the tables the migrations leave in the live schema.
func baseTableNames(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT table_name
		  FROM information_schema.tables
		 WHERE table_schema = current_schema()
		   AND table_type = 'BASE TABLE'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		names = append(names, table)
	}
	require.NoError(t, rows.Err())
	return names
}
