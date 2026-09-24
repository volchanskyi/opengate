package inventory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeInventoryText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", sanitizeInventoryText(""))
	assert.Equal(t, "", sanitizeInventoryText("   "))
	assert.Equal(t, "redis:7", sanitizeInventoryText("  redis:7  "), "surrounding whitespace is trimmed")
	assert.Equal(t, redactedField, sanitizeInventoryText("redis:7\npassword=x"), "a newline redacts the value")
	assert.Equal(t, redactedField, sanitizeInventoryText("tab\there"), "a tab redacts the value")

	// A space is 0x20, the first character that is not a control character, and
	// it is ordinary inside a service or image name. Redacting it would empty
	// most of the inventory the moment a name carried one.
	assert.Equal(t, "SQL Server (MSSQLSERVER)",
		sanitizeInventoryText("SQL Server (MSSQLSERVER)"),
		"a space inside a name is not a control character")

	// A field longer than the cap is truncated to exactly maxInventoryFieldLen.
	long := strings.Repeat("a", maxInventoryFieldLen+50)
	got := sanitizeInventoryText(long)
	assert.Len(t, got, maxInventoryFieldLen)

	// A field exactly at the cap is preserved.
	exact := strings.Repeat("b", maxInventoryFieldLen)
	assert.Equal(t, exact, sanitizeInventoryText(exact))
}

func TestClampPort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uint16(0), clampPort(-1))
	assert.Equal(t, uint16(0), clampPort(65536))
	assert.Equal(t, uint16(0), clampPort(0))
	assert.Equal(t, uint16(65535), clampPort(65535))
	assert.Equal(t, uint16(5432), clampPort(5432))
}

func TestValidKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{KindPort, KindService, KindDBEngine, KindContainer, KindPackage} {
		assert.True(t, validKind(kind), kind)
	}
	assert.False(t, validKind(""))
	assert.False(t, validKind("wat"))
}
