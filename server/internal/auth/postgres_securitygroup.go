package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
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
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO security_groups (id, name, description, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		g.ID, g.Name, g.Description, g.IsSystem)
	return err
}

func (p *PostgresSecurityGroups) Get(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error) {
	var g SecurityGroup
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups WHERE id = $1`,
		id).Scan(&g.ID, &g.Name, &g.Description, &g.IsSystem, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecurityGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (p *PostgresSecurityGroups) List(ctx context.Context) ([]*SecurityGroup, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*SecurityGroup
	for rows.Next() {
		var g SecurityGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.IsSystem, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, &g)
	}
	return groups, rows.Err()
}

func (p *PostgresSecurityGroups) Delete(ctx context.Context, id SecurityGroupID) error {
	g, err := p.Get(ctx, id)
	if err != nil {
		return err
	}
	if g.IsSystem {
		return ErrSystemGroup
	}
	res, err := p.db.ExecContext(ctx, `DELETE FROM security_groups WHERE id = $1`, id)
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
}

func (p *PostgresSecurityGroups) AddMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO security_group_members (group_id, user_id, added_at) VALUES ($1, $2, NOW())
		 ON CONFLICT DO NOTHING`,
		groupID, userID)
	if err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return p.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (p *PostgresSecurityGroups) RemoveMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error {
	// Prevent removing the last administrator.
	if groupID == AdminGroupID {
		count, err := p.CountMembers(ctx, groupID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM security_group_members WHERE group_id = $1 AND user_id = $2`,
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
	if groupID == AdminGroupID {
		return p.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (p *PostgresSecurityGroups) ListMembers(ctx context.Context, groupID SecurityGroupID) ([]*Member, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT u.id, u.email, u.display_name, u.is_admin, u.created_at, u.updated_at
		 FROM users u
		 INNER JOIN security_group_members sgm ON sgm.user_id = u.id
		 WHERE sgm.group_id = $1
		 ORDER BY u.email`,
		groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Email, &m.DisplayName, &m.IsAdmin, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		members = append(members, &m)
	}
	return members, rows.Err()
}

func (p *PostgresSecurityGroups) IsUserInGroup(ctx context.Context, userID uuid.UUID, groupID SecurityGroupID) (bool, error) {
	var exists bool
	err := p.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM security_group_members WHERE group_id = $1 AND user_id = $2)`,
		groupID, userID).Scan(&exists)
	return exists, err
}

func (p *PostgresSecurityGroups) CountMembers(ctx context.Context, groupID SecurityGroupID) (int, error) {
	var count int
	err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_group_members WHERE group_id = $1`,
		groupID).Scan(&count)
	return count, err
}

// syncIsAdmin keeps the users.is_admin boolean in sync with Administrators
// group membership. Called from Add/RemoveMember whenever the touched group is
// AdminGroupID. The coupling between the security_group_members and users
// tables is intentionally contained inside this adapter — both tables belong
// to the auth aggregate, so an inline UPDATE is preferred over a use-case-
// orchestrated transaction.
func (p *PostgresSecurityGroups) syncIsAdmin(ctx context.Context, userID uuid.UUID) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE users SET is_admin = EXISTS(
			SELECT 1 FROM security_group_members
			WHERE user_id = $1 AND group_id = $2
		), updated_at = NOW() WHERE id = $1`,
		userID, AdminGroupID)
	return err
}
