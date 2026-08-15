package rules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// Which machines cannot evaluate which rules — the one coverage state that is
// durable. Presence of a row is the state, so there is no column that can go
// stale, and steady state costs no writes at all.

const (
	markUnsupportedSQL = `INSERT INTO rule_coverage_unsupported
		   (tenant_id, organization_id, device_id, rule_id, since, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())
		 ON CONFLICT (device_id, rule_id) DO NOTHING`

	clearUnsupportedSQL = `DELETE FROM rule_coverage_unsupported
		  WHERE ` + scopedToTenant + ` AND device_id = $1 AND rule_id = $2`

	countUnsupportedSQL = `SELECT rule_id, COUNT(*)
		   FROM rule_coverage_unsupported
		  WHERE ` + scopedToTenant + ` AND organization_id = $1
		  GROUP BY rule_id`

	eraseDeviceCoverageSQL = `DELETE FROM rule_coverage_unsupported
		  WHERE ` + scopedToTenant + ` AND device_id = $1`

	unsupportedSinceSQL = `SELECT since FROM rule_coverage_unsupported
		  WHERE ` + scopedToTenant + ` AND device_id = $1 AND rule_id = $2`

	// fleetCoverageSQL is the same question asked about the whole install: how
	// many machines there are, and how many of them cannot evaluate each rule.
	//
	// Both halves in one statement, because the caller refreshes a gauge from
	// this on a timer and the two numbers are only meaningful against each other
	// — a blind-spot count read a moment apart from the fleet it is a fraction of
	// would report a share nobody's estate was ever in. The fleet size rides on
	// every row so a single pass answers both, and the CROSS JOIN keeps it
	// present on the row an install with nothing blind still returns.
	//
	// It names no tenant, deliberately: the platform's own view is of every
	// tenant at once, including the ones nobody is currently serving requests
	// for, so there is nothing for a predicate to confine it to. It runs
	// admin-scoped for the same reason a purge does.
	fleetCoverageSQL = `
		SELECT f.machines, u.rule_id, u.blind
		  FROM (SELECT COUNT(*) AS machines FROM devices) f
		  LEFT JOIN (SELECT rule_id, COUNT(*) AS blind
		               FROM rule_coverage_unsupported
		              GROUP BY rule_id) u ON TRUE`
)

// MarkUnsupported records that a machine cannot evaluate a rule. A machine that
// already said so keeps its original since, so "blind since March" stays true
// across every later report — and a repeated report costs no write.
func (s *Store) MarkUnsupported(ctx context.Context, organizationID, deviceID uuid.UUID, ruleID string) error {
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	return s.exec(ctx, "mark rule unsupported", markUnsupportedSQL,
		tenant, organizationID, deviceID, ruleID)
}

// ClearUnsupported records that a machine can evaluate a rule again. The row is
// deleted rather than flipped to an active state: there is no stored active, so
// there is nothing that can go stale into a claim that a decommissioned machine
// is being watched.
func (s *Store) ClearUnsupported(ctx context.Context, deviceID uuid.UUID, ruleID string) error {
	return s.exec(ctx, "clear rule unsupported", clearUnsupportedSQL, deviceID, ruleID)
}

// CountUnsupported returns, per rule, how many of a customer's machines cannot
// evaluate it. Rules nothing is blind to are absent rather than present as zero.
func (s *Store) CountUnsupported(ctx context.Context, organizationID uuid.UUID) (map[string]int, error) {
	out := make(map[string]int)
	err := s.eachRow(ctx, "count unsupported coverage", countUnsupportedSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var (
				ruleID string
				count  int
			)
			if err := rows.Scan(&ruleID, &count); err != nil {
				return fmt.Errorf("scan unsupported coverage: %w", err)
			}
			out[ruleID] = count
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FleetCoverage returns how many machines the whole install has, and per rule
// how many of them cannot evaluate it.
//
// It is the fleet-wide counterpart of [Store.CountUnsupported]: that one answers
// "how much of Contoso's estate is this rule watching", and this one answers "how
// much of everything", which is the question a staged rollout is actually judged
// on. Rules nothing is blind to are absent rather than present as zero, the same
// as the per-customer read.
//
// It scopes itself, because its caller is a background refresh belonging to no
// request and has no tenant to pass.
func (s *Store) FleetCoverage(ctx context.Context) (int, map[string]int, error) {
	ctx = dbtx.WithDefaultTenant(ctx, true)

	var fleet int
	blind := make(map[string]int)
	err := s.eachRow(ctx, "count fleet coverage", fleetCoverageSQL, nil,
		func(rows *sql.Rows) error {
			var (
				machines int
				ruleID   sql.NullString
				count    sql.NullInt64
			)
			if err := rows.Scan(&machines, &ruleID, &count); err != nil {
				return fmt.Errorf("scan fleet coverage: %w", err)
			}
			fleet = machines
			// An install with nothing blind still answers with its fleet size,
			// on one row carrying no rule.
			if ruleID.Valid {
				blind[ruleID.String] = int(count.Int64)
			}
			return nil
		})
	if err != nil {
		return 0, nil, err
	}
	return fleet, blind, nil
}

// EraseDeviceCoverage drops everything a machine ever reported it could not
// evaluate. A decommissioned machine that kept its rows would inflate a
// customer's blind spot forever.
func (s *Store) EraseDeviceCoverage(ctx context.Context, deviceID uuid.UUID) error {
	return s.exec(ctx, "erase device rule coverage", eraseDeviceCoverageSQL, deviceID)
}

// UnsupportedSince reports when a machine first said it could not evaluate a
// rule, and whether it says so at all.
func (s *Store) UnsupportedSince(ctx context.Context, deviceID uuid.UUID, ruleID string) (time.Time, bool, error) {
	var since time.Time
	found, err := s.queryRow(ctx, "read unsupported coverage", unsupportedSinceSQL,
		[]any{deviceID, ruleID}, &since)
	if err != nil {
		return time.Time{}, false, err
	}
	return since, found, nil
}
