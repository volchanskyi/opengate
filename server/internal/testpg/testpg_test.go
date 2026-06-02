package testpg_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

// TestBaseURL_IsConnectable asserts BaseURL always yields a live, pingable
// database — whether from POSTGRES_TEST_URL or an auto-provisioned container.
// The test never skips: that is the whole point of the package.
func TestBaseURL_IsConnectable(t *testing.T) {
	url := testpg.BaseURL(t)
	require.NotEmpty(t, url)

	d, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, d.Ping())
}

// TestURL_MemoizesResult asserts repeated calls return the same base URL, so a
// single container backs the whole test binary.
func TestURL_MemoizesResult(t *testing.T) {
	first := testpg.BaseURL(t)
	second := testpg.BaseURL(t)
	require.Equal(t, first, second)
}
