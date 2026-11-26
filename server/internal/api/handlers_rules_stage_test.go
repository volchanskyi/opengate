package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/audit"
)

func TestAnAdministratorMovesARuleToTheStageTheyPick(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)
	ctx := testTenantContext(t)

	for _, step := range []struct {
		stage RuleRolloutStage
		reach int
	}{{"canary", 5}, {"staged", 25}, {"full", 100}, {"canary", 5}} {
		resp := doRequest(e.srv, http.MethodPut,
			e.query(testPathRules+"/disk-critical/rollout"), e.adminToken,
			RuleRolloutInput{Enabled: true, Stage: step.stage, CanaryPercent: 5, StagedPercent: 25})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var answered RuleRollout
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &answered))
		assert.Equal(t, RuleStage(step.stage), answered.Stage)
		assert.Equal(t, step.reach, answered.RolloutPercent, "the reach follows the share its stage names")

		read := doRequest(e.srv, http.MethodGet, e.query(testPathRules+"/disk-critical"), e.adminToken, nil)
		require.Equal(t, http.StatusOK, read.Code, read.Body.String())
		var detail RuleDetail
		require.NoError(t, json.Unmarshal(read.Body.Bytes(), &detail))
		assert.Equal(t, step.reach, detail.Rule.Rollout.RolloutPercent, "the stage is stored, not only answered")
		assert.Equal(t, RuleStage(step.stage), detail.Rule.Rollout.Stage)

		require.Eventually(t, func() bool {
			events, err := e.srv.audit.Query(ctx, audit.Query{Action: "rule.rollout.set", Limit: 50})
			if err != nil {
				return false
			}
			for _, event := range events {
				if strings.Contains(event.Details, "stage="+string(step.stage)) {
					return true
				}
			}
			return false
		}, auditWaitFor, auditPollEvery, "the audit log names the stage %s", step.stage)
	}
}

func TestAStageMoveLeavesAStopInPlace(t *testing.T) {
	t.Parallel()
	e := newRuleAdminEstate(t)

	stop := doRequest(e.srv, http.MethodPost, e.query(testPathRules+"/disk-critical/stop"), e.adminToken,
		RuleStopInput{Scope: RuleStopScopeOrganization, Stopped: true})
	require.Equal(t, http.StatusNoContent, stop.Code, stop.Body.String())

	moved := doRequest(e.srv, http.MethodPut, e.query(testPathRules+"/disk-critical/rollout"), e.adminToken,
		RuleRolloutInput{Enabled: true, Stage: "staged", CanaryPercent: 1, StagedPercent: 10})
	require.Equal(t, http.StatusOK, moved.Code, moved.Body.String())

	assert.True(t, e.killed(t, e.org, "disk-critical"), "moving a stopped rule's stage leaves it stopped")
}
