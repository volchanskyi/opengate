package rules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// A row's presence is the unsupported state, so no column can go stale.

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

	// One statement reads the fleet size and the blind counts so the two stay consistent.
	// It names no tenant: the platform view spans every tenant and runs admin-scoped.
	fleetCoverageSQL = `
		SELECT f.machines, u.rule_id, u.blind
		  FROM (SELECT COUNT(*) AS machines FROM devices) f
		  LEFT JOIN (SELECT rule_id, COUNT(*) AS blind
		               FROM rule_coverage_unsupported
		              GROUP BY rule_id) u ON TRUE`
)

// MarkUnsupported records that a machine cannot evaluate a rule; a repeat keeps the original since.
func (s *Store) MarkUnsupported(ctx context.Context, organizationID, deviceID uuid.UUID, ruleID string) error {
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	return s.exec(ctx, "mark rule unsupported", markUnsupportedSQL,
		tenant, organizationID, deviceID, ruleID)
}

// ClearUnsupported records that a machine can evaluate a rule again by deleting its row.
func (s *Store) ClearUnsupported(ctx context.Context, deviceID uuid.UUID, ruleID string) error {
	return s.exec(ctx, "clear rule unsupported", clearUnsupportedSQL, deviceID, ruleID)
}

// CountUnsupported returns, per rule, how many of a customer's machines cannot
// evaluate it; a rule nothing is blind to is absent from the map.
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

// FleetCoverage returns the install-wide machine count and, per rule, how many cannot evaluate it.
// It scopes itself because its caller is a background job with no tenant; unblind rules are absent.
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

// EraseDeviceCoverage drops every unsupported-rule row a machine reported.
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
