package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// arrangeSeparateTenant creates a second installation-worth of isolation
// inside the same database. A tenant is what the product actually permits by,
// and there is no operator door that creates one.
func (p *Product) arrangeSeparateTenant(name string) uuid.UUID {
	p.t.Helper()
	tenantID := uuid.New()
	testutil.EnsureTenant(p.t, arrangeTenantContext(), p.assembly.Store, tenantID, name)
	return tenantID
}

// TechnicianIn signs a technician in inside another tenant, which is the only
// boundary the product refuses across.
func (p *Product) TechnicianIn(tenantID uuid.UUID) *Technician {
	p.t.Helper()

	ctx := dbtx.WithTenant(arrangeTenantContext(), tenantID, false)
	user := testutil.SeedUser(p.t, ctx, p.assembly.Store)

	token, err := p.assembly.JWT.GenerateToken(user.ID, user.Email, true, tenantID)
	require.NoError(p.t, err)
	return &Technician{t: p.t, product: p, User: user, token: token, admin: true}
}

// TestOneTenantsEstateIsInvisibleToAnother is the sentence Tenancy and Access
// promises, stated once across the whole surface rather than route by route. A
// guessed identifier must answer the way a missing one does: a different
// status code for "exists but is not yours" tells the caller the row is there.
func TestOneTenantsEstateIsInvisibleToAnother(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-01")
	machine.AwaitOnline()

	outsider := product.TechnicianIn(product.arrangeSeparateTenant("Northwind"))

	assert.Empty(t, outsider.devices(), "another tenant's fleet page is empty, not somebody else's")

	device := "/api/v1/devices/" + machine.DeviceID.String()
	guessed := "/api/v1/devices/" + uuid.NewString()
	for _, path := range []string{device, device + "/hardware", device + "/inventory", device + "/incidents"} {
		assert.Equalf(t, outsider.Get(guessed).Status, outsider.Get(path).Status,
			"%s must answer a guessed id exactly as it answers a missing one", path)
	}

	assert.Equal(t, http.StatusNotFound,
		outsider.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()}).Status,
		"a live connection is the one thing that must never cross a tenant")
}

// TestTheLastAdministratorCannotBeDemoted keeps an installation from locking
// itself out. Take administration away from the last person who holds it and
// nobody is left who can mint an enrolment token, publish a build, or put
// anybody back — including themselves.
func TestTheLastAdministratorCannotBeDemoted(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	admin := product.Administrator(product.arrangeCustomer("Contoso"))

	first := admin.registerAdministrator("first.admin@contoso.example")
	second := admin.registerAdministrator("second.admin@contoso.example")

	assert.Equal(t, http.StatusOK, admin.demote(first).Status,
		"an installation with two administrators may lose one")
	assert.Equal(t, http.StatusForbidden, admin.demote(second).Status,
		"taking administration from the last person who holds it must be refused")
}

// registerAdministrator creates an operator and gives them administration,
// which is what somebody setting an installation up does first.
func (a *Technician) registerAdministrator(email string) uuid.UUID {
	a.t.Helper()

	var created struct {
		ID uuid.UUID `json:"id"`
	}
	reply := a.Post("/api/v1/auth/register", map[string]any{
		"email": email, "password": "a-password-nobody-guesses", "display_name": email,
	})
	require.Containsf(a.t, []int{http.StatusOK, http.StatusCreated}, reply.Status,
		"registering an operator failed: %s", reply.Text())

	var users []struct {
		ID      uuid.UUID `json:"id"`
		Email   string    `json:"email"`
		IsAdmin bool      `json:"is_admin"`
	}
	a.Get("/api/v1/users").Into(&users)
	for _, user := range users {
		if user.Email == email {
			created.ID = user.ID
		}
	}
	require.NotEqual(a.t, uuid.Nil, created.ID, "the operator just registered is in the operator list")

	require.Equal(a.t, http.StatusOK,
		a.Patch("/api/v1/users/"+created.ID.String(), map[string]any{"is_admin": true}).Status)
	return created.ID
}

// demote takes administration away from an operator.
func (a *Technician) demote(user uuid.UUID) Reply {
	a.t.Helper()
	return a.Patch("/api/v1/users/"+user.String(), map[string]any{"is_admin": false})
}
