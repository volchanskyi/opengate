package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

func TestTheQueueAndTheRoomServeNamesBesideIds(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, nil)
	host, err := e.srv.devices.Get(e.ctx, e.device)
	require.NoError(t, err)

	taken := doRequest(e.srv, http.MethodPost, "/api/v1/investigations/"+incident.String()+"/assignee",
		e.token, map[string]any{"assignee_id": e.user.ID})
	require.Equal(t, http.StatusOK, taken.Code, taken.Body.String())

	w := doRequest(e.srv, http.MethodGet, "/api/v1/investigations", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page IncidentPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Items[0].ScopeName)
	assert.Equal(t, host.Hostname, *page.Items[0].ScopeName)

	w = doRequest(e.srv, http.MethodGet, "/api/v1/investigations/"+incident.String(), e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var room IncidentDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &room))
	require.Len(t, room.Alerts, 1)
	require.NotNil(t, room.Alerts[0].Hostname)
	assert.Equal(t, host.Hostname, *room.Alerts[0].Hostname)
	assert.Equal(t, map[string]string{e.user.ID.String(): e.user.DisplayName}, room.People)
	assert.Contains(t, w.Body.String(), `"people":{`)
}

func TestNamesOfRemovedRecordsAreServedAsNull(t *testing.T) {
	t.Parallel()
	assert.Nil(t, namedOrNull(""), "a removed record is null, which the console reads as removed")
	name := namedOrNull("reception-pc")
	require.NotNil(t, name)
	assert.Equal(t, "reception-pc", *name)
}

func TestTheDeviceListAsksForNoSiteOrRefusesBoth(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})

	w := doRequest(e.srv, http.MethodGet, "/api/v1/devices?without_site=true", e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed []Device
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	assert.Empty(t, listed, "the one seeded device is filed under a site")

	w = doRequest(e.srv, http.MethodGet, "/api/v1/devices?without_site=true&site_id="+e.site.String(), e.token, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
