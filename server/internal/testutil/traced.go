package testutil

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

// StatementCounter counts every statement a traced pool sends, a transaction's begin and commit
// included, so each is one trip to the database.
type StatementCounter struct {
	mu         sync.Mutex
	statements []string
}

// TraceQueryStart records one statement.
func (c *StatementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, strings.Join(strings.Fields(data.SQL), " "))
	return ctx
}

// TraceQueryEnd records nothing; a statement counts once it is sent.
func (c *StatementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Take returns the statements counted since the last call, and starts the count again.
func (c *StatementCounter) Take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.statements
	c.statements = nil
	return out
}

// NewTracedDB opens a one-connection pool on store's schema whose statements counter sees.
func NewTracedDB(t testing.TB, store *db.PostgresStore, counter *StatementCounter) *sql.DB {
	t.Helper()
	ctx := context.Background()
	var schema string
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema))

	config, err := pgx.ParseConfig(testpg.BaseURL(t))
	require.NoError(t, err)
	config.RuntimeParams["search_path"] = schema
	config.Tracer = counter
	traced := stdlib.OpenDB(*config)
	traced.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = traced.Close() })
	require.NoError(t, traced.PingContext(ctx))
	counter.Take()
	return traced
}
