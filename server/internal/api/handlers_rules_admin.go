package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// errRulesNotAdministrable is a deployment wired without the mutable half of the rule store,
// so the read-only catalogue is served and nothing can be changed.
var errRulesNotAdministrable = errors.New("rule administration is not configured on this server")

const msgRuleNotFound = "no such rule in the pack this server runs"

// customerOrDefault resolves the customer a write is filed against, taking the tenant's own
// when none is named because every row is keyed on a customer.
func (s *Server) customerOrDefault(ctx context.Context, named *uuid.UUID) (uuid.UUID, error) {
	if named != nil && *named != uuid.Nil {
		return *named, nil
	}
	return s.organizations.EnsureDefault(ctx)
}

// administrableRule resolves the rule a write names; found is false for an unknown rule and
// the error is set only when the server cannot administer rules at all.
func (s *Server) administrableRule(ruleID string) (rules.Definition, bool, error) {
	if s.ruleCatalogue == nil || s.ruleAdmin == nil {
		return rules.Definition{}, false, errRulesNotAdministrable
	}
	definition, ok := s.ruleCatalogue.Lookup(ruleID)
	return definition, ok, nil
}
