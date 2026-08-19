package rules

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// The moves a rule upgrade made to a customer's tuning, stored until somebody
// has seen them.

// ErrClampNotFound means no outstanding move has that id — it was never
// recorded, or somebody has already acknowledged it.
var ErrClampNotFound = errors.New("no outstanding clamp with that id")

const (
	// Keyed on the binding, the parameter and the version that narrowed the
	// range, so reading the same upgrade twice keeps the first record rather
	// than making a second. The first record is the one that carries when the
	// move actually happened.
	recordClampSQL = `INSERT INTO rule_binding_clamps
		   (id, tenant_id, organization_id, binding_id, rule_id, rule_version,
		    param, from_value, to_value, clamped_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		 ON CONFLICT (binding_id, param, rule_version) DO NOTHING`

	listOutstandingClampsSQL = `SELECT id, organization_id, binding_id, rule_id, rule_version,
		        param, from_value, to_value, clamped_at
		   FROM rule_binding_clamps
		  WHERE ` + scopedToTenant + ` AND organization_id = $1 AND acknowledged_at IS NULL
		  ORDER BY clamped_at, id`

	acknowledgeClampSQL = `UPDATE rule_binding_clamps
		    SET acknowledged_at = NOW(), acknowledged_by = $2
		  WHERE ` + scopedToTenant + ` AND id = $1 AND acknowledged_at IS NULL`
)

// ReconcileClamps reads one customer's tuning against the pack as it now stands,
// records every value a rule version no longer allows, and returns what is still
// outstanding.
//
// It is idempotent, which is what lets it run on the read that displays the
// flag: an upgrade landed six weeks ago records the move it made then, once,
// however many times somebody opens the screen.
func (s *Store) ReconcileClamps(ctx context.Context, cat Pack, organizationID uuid.UUID) ([]Clamp, error) {
	bindings, err := s.ListBindings(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return nil, err
	}

	for _, move := range ClampBindings(cat, bindings) {
		if err := s.exec(ctx, "record rule binding clamp", recordClampSQL,
			move.ID, tenant, move.OrganizationID, move.BindingID, move.RuleID,
			move.RuleVersion, move.Param, move.From, move.To); err != nil {
			return nil, err
		}
	}
	return s.ListClamps(ctx, organizationID)
}

// ListClamps returns the moves one customer has not acknowledged yet.
func (s *Store) ListClamps(ctx context.Context, organizationID uuid.UUID) ([]Clamp, error) {
	var out []Clamp
	err := s.eachRow(ctx, "list rule binding clamps", listOutstandingClampsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var c Clamp
			if err := rows.Scan(&c.ID, &c.OrganizationID, &c.BindingID, &c.RuleID,
				&c.RuleVersion, &c.Param, &c.From, &c.To, &c.ClampedAt); err != nil {
				return fmt.Errorf("scan rule binding clamp: %w", err)
			}
			out = append(out, c)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AcknowledgeClamp records that an administrator has seen one move. Only an
// outstanding one can be acknowledged, so the action says what it did rather
// than succeeding against a move somebody else already handled.
func (s *Store) AcknowledgeClamp(ctx context.Context, id uuid.UUID, acknowledgedBy string) error {
	acknowledged, err := s.affected(ctx, "acknowledge rule binding clamp",
		acknowledgeClampSQL, id, acknowledgedBy)
	if err != nil {
		return err
	}
	if acknowledged == 0 {
		return fmt.Errorf("%w: %s", ErrClampNotFound, id)
	}
	return nil
}
