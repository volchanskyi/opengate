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

// A customer's label list and which machines carry which label, stored and read
// back.

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

	// A rule aimed at the label, in the customer that owns it. The selector is a
	// bare key and value, so a check that forgot the customer would refuse a
	// delete because somebody else in the tenant uses the same word.
	labelAimedAtSQL = `SELECT COUNT(*) FROM rule_bindings
		  WHERE ` + scopedToTenant + ` AND organization_id = $1 AND selector @> $2::jsonb`

	// Deleting takes the label's assignments with it. The database refuses the
	// delete otherwise, which would leave an operator with an error naming a
	// constraint rather than machines.
	deleteTagsOfLabelSQL = `DELETE FROM device_tags WHERE ` + scopedToTenant + ` AND label_id = $1`

	deleteLabelSQL = `DELETE FROM device_tag_labels WHERE ` + scopedToTenant + ` AND id = $1`

	// The machine's customer and the label's have to be the same one, and the
	// join is what decides it rather than a check the caller has to remember. No
	// row joins when they differ, which is the refusal.
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

// ListLabels returns one customer's list, ordered so the screen reading it is
// stable across reads.
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

// DeleteLabel removes one entry from a customer's list, and the assignments that
// carried it.
//
// It is refused while a rule is aimed at the label. Removing it then would take
// a targeted override off every machine that carried it — which does not read as
// a deletion at all, it reads as a threshold that quietly widened across an
// estate one afternoon.
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

// AssignTag gives one machine one label, replacing whatever it carried for that
// label's key. A machine and a label belonging to different customers is
// refused: the isolation wall is at the tenant, so nothing in the database stops
// that on its own.
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

// TagsFor reads one machine's labels, which is what a targeted binding's
// selector is matched against.
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

// ListTagAssignments reads every machine's labels for one customer, which is
// what the assignment screen lists.
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

// DescribeSelector renders a selector as an operator reads it, so a targeted
// binding says which machines it is aimed at rather than showing a map.
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

// sortedKeys gives a selector a stable reading order.
func sortedKeys(s Selector) []string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
