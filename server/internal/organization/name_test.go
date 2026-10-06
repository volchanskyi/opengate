package organization_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/organization"
)

func TestValidateNameBoundsTheLabelExactly(t *testing.T) {
	t.Parallel()

	atTheLimit := strings.Repeat("a", organization.MaxNameLen)
	require.NoError(t, organization.ValidateName(atTheLimit),
		"a name of exactly the maximum length is allowed")

	assert.ErrorIs(t, organization.ValidateName(atTheLimit+"a"), organization.ErrNameRequired,
		"one character past the maximum is refused")
}

func TestValidateNameRefusesAnEmptyLabel(t *testing.T) {
	t.Parallel()

	assert.ErrorIs(t, organization.ValidateName(""), organization.ErrNameRequired)
	assert.NoError(t, organization.ValidateName("Contoso"))
}
