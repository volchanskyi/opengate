package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPathSecurityGroups  = "/api/v1/security-groups"
	testPathSecurityGroupsS = "/api/v1/security-groups/"
)

func TestSecurityGroupHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, adminToken := seedTestUser(t, srv, cfg, "sgadmin@example.com", true)
	_, nonAdminToken := seedTestUser(t, srv, cfg, "sguser@example.com", false)

	var createdGroupID uuid.UUID

	t.Run("list security groups as non-admin returns 403", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroups, nonAdminToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("create security group as non-admin returns 403", func(t *testing.T) {
		body := map[string]string{"name": "forbidden-group"}
		w := doRequest(srv, http.MethodPost, testPathSecurityGroups, nonAdminToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("create security group missing name returns 400", func(t *testing.T) {
		body := map[string]string{}
		w := doRequest(srv, http.MethodPost, testPathSecurityGroups, adminToken, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create security group", func(t *testing.T) {
		body := map[string]string{"name": "test-security-group", "description": "A test group"}
		w := doRequest(srv, http.MethodPost, testPathSecurityGroups, adminToken, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var g SecurityGroup
		err := json.NewDecoder(w.Body).Decode(&g)
		require.NoError(t, err)
		assert.Equal(t, "test-security-group", g.Name)
		assert.Equal(t, "A test group", g.Description)
		createdGroupID = g.Id
	})

	t.Run("list security groups", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroups, adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var groups []SecurityGroup
		err := json.NewDecoder(w.Body).Decode(&groups)
		require.NoError(t, err)
		// At least the one we created (may also have system groups from migration).
		assert.GreaterOrEqual(t, len(groups), 1)
	})

	t.Run("get security group", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroupsS+createdGroupID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var g SecurityGroupWithMembers
		err := json.NewDecoder(w.Body).Decode(&g)
		require.NoError(t, err)
		assert.Equal(t, "test-security-group", g.Name)
	})

	t.Run("get security group as non-admin returns 403", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroupsS+createdGroupID.String(), nonAdminToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("get security group not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroupsS+uuid.New().String(), adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete security group as non-admin returns 403", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+createdGroupID.String(), nonAdminToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete security group not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+uuid.New().String(), adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete security group", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+createdGroupID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

func TestSecurityGroupMemberHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	adminUser, adminToken := seedTestUser(t, srv, cfg, "memadmin@example.com", true)
	_, nonAdminToken := seedTestUser(t, srv, cfg, "memuser@example.com", false)

	// Create a group to work with.
	body := map[string]string{"name": "member-test-group"}
	w := doRequest(srv, http.MethodPost, testPathSecurityGroups, adminToken, body)
	require.Equal(t, http.StatusCreated, w.Code)

	var group SecurityGroup
	err := json.NewDecoder(w.Body).Decode(&group)
	require.NoError(t, err)
	groupID := group.Id

	membersPath := testPathSecurityGroupsS + groupID.String() + "/members"

	t.Run("add member as non-admin returns 403", func(t *testing.T) {
		body := map[string]string{"user_id": adminUser.ID.String()}
		w := doRequest(srv, http.MethodPost, membersPath, nonAdminToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("add member to non-existent group returns 404", func(t *testing.T) {
		body := map[string]string{"user_id": adminUser.ID.String()}
		w := doRequest(srv, http.MethodPost, testPathSecurityGroupsS+uuid.New().String()+"/members", adminToken, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("add non-existent user returns 404", func(t *testing.T) {
		body := map[string]string{"user_id": uuid.New().String()}
		w := doRequest(srv, http.MethodPost, membersPath, adminToken, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("add member", func(t *testing.T) {
		body := map[string]string{"user_id": adminUser.ID.String()}
		w := doRequest(srv, http.MethodPost, membersPath, adminToken, body)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("get group shows member", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathSecurityGroupsS+groupID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var g SecurityGroupWithMembers
		err := json.NewDecoder(w.Body).Decode(&g)
		require.NoError(t, err)
		assert.Len(t, g.Members, 1)
	})

	t.Run("remove member as non-admin returns 403", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+groupID.String()+"/members/"+adminUser.ID.String(), nonAdminToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("remove non-existent member returns 404", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+groupID.String()+"/members/"+uuid.New().String(), adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("remove member", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathSecurityGroupsS+groupID.String()+"/members/"+adminUser.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
