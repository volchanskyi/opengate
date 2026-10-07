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

// Technician is one authenticated operator inside one customer, acting through the HTTP API.
type Technician struct {
	t       *testing.T
	product *Product

	User     *auth.User
	Customer uuid.UUID
	token    string
	admin    bool
}

// Technician signs a technician in against a customer.
func (p *Product) Technician(customer uuid.UUID) *Technician {
	p.t.Helper()
	return p.technician(customer, false)
}

// Administrator signs in a technician who also holds elevated permission.
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

// Reply is the status and body an API call returned.
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

// Text returns the reply body as a string.
func (r Reply) Text() string { return string(r.Body) }

// Get issues a GET; path is the API path, already including any query.
func (a *Technician) Get(path string) Reply { return a.do(http.MethodGet, path, nil) }

// Post issues a POST.
func (a *Technician) Post(path string, body any) Reply { return a.do(http.MethodPost, path, body) }

// Patch issues a PATCH.
func (a *Technician) Patch(path string, body any) Reply { return a.do(http.MethodPatch, path, body) }

// Put issues a PUT.
func (a *Technician) Put(path string, body any) Reply { return a.do(http.MethodPut, path, body) }

// Delete issues a DELETE.
func (a *Technician) Delete(path string) Reply { return a.do(http.MethodDelete, path, nil) }

// InCustomer returns the path with the technician's customer filter appended.
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
