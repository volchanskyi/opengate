package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// A customer's parameter overrides, stored and read back.

const (
	upsertBindingSQL = `INSERT INTO rule_bindings
		   (id, tenant_id, organization_id, rule_id, level, level_key,
		    selector, precedence, params, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), $10)
		 ON CONFLICT (organization_id, rule_id, level, level_key, selector)
		 DO UPDATE SET precedence = EXCLUDED.precedence,
		               params     = EXCLUDED.params,
		               updated_at = NOW(),
		               updated_by = EXCLUDED.updated_by`

	listBindingsSQL = `SELECT id, organization_id, rule_id, level, level_key,
		        selector, precedence, params, updated_by
		   FROM rule_bindings
		  WHERE ` + scopedToTenant + ` AND organization_id = $1
		  ORDER BY rule_id, level, level_key, precedence DESC, id`

	deleteBindingSQL = `DELETE FROM rule_bindings WHERE ` + scopedToTenant + ` AND id = $1`
)

// UpsertBinding validates a binding against the rule it names and stores it.
// Validation happens here rather than at a read, so a value the rule's author
// never allowed is refused while an operator is still looking at it.
func (s *Store) UpsertBinding(ctx context.Context, cat *Catalogue, b Binding) error {
	if err := ValidateBindingAgainst(cat, b); err != nil {
		return err
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	selector, params, err := encodeBindingJSON(b)
	if err != nil {
		return err
	}
	return s.exec(ctx, "upsert rule binding", upsertBindingSQL,
		b.ID, tenant, b.OrganizationID, b.RuleID, levelNames[b.Level], b.LevelKey,
		selector, b.Precedence, params, b.UpdatedBy)
}

// encodeBindingJSON renders the two jsonb columns. A nil selector is stored as
// the empty object, which is how the level's blanket binding is spelled and what
// the partial unique index keys off.
func encodeBindingJSON(b Binding) (selector, params []byte, err error) {
	selector, err = json.Marshal(orEmptyMap(b.Selector))
	if err != nil {
		return nil, nil, fmt.Errorf("encode selector: %w", err)
	}
	params, err = json.Marshal(orEmptyMap(b.Params))
	if err != nil {
		return nil, nil, fmt.Errorf("encode params: %w", err)
	}
	return selector, params, nil
}

// orEmptyMap renders a nil map as an empty JSON object rather than null, so the
// column's shape never depends on whether a caller left a field out.
func orEmptyMap[M ~map[string]V, V any](m M) M {
	if m == nil {
		return M{}
	}
	return m
}

// ListBindings returns every binding one customer has for any rule, ordered so
// the result is stable across reads.
func (s *Store) ListBindings(ctx context.Context, organizationID uuid.UUID) ([]Binding, error) {
	var out []Binding
	err := s.eachRow(ctx, "list rule bindings", listBindingsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			b, err := scanBinding(rows)
			if err != nil {
				return err
			}
			out = append(out, b)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scanBinding reads one row into a Binding.
func scanBinding(rows *sql.Rows) (Binding, error) {
	var (
		b        Binding
		levelStr string
		selector []byte
		params   []byte
	)
	if err := rows.Scan(&b.ID, &b.OrganizationID, &b.RuleID, &levelStr, &b.LevelKey,
		&selector, &b.Precedence, &params, &b.UpdatedBy); err != nil {
		return Binding{}, fmt.Errorf("scan rule binding: %w", err)
	}
	b.Level = levelByName[levelStr]

	var sel Selector
	if err := json.Unmarshal(selector, &sel); err != nil {
		return Binding{}, fmt.Errorf("decode selector: %w", err)
	}
	if len(sel) > 0 {
		b.Selector = sel
	}
	if err := json.Unmarshal(params, &b.Params); err != nil {
		return Binding{}, fmt.Errorf("decode params: %w", err)
	}
	return b, nil
}

// DeleteBinding removes one binding.
func (s *Store) DeleteBinding(ctx context.Context, id uuid.UUID) error {
	return s.exec(ctx, "delete rule binding", deleteBindingSQL, id)
}
