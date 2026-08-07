package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// organizationEnv is a server with an admin and a plain member, the two callers
// every organization route distinguishes between.
type organizationEnv struct {
	srv         *Server
	adminToken  string
	memberToken string
}

func newOrganizationEnv(t *testing.T) organizationEnv {
	t.Helper()
	srv, cfg := newTestServer(t)
	_, adminToken := seedTestUser(t, srv, cfg, "org-admin-"+uuid.New().String()[:8]+"@example.com", true)
	_, memberToken := seedTestUser(t, srv, cfg, "org-member-"+uuid.New().String()[:8]+"@example.com", false)
	return organizationEnv{srv: srv, adminToken: adminToken, memberToken: memberToken}
}

// createOrganization posts a new customer as the admin and returns its id.
func createOrganization(t *testing.T, env organizationEnv, name string) uuid.UUID {
	t.Helper()
	w := doRequest(env.srv, http.MethodPost, "/api/v1/organizations", env.adminToken,
		map[string]string{"name": name})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created Organization
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	return created.Id
}

// TestOrganizationListIsAFleetReadAndNeverEmpty covers the picker's source: any
// member of the tenant may read it, and a tenant that has never been given a
// customer is handed one rather than an empty list.
func TestOrganizationListIsAFleetReadAndNeverEmpty(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)

	w := doRequest(env.srv, http.MethodGet, "/api/v1/organizations", env.memberToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed []Organization
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	assert.NotEmpty(t, listed, "the picker must never have nothing to offer")
}

// TestOrganizationMutationsAreAdminGated covers the mutation boundary: taking on
// a customer, renaming one and deleting one all reshape who the fleet is for.
func TestOrganizationMutationsAreAdminGated(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)
	existing := createOrganization(t, env, "Contoso")

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create", http.MethodPost, "/api/v1/organizations", map[string]string{"name": "Sneaky"}},
		{"update", http.MethodPatch, "/api/v1/organizations/" + existing.String(), map[string]string{"name": "Renamed"}},
		{"delete", http.MethodDelete, "/api/v1/organizations/" + existing.String(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(env.srv, tt.method, tt.path, env.memberToken, tt.body)
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
	}
}

// TestOrganizationCreateRenameArchiveAndDelete walks the management lifecycle,
// including the duplicate-name refusal and the archived customer dropping out of
// the working set.
func TestOrganizationCreateRenameArchiveAndDelete(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)

	id := createOrganization(t, env, "Fabrikam")

	t.Run("duplicate name refused", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPost, "/api/v1/organizations", env.adminToken,
			map[string]string{"name": "Fabrikam"})
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})

	t.Run("empty name refused", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPost, "/api/v1/organizations", env.adminToken,
			map[string]string{"name": ""})
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("rename", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPatch, "/api/v1/organizations/"+id.String(), env.adminToken,
			map[string]string{"name": "Fabrikam Ltd"})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var updated Organization
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Fabrikam Ltd", updated.Name)
	})

	t.Run("archive leaves the working set but keeps the row", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPatch, "/api/v1/organizations/"+id.String(), env.adminToken,
			map[string]any{"archived": true})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.NotContains(t, listedOrganizationIDs(t, env, ""), id)
		assert.Contains(t, listedOrganizationIDs(t, env, "?include_archived=true"), id)

		w = doRequest(env.srv, http.MethodGet, "/api/v1/organizations/"+id.String(), env.memberToken, nil)
		assert.Equal(t, http.StatusOK, w.Code, "an archived customer is still readable by id")
	})

	t.Run("delete", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodDelete, "/api/v1/organizations/"+id.String(), env.adminToken, nil)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		assert.NotContains(t, listedOrganizationIDs(t, env, "?include_archived=true"), id)
	})

	t.Run("missing customer is not found", func(t *testing.T) {
		missing := uuid.New().String()
		w := doRequest(env.srv, http.MethodGet, "/api/v1/organizations/"+missing, env.memberToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		w = doRequest(env.srv, http.MethodPatch, "/api/v1/organizations/"+missing, env.adminToken,
			map[string]string{"name": "Nothing"})
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestDeletingTheLastOrganizationIsRefused is the no-orphan floor at the API: a
// tenant must keep somewhere to put a device.
func TestDeletingTheLastOrganizationIsRefused(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)

	only := listedOrganizationIDs(t, env, "?include_archived=true")
	require.Len(t, only, 1, "a fresh tenant has exactly the one it was given")

	w := doRequest(env.srv, http.MethodDelete, "/api/v1/organizations/"+only[0].String(), env.adminToken, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Len(t, listedOrganizationIDs(t, env, "?include_archived=true"), 1)
}

// TestFleetReadsNarrowToTheSelectedCustomer is the behavioural half of the
// filter contract: the device list and the dashboard rollup both answer for the
// selected customer, and for the whole tenant when none is selected.
func TestFleetReadsNarrowToTheSelectedCustomer(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)
	ctx := testTenantContext(t)

	contoso := createOrganization(t, env, "Contoso")
	fabrikam := createOrganization(t, env, "Fabrikam")

	first := seedDeviceInOrganization(t, env, ctx, contoso)
	seedDeviceInOrganization(t, env, ctx, fabrikam)

	t.Run("device list", func(t *testing.T) {
		narrowed := listDeviceIDs(t, env, "?organization_id="+contoso.String())
		require.Len(t, narrowed, 1)
		assert.Equal(t, first, narrowed[0])
		assert.Len(t, listDeviceIDs(t, env, ""), 2, "no customer selected returns the whole tenant")
	})

	t.Run("fleet summary", func(t *testing.T) {
		assert.Equal(t, 1, summaryTotal(t, env, "?organization_id="+fabrikam.String()))
		assert.Equal(t, 2, summaryTotal(t, env, ""))
	})
}

// TestMoveDeviceOrganization covers reassigning a device: admin-gated, refused
// for a customer outside the tenant, and complete when it succeeds.
func TestMoveDeviceOrganization(t *testing.T) {
	t.Parallel()
	env := newOrganizationEnv(t)
	ctx := testTenantContext(t)

	contoso := createOrganization(t, env, "Contoso")
	fabrikam := createOrganization(t, env, "Fabrikam")
	deviceID := seedDeviceInOrganization(t, env, ctx, contoso)
	path := "/api/v1/devices/" + deviceID.String() + "/organization"

	t.Run("member refused", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPut, path, env.memberToken,
			map[string]string{"organization_id": fabrikam.String()})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unknown customer is not found", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPut, path, env.adminToken,
			map[string]string{"organization_id": uuid.New().String()})
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("unknown device is not found", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPut, "/api/v1/devices/"+uuid.New().String()+"/organization",
			env.adminToken, map[string]string{"organization_id": fabrikam.String()})
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("moved", func(t *testing.T) {
		w := doRequest(env.srv, http.MethodPut, path, env.adminToken,
			map[string]string{"organization_id": fabrikam.String()})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var moved Device
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &moved))
		assert.Equal(t, fabrikam, moved.OrganizationId)
		assert.Equal(t, deviceID, moved.Id, "a move keeps the device's identity")

		assert.Equal(t, []uuid.UUID{deviceID}, listDeviceIDs(t, env, "?organization_id="+fabrikam.String()))
		assert.Empty(t, listDeviceIDs(t, env, "?organization_id="+contoso.String()))
	})
}

// seedDeviceInOrganization inserts a device and puts it in the named customer.
func seedDeviceInOrganization(t *testing.T, env organizationEnv, ctx context.Context, organizationID uuid.UUID) uuid.UUID {
	t.Helper()
	d := &device.Device{ID: uuid.New(), Hostname: "host-" + uuid.New().String()[:8], Status: device.StatusOffline}
	require.NoError(t, env.srv.devices.Upsert(ctx, d))
	require.NoError(t, env.srv.devices.UpdateOrganization(ctx, d.ID, organizationID))
	return d.ID
}

func listedOrganizationIDs(t *testing.T, env organizationEnv, query string) []uuid.UUID {
	t.Helper()
	w := doRequest(env.srv, http.MethodGet, "/api/v1/organizations"+query, env.memberToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed []Organization
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	out := make([]uuid.UUID, 0, len(listed))
	for _, o := range listed {
		out = append(out, o.Id)
	}
	return out
}

func listDeviceIDs(t *testing.T, env organizationEnv, query string) []uuid.UUID {
	t.Helper()
	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices"+query, env.memberToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed []Device
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	out := make([]uuid.UUID, 0, len(listed))
	for _, d := range listed {
		out = append(out, d.Id)
	}
	return out
}

func summaryTotal(t *testing.T, env organizationEnv, query string) int {
	t.Helper()
	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/summary"+query, env.memberToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var summary DeviceSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	return summary.Total
}
