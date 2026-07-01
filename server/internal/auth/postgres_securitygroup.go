package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresSecurityGroups implements [SecurityGroupRepository] against
// PostgreSQL. The db package owns the security_groups +
// security_group_members schemas and migrations; this adapter only issues
// queries.
type PostgresSecurityGroups struct {
	db *sql.DB
}

// NewPostgresSecurityGroups returns a Postgres-backed SecurityGroupRepository.
func NewPostgresSecurityGroups(db *sql.DB) *PostgresSecurityGroups {
	return &PostgresSecurityGroups{db: db}
}

func (p *PostgresSecurityGroups) Create(ctx context.Context, g *SecurityGroup) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO security_groups (id, org_id, name, description, is_system, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
			g.ID, tenant.OrgID, g.Name, g.Description, g.IsSystem)
		return err
	})
}

func (p *PostgresSecurityGroups) Get(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error) {
	var g SecurityGroup
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT id, name, description, is_system, created_at, updated_at
			 FROM security_groups
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`,
			id).Scan(&g.ID, &g.Name, &g.Description, &g.IsSystem, &g.CreatedAt, &g.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecurityGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (p *PostgresSecurityGroups) List(ctx context.Context) ([]*SecurityGroup, error) {
	var groups []*SecurityGroup
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, name, description, is_system, created_at, updated_at
			 FROM security_groups
			 WHERE org_id = current_setting('app.current_org')::uuid
			 ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var g SecurityGroup
			if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.IsSystem, &g.CreatedAt, &g.UpdatedAt); err != nil {
				return err
			}
			groups = append(groups, &g)
		}
		return rows.Err()
	})
	return groups, err
}

func (p *PostgresSecurityGroups) Delete(ctx context.Context, id SecurityGroupID) error {
	g, err := p.Get(ctx, id)
	if err != nil {
		return err
	}
	if g.IsSystem {
		return ErrSystemGroup
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM security_groups
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrSecurityGroupNotFound
		}
		return nil
	})
}

func (p *PostgresSecurityGroups) AddMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	if err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO security_group_members (org_id, group_id, user_id, added_at)
			 VALUES ($1, $2, $3, NOW())
			 ON CONFLICT DO NOTHING`,
			tenant.OrgID, groupID, userID)
		return err
	}); err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return p.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (p *PostgresSecurityGroups) RemoveMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	if groupID == AdminGroupID {
		count, err := p.CountMembers(ctx, groupID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	if err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM security_group_members
			 WHERE org_id = current_setting('app.current_org')::uuid AND group_id = $1 AND user_id = $2`,
			groupID, userID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrMemberNotFound
		}
		return nil
	}); err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return p.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (p *PostgresSecurityGroups) ListMembers(ctx context.Context, groupID SecurityGroupID) ([]*Member, error) {
	var members []*Member
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT u.id, u.email, u.display_name, u.is_admin, u.created_at, u.updated_at
			 FROM users u
			 INNER JOIN security_group_members sgm ON sgm.user_id = u.id
			 WHERE sgm.org_id = current_setting('app.current_org')::uuid
			   AND u.org_id = current_setting('app.current_org')::uuid
			   AND sgm.group_id = $1
			 ORDER BY u.email`,
			groupID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.ID, &m.Email, &m.DisplayName, &m.IsAdmin, &m.CreatedAt, &m.UpdatedAt); err != nil {
				return err
			}
			members = append(members, &m)
		}
		return rows.Err()
	})
	return members, err
}

func (p *PostgresSecurityGroups) IsUserInGroup(ctx context.Context, userID uuid.UUID, groupID SecurityGroupID) (bool, error) {
	var exists bool
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT EXISTS(
				SELECT 1 FROM security_group_members
				WHERE org_id = current_setting('app.current_org')::uuid AND group_id = $1 AND user_id = $2)`,
			groupID, userID).Scan(&exists)
	})
	return exists, err
}

func (p *PostgresSecurityGroups) CountMembers(ctx context.Context, groupID SecurityGroupID) (int, error) {
	var count int
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM security_group_members
			 WHERE org_id = current_setting('app.current_org')::uuid AND group_id = $1`,
			groupID).Scan(&count)
	})
	return count, err
}

// syncIsAdmin keeps the users.is_admin boolean in sync with Administrators
// group membership. Called from Add/RemoveMember whenever the touched group is
// AdminGroupID. The coupling between the security_group_members and users
// tables is intentionally contained inside this adapter — both tables belong
// to the auth aggregate, so an inline UPDATE is preferred over a use-case-
// orchestrated transaction.
func (p *PostgresSecurityGroups) syncIsAdmin(ctx context.Context, userID uuid.UUID) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE users SET is_admin = EXISTS(
				SELECT 1 FROM security_group_members
				WHERE org_id = current_setting('app.current_org')::uuid AND user_id = $1 AND group_id = $2
			), updated_at = NOW()
			WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`,
			userID, AdminGroupID)
		return err
	})
}
