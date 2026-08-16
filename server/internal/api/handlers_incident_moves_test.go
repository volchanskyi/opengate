package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// The moves a technician makes on an incident, driven through HTTP.
//
// Each refusal below is its own answer because each is a different mistake with
// a different fix: a move the lifecycle does not allow, a resolution with no
// answer for why, a code outside the closed set, a person who is not in the
// tenant. Collapsing them into one rejection would leave whoever made the
// mistake guessing which one it was.

// TestStatusMovesAreTypedRefusals. Each of these is a different mistake with a
// different fix, so each is refused on its own terms rather than as one
// undifferentiated rejection — and every accepted move leaves a line behind.
func TestStatusMovesAreTypedRefusals(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, nil)
	path := "/api/v1/investigations/" + incident.String() + "/status"

	resolvedSelf := ResolvedSelf
	for _, tc := range []struct {
		name string
		body SetIncidentStatusRequest
	}{
		{"resolving with no answer for why", SetIncidentStatusRequest{Status: Resolved}},
		{"a cause on a move that is not a resolution",
			SetIncidentStatusRequest{Status: Acknowledged, CauseCode: &resolvedSelf}},
		{"a status outside the set", SetIncidentStatusRequest{Status: "escalated"}},
		{"a cause outside the set", func() SetIncidentStatusRequest {
			invented := IncidentCauseCode("blamed_the_intern")
			return SetIncidentStatusRequest{Status: Resolved, CauseCode: &invented}
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(e.srv, http.MethodPost, path, e.token, tc.body)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}

	w := doRequest(e.srv, http.MethodPost, path, e.token,
		SetIncidentStatusRequest{Status: Acknowledged})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var moved Incident
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &moved))
	assert.Equal(t, Acknowledged, moved.Status)

	// Standing still is not a transition: recording one would put a line in a
	// handover that says nothing happened.
	w = doRequest(e.srv, http.MethodPost, path, e.token,
		SetIncidentStatusRequest{Status: Acknowledged})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = doRequest(e.srv, http.MethodPost, path, e.token,
		SetIncidentStatusRequest{Status: Resolved, CauseCode: &resolvedSelf})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &moved))
	require.NotNil(t, moved.CauseCode)
	assert.Equal(t, ResolvedSelf, *moved.CauseCode)

	w = doRequest(e.srv, http.MethodGet, "/api/v1/investigations/"+incident.String(), e.token, nil)
	var detail IncidentDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Len(t, detail.Events, 2, "every accepted move leaves a line in the room's history")
}

// TestAssignmentNamesSomebodyInTheTenant. The assignee is a person the caller
// can hand work to, so it is resolved through the tenant-scoped user read: a
// name from outside answers the same as one that does not exist.
func TestAssignmentNamesSomebodyInTheTenant(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, nil)
	path := "/api/v1/investigations/" + incident.String() + "/assignee"

	stranger := uuid.New()
	w := doRequest(e.srv, http.MethodPost, path, e.token,
		SetIncidentAssigneeRequest{AssigneeId: &stranger})
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = doRequest(e.srv, http.MethodPost, path, e.token,
		SetIncidentAssigneeRequest{AssigneeId: &e.user.ID})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var taken Incident
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &taken))
	require.NotNil(t, taken.AssigneeId)
	assert.Equal(t, e.user.ID, *taken.AssigneeId)

	// Handing it back is a move a technician going off shift has to be able to
	// make, so it is stated rather than left as an absent field.
	w = doRequest(e.srv, http.MethodPost, path, e.token, SetIncidentAssigneeRequest{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var released Incident
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &released))
	assert.Nil(t, released.AssigneeId)
}

// TestCommentsBecomeLinesAndAreBounded.
func TestCommentsBecomeLinesAndAreBounded(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, _ := e.open(t, alerts.SeverityCritical, nil)
	path := "/api/v1/investigations/" + incident.String() + "/comments"

	w := doRequest(e.srv, http.MethodPost, path, e.token,
		AddIncidentCommentRequest{Body: "array controller replaced, watching overnight"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var event IncidentEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &event))
	assert.Equal(t, IncidentEventKind("comment"), event.Kind)
	require.NotNil(t, event.ActorId)
	assert.Equal(t, e.user.ID, *event.ActorId)
	assert.Equal(t, "array controller replaced, watching overnight", event.Body["body"])

	for _, body := range []string{"", "   "} {
		w = doRequest(e.srv, http.MethodPost, path, e.token, AddIncidentCommentRequest{Body: body})
		assert.Equal(t, http.StatusBadRequest, w.Code, "a comment that says nothing is not a comment")
	}
}
