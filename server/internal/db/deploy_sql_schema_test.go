package db

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The schema the migrations land in, as everywhere else in this package.
const deploySQLSchema = "opengate_test"

// The directories whose files embed SQL written against the application
// schema: the chart's hooks, the statement files they read, and the deploy
// workflows. Every regular file in them is read, so SQL that moves into a
// helper template or out into a file two callers share is still covered.
var deploySQLSources = []string{
	filepath.Join("..", "..", "..", "deploy", "helm", "opengate", "templates"),
	filepath.Join("..", "..", "..", "deploy", "helm", "opengate", "files"),
	filepath.Join("..", "..", "..", ".github", "workflows"),
}

// embeddedInsert is one INSERT found in a deployment artifact: the table it
// writes and the columns it names.
type embeddedInsert struct {
	source  string
	table   string
	columns []string
}

// An INSERT that names its columns. No column list in these artifacts contains
// a nested parenthesis, so everything up to the first ")" is the list.
var embeddedInsertRE = regexp.MustCompile(`(?is)INSERT\s+INTO\s+([a-z_][a-z0-9_]*)\s*\(([^)]*)\)`)

// An INSERT that names none, which takes on a new meaning the moment a column
// is added ahead of the ones it supplies.
var positionalInsertRE = regexp.MustCompile(`(?is)INSERT\s+INTO\s+([a-z_][a-z0-9_]*)\s+(?:VALUES|SELECT)\b`)

// TestDeploymentSQLNamesEveryRequiredColumn holds the SQL embedded in the Helm
// chart and the deploy workflows against the schema the migrations build.
//
// Nothing compiles this SQL and nothing types it. It runs for the first time
// inside a post-upgrade hook against a live cluster, so a column the schema
// requires and the statement omits is a failed deployment rather than a failed
// build — and the migration that adds the column breaks the statement from a
// distance, with nothing in its own diff to show for it.
func TestDeploymentSQLNamesEveryRequiredColumn(t *testing.T) {
	store := newPostgresTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inserts := collectEmbeddedInserts(t)
	require.NotEmpty(t, inserts, "no deployment SQL was found; the sources above have moved")

	for _, ins := range inserts {
		t.Run(ins.source+"/"+ins.table, func(t *testing.T) {
			var exists bool
			require.NoError(t, store.DB().QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.tables
					WHERE table_schema = $1 AND table_name = $2
				)`, deploySQLSchema, ins.table).Scan(&exists))
			require.True(t, exists, "writes a table the migrations do not create")

			named := make(map[string]bool, len(ins.columns))
			for _, column := range ins.columns {
				named[column] = true
			}

			for _, column := range requiredColumns(ctx, t, store, ins.table) {
				assert.Truef(t, named[column],
					"%s omits %s.%s, which the schema requires and defaults nowhere — "+
						"the statement fails on deploy with a not-null violation",
					ins.source, ins.table, column)
			}
		})
	}
}

// requiredColumns returns the columns of a table that a caller must supply: not
// nullable, and filled in by nothing. A default, a generated expression and an
// identity sequence each supply a value on their own.
func requiredColumns(ctx context.Context, t *testing.T, store *PostgresStore, table string) []string {
	t.Helper()

	rows, err := store.DB().QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		  AND is_nullable = 'NO'
		  AND column_default IS NULL
		  AND is_generated = 'NEVER'
		  AND is_identity = 'NO'
		ORDER BY column_name`, deploySQLSchema, table)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		columns = append(columns, column)
	}
	require.NoError(t, rows.Err())
	return columns
}

// collectEmbeddedInserts reads every deployment artifact and returns the
// column-listed INSERTs it embeds, failing on any that names no columns.
//
// Each directory is read through its own file system root, so a name can only
// ever resolve inside the directory being scanned.
func collectEmbeddedInserts(t *testing.T) []embeddedInsert {
	t.Helper()

	var found []embeddedInsert
	for _, dir := range deploySQLSources {
		root := os.DirFS(dir)
		entries, err := fs.ReadDir(root, ".")
		require.NoErrorf(t, err, "read %s", dir)

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			body, err := fs.ReadFile(root, entry.Name())
			require.NoErrorf(t, err, "read %s/%s", dir, entry.Name())

			source := path.Join(filepath.Base(dir), entry.Name())

			for _, match := range positionalInsertRE.FindAllStringSubmatch(string(body), -1) {
				t.Errorf("%s writes %s without naming its columns; a positional "+
					"INSERT changes meaning when a column is added", source, match[1])
			}
			for _, match := range embeddedInsertRE.FindAllStringSubmatch(string(body), -1) {
				found = append(found, embeddedInsert{
					source:  source,
					table:   strings.ToLower(match[1]),
					columns: splitColumnList(match[2]),
				})
			}
		}
	}
	return found
}

// splitColumnList turns "group_id, user_id" into its column names.
func splitColumnList(list string) []string {
	parts := strings.Split(list, ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.ToLower(strings.TrimSpace(part)); name != "" {
			columns = append(columns, name)
		}
	}
	return columns
}
