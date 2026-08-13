package rules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
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
