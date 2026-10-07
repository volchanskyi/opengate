package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	createLabelSQL = `INSERT INTO device_tag_labels
		   (id, tenant_id, organization_id, key, value, created_at, created_by)
		 VALUES ($1, $2, $3, $4, $5, NOW(), $6)
		 ON CONFLICT (organization_id, key, value) DO NOTHING`

	listLabelsSQL = `SELECT id, organization_id, key, value, created_by
		   FROM device_tag_labels
		  WHERE ` + scopedToTenant + ` AND organization_id = $1
		  ORDER BY key, value`

	labelByIDSQL = `SELECT id, organization_id, key, value, created_by
		   FROM device_tag_labels
		  WHERE ` + scopedToTenant + ` AND id = $1`

	// The organization predicate keeps another customer's identical key and value from matching.
	labelAimedAtSQL = `SELECT COUNT(*) FROM rule_bindings
		  WHERE ` + scopedToTenant + ` AND organization_id = $1 AND selector @> $2::jsonb`

	// The label's assignments go first so the label delete meets no constraint.
	deleteTagsOfLabelSQL = `DELETE FROM device_tags WHERE ` + scopedToTenant + ` AND label_id = $1`

	deleteLabelSQL = `DELETE FROM device_tag_labels WHERE ` + scopedToTenant + ` AND id = $1`

	// The join on organization yields no row when machine and label belong to different customers.
	assignTagSQL = `INSERT INTO device_tags
		   (tenant_id, organization_id, device_id, label_id, key, value, assigned_at, assigned_by)
		 SELECT d.tenant_id, d.organization_id, d.id, l.id, l.key, l.value, NOW(), $3
		   FROM devices d
		   JOIN device_tag_labels l ON l.organization_id = d.organization_id
		  WHERE d.id = $1 AND l.id = $2
		    AND d.` + scopedToTenant + `
		 ON CONFLICT (device_id, key)
		 DO UPDATE SET label_id    = EXCLUDED.label_id,
		               value       = EXCLUDED.value,
		               assigned_at = NOW(),
		               assigned_by = EXCLUDED.assigned_by`

	clearTagSQL = `DELETE FROM device_tags
		  WHERE ` + scopedToTenant + ` AND device_id = $1 AND key = $2`

	tagsForDeviceSQL = `SELECT key, value FROM device_tags
		  WHERE ` + scopedToTenant + ` AND device_id = $1`

	listTagAssignmentsSQL = `SELECT device_id, key, value FROM device_tags
		  WHERE ` + scopedToTenant + ` AND organization_id = $1
		  ORDER BY device_id, key`
)

// CreateLabel adds one entry to a customer's list.
func (s *Store) CreateLabel(ctx context.Context, l Label) error {
	if err := ValidateLabel(l); err != nil {
		return err
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	created, err := s.affected(ctx, "create device tag label", createLabelSQL,
		l.ID, tenant, l.OrganizationID, l.Key, l.Value, l.CreatedBy)
	if err != nil {
		return err
	}
	if created == 0 {
		return fmt.Errorf("%w: %s=%s", ErrLabelExists, l.Key, l.Value)
	}
	return nil
}

// ListLabels returns one customer's labels in key and value order.
func (s *Store) ListLabels(ctx context.Context, organizationID uuid.UUID) ([]Label, error) {
	var out []Label
	err := s.eachRow(ctx, "list device tag labels", listLabelsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var l Label
			if err := rows.Scan(&l.ID, &l.OrganizationID, &l.Key, &l.Value, &l.CreatedBy); err != nil {
				return fmt.Errorf("scan device tag label: %w", err)
			}
			out = append(out, l)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Label reads one entry by id.
func (s *Store) Label(ctx context.Context, id uuid.UUID) (Label, error) {
	var l Label
	found, err := s.queryRow(ctx, "read device tag label", labelByIDSQL, []any{id},
		&l.ID, &l.OrganizationID, &l.Key, &l.Value, &l.CreatedBy)
	if err != nil {
		return Label{}, err
	}
	if !found {
		return Label{}, fmt.Errorf("%w: %s", ErrLabelNotFound, id)
	}
	return l, nil
}

// DeleteLabel removes a label and its assignments, and returns ErrLabelInUse while a rule targets
// it, since deleting it would silently widen that rule's override.
func (s *Store) DeleteLabel(ctx context.Context, id uuid.UUID) error {
	label, err := s.Label(ctx, id)
	if err != nil {
		return err
	}
	selector, err := json.Marshal(label.Selector())
	if err != nil {
		return fmt.Errorf("encode selector: %w", err)
	}

	var aimed int
	if _, err := s.queryRow(ctx, "count rules aimed at label", labelAimedAtSQL,
		[]any{label.OrganizationID, selector}, &aimed); err != nil {
		return err
	}
	if aimed > 0 {
		return fmt.Errorf("%w: %s=%s is aimed at by %d rule settings",
			ErrLabelInUse, label.Key, label.Value, aimed)
	}

	if err := s.exec(ctx, "clear device tags of label", deleteTagsOfLabelSQL, id); err != nil {
		return err
	}
	return s.exec(ctx, "delete device tag label", deleteLabelSQL, id)
}

// AssignTag gives one machine one label, replacing its label for that key, and returns
// ErrLabelForeign when machine and label belong to different customers.
func (s *Store) AssignTag(ctx context.Context, deviceID, labelID uuid.UUID, assignedBy string) error {
	assigned, err := s.affected(ctx, "assign device tag", assignTagSQL, deviceID, labelID, assignedBy)
	if err != nil {
		return err
	}
	if assigned == 0 {
		return fmt.Errorf("%w: label %s and machine %s", ErrLabelForeign, labelID, deviceID)
	}
	return nil
}

// ClearTag takes one key off one machine.
func (s *Store) ClearTag(ctx context.Context, deviceID uuid.UUID, key string) error {
	return s.exec(ctx, "clear device tag", clearTagSQL, deviceID, key)
}

// TagsFor reads one machine's labels, the set a binding selector is matched against.
func (s *Store) TagsFor(ctx context.Context, deviceID uuid.UUID) (map[string]string, error) {
	out := make(map[string]string)
	err := s.eachRow(ctx, "read device tags", tagsForDeviceSQL, []any{deviceID},
		func(rows *sql.Rows) error {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return fmt.Errorf("scan device tag: %w", err)
			}
			out[key] = value
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListTagAssignments reads every machine's labels for one customer.
func (s *Store) ListTagAssignments(ctx context.Context, organizationID uuid.UUID) (map[uuid.UUID]map[string]string, error) {
	out := make(map[uuid.UUID]map[string]string)
	err := s.eachRow(ctx, "list device tag assignments", listTagAssignmentsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var (
				deviceID   uuid.UUID
				key, value string
			)
			if err := rows.Scan(&deviceID, &key, &value); err != nil {
				return fmt.Errorf("scan device tag assignment: %w", err)
			}
			if out[deviceID] == nil {
				out[deviceID] = make(map[string]string)
			}
			out[deviceID][key] = value
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DescribeSelector renders a selector as sorted comma-separated key=value pairs.
func DescribeSelector(s Selector) string {
	if s.IsEmpty() {
		return ""
	}
	parts := make([]string, 0, len(s))
	for _, key := range sortedKeys(s) {
		parts = append(parts, key+"="+s[key])
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(s Selector) []string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
