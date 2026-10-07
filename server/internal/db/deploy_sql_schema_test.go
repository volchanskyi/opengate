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

const deploySQLSchema = "opengate_test"

// Every regular file in these directories is read, so SQL moved into a helper template or a
// shared file stays covered.
var deploySQLSources = []string{
	filepath.Join("..", "..", "..", "deploy", "helm", "opengate", "templates"),
	filepath.Join("..", "..", "..", "deploy", "helm", "opengate", "files"),
	filepath.Join("..", "..", "..", ".github", "workflows"),
}

type embeddedInsert struct {
	source  string
	table   string
	columns []string
}

// No column list in these artifacts nests parentheses, so the first ")" ends the list.
var embeddedInsertRE = regexp.MustCompile(`(?is)INSERT\s+INTO\s+([a-z_][a-z0-9_]*)\s*\(([^)]*)\)`)

// An INSERT without a column list changes meaning when a column is added ahead of its values.
var positionalInsertRE = regexp.MustCompile(`(?is)INSERT\s+INTO\s+([a-z_][a-z0-9_]*)\s+(?:VALUES|SELECT)\b`)

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

// A default, a generated expression and an identity sequence each supply a value on their own.
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

// Each directory is read through its own file system root, so a name resolves only inside it.
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
