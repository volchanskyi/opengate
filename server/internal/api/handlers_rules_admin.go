package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// Changing what a rule does to an estate.
//
// Every write across this and the two files beside it is administrator-only and
// lands in the audit log, because each of them changes what a fleet is watched
// for and the answer to "who widened that threshold" has to exist. What none of
// them can do is change a rule's logic: definitions are compiled in,
// cost-bounded in CI, and there is no route to them from any endpoint. What an
// operator changes is the numbers the rule declared adjustable, who it reaches,
// and whether it runs at all.
//
// What is here is what all of them share. The reads are in
// handlers_rules_read.go, the tuning in handlers_rules_tuning.go, and how far
// and how fast a rule spreads in handlers_rules_rollout.go.

// errRulesNotAdministrable is a deployment wired without the mutable half of the
// rule store. The read-only catalogue can still be served; nothing can be
// changed.
var errRulesNotAdministrable = errors.New("rule administration is not configured on this server")

const msgRuleNotFound = "no such rule in the pack this server runs"

// customerOrDefault resolves the customer a write is filed against.
//
// A write that names none takes the tenant's own, which is the same rule a site
// or a device create follows — and it is not a convenience. Every one of these
// rows is keyed on a customer, so an unnamed one would be filed against nothing
// and refused by the database, on the screen's *default* state: the picker
// starts on every customer, so every write would fail until somebody narrowed
// it, with an error naming a foreign key.
func (s *Server) customerOrDefault(ctx context.Context, named *uuid.UUID) (uuid.UUID, error) {
	if named != nil && *named != uuid.Nil {
		return *named, nil
	}
	return s.organizations.EnsureDefault(ctx)
}

// administrableRule resolves the rule a write names, on a server wired to change
// anything at all.
//
// The two questions are asked together because the answers are different kinds
// of thing and a caller must not conflate them: a server without the mutable
// store cannot honour the request at all, while an unknown rule is an ordinary
// answer to an ordinary question. It returns found=false for the second, and an
// error only for the first.
func (s *Server) administrableRule(ruleID string) (rules.Definition, bool, error) {
	if s.ruleCatalogue == nil || s.ruleAdmin == nil {
		return rules.Definition{}, false, errRulesNotAdministrable
	}
	definition, ok := s.ruleCatalogue.Lookup(ruleID)
	return definition, ok, nil
}
