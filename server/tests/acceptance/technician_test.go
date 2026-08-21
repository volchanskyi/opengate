package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// Technician is one authenticated operator inside one customer. Everything a
// technician can do, they do through the HTTP API — the same requests the
// browser issues, with the same token.
type Technician struct {
	t       *testing.T
	product *Product

	// User is who they are.
	User *auth.User
	// Customer is the customer they are looking at.
	Customer uuid.UUID
	// token is the bearer credential every request carries.
	token string
	// admin records whether they hold elevated permission.
	admin bool
}

// Technician signs a technician in against a customer. Asking for a second
// technician in a second customer is how a tenancy outcome is stated.
func (p *Product) Technician(customer uuid.UUID) *Technician {
	p.t.Helper()
	return p.technician(customer, false)
}

// Administrator signs in somebody who also holds elevated permission —
// minting enrolment tokens, publishing builds, pulling logs, erasing machines.
func (p *Product) Administrator(customer uuid.UUID) *Technician {
	p.t.Helper()
	return p.technician(customer, true)
}

func (p *Product) technician(customer uuid.UUID, admin bool) *Technician {
	p.t.Helper()

	user := testutil.SeedUser(p.t, arrangeTenantContext(), p.assembly.Store)
	token, err := p.assembly.JWT.GenerateToken(user.ID, user.Email, admin)
	require.NoError(p.t, err)

	return &Technician{t: p.t, product: p, User: user, Customer: customer, token: token, admin: admin}
}

// Reply is what came back through the door: the status a technician's browser
// would show, and the body it would render.
type Reply struct {
	t      *testing.T
	Status int
	Body   []byte
}

// Into decodes the reply body into v, failing the test if it will not decode.
func (r Reply) Into(v any) Reply {
	r.t.Helper()
	require.NoErrorf(r.t, json.Unmarshal(r.Body, v), "reply body was %s", r.Body)
	return r
}

// Text is the reply body as a technician would read it in a message.
func (r Reply) Text() string { return string(r.Body) }

// Get asks for something. path is the API path, already including any query.
func (a *Technician) Get(path string) Reply { return a.do(http.MethodGet, path, nil) }

// Post creates something.
func (a *Technician) Post(path string, body any) Reply { return a.do(http.MethodPost, path, body) }

// Patch changes something.
func (a *Technician) Patch(path string, body any) Reply { return a.do(http.MethodPatch, path, body) }

// Put replaces something.
func (a *Technician) Put(path string, body any) Reply { return a.do(http.MethodPut, path, body) }

// Delete removes something.
func (a *Technician) Delete(path string) Reply { return a.do(http.MethodDelete, path, nil) }

// InCustomer returns the path with the customer filter a technician's browser
// carries, so a test never has to remember the query parameter's spelling.
func (a *Technician) InCustomer(path string) string {
	sep := "?"
	if bytes.ContainsRune([]byte(path), '?') {
		sep = "&"
	}
	return fmt.Sprintf("%s%sorganization_id=%s", path, sep, a.Customer)
}

func (a *Technician) do(method, path string, body any) Reply {
	a.t.Helper()

	var buf bytes.Buffer
	if body != nil {
		require.NoError(a.t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.product.HTTP.URL+path, &buf)
	require.NoError(a.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.token)

	resp, err := a.product.HTTP.Client().Do(req)
	require.NoError(a.t, err)
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	require.NoError(a.t, err)

	return Reply{t: a.t, Status: resp.StatusCode, Body: payload}
}
