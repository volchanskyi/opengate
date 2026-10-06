package testpg_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

func TestBaseURL_IsConnectable(t *testing.T) {
	url := testpg.BaseURL(t)
	require.NotEmpty(t, url)

	d, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, d.Ping())
}

func TestURL_MemoizesResult(t *testing.T) {
	first := testpg.BaseURL(t)
	second := testpg.BaseURL(t)
	require.Equal(t, first, second)
}
