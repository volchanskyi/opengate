package organization_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/organization"
)

// A customer name is a label in a picker, and the bound on it is the only thing
// standing between that picker and a field somebody pasted a document into.
//
// The cases that matter are the two either side of the limit: a name of exactly
// the maximum is a name somebody may legitimately have, and refusing it is a
// customer who cannot be created. The pair is what distinguishes "longer than
// the maximum" from "as long as the maximum".
func TestValidateNameBoundsTheLabelExactly(t *testing.T) {
	t.Parallel()

	atTheLimit := strings.Repeat("a", organization.MaxNameLen)
	require.NoError(t, organization.ValidateName(atTheLimit),
		"a name of exactly the maximum length is allowed")

	assert.ErrorIs(t, organization.ValidateName(atTheLimit+"a"), organization.ErrNameRequired,
		"one character past the maximum is refused")
}

// The empty name is refused as well, which is the other half of the same check.
func TestValidateNameRefusesAnEmptyLabel(t *testing.T) {
	t.Parallel()

	assert.ErrorIs(t, organization.ValidateName(""), organization.ErrNameRequired)
	assert.NoError(t, organization.ValidateName("Contoso"))
}
