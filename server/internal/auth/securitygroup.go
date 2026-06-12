package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// SecurityGroupID uniquely identifies a security group.
type SecurityGroupID = uuid.UUID

// AdminGroupID is the well-known UUID for the built-in Administrators group.
// Membership in this group is mirrored to the users.is_admin column by the
// repository's Add/RemoveMember implementations.
var AdminGroupID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// ErrSecurityGroupNotFound is returned when a Get / Delete / member operation
// targets a security group that does not exist.
var ErrSecurityGroupNotFound = errors.New("security group not found")

// ErrSystemGroup is returned when attempting to delete a system group.
var ErrSystemGroup = errors.New("cannot delete system group")

// ErrLastAdmin is returned when attempting to remove the last member of the
// Administrators group.
var ErrLastAdmin = errors.New("cannot remove last administrator")

// ErrMemberNotFound is returned when removing a non-existent membership.
var ErrMemberNotFound = errors.New("member not found")

// SecurityGroup is a named permission group.
type SecurityGroup struct {
	ID          SecurityGroupID `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	IsSystem    bool            `json:"is_system"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Member is a user as seen from a security group's perspective. The password
// hash is intentionally omitted: callers list members for display and authz,
// never to re-verify credentials.
type Member struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SecurityGroupRepository is the outbound persistence port for security groups
// and their memberships. The interface lives with the consuming
// module (auth); the Postgres implementation lives alongside in this package.
//
// AddMember / RemoveMember for the Administrators group atomically synchronize
// the users.is_admin column — that coupling is contained inside the adapter
// rather than leaking into a use-case orchestrator.
type SecurityGroupRepository interface {
	Create(ctx context.Context, g *SecurityGroup) error
	Get(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error)
	List(ctx context.Context) ([]*SecurityGroup, error)
	Delete(ctx context.Context, id SecurityGroupID) error
	AddMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error
	RemoveMember(ctx context.Context, groupID SecurityGroupID, userID uuid.UUID) error
	ListMembers(ctx context.Context, groupID SecurityGroupID) ([]*Member, error)
	IsUserInGroup(ctx context.Context, userID uuid.UUID, groupID SecurityGroupID) (bool, error)
	CountMembers(ctx context.Context, groupID SecurityGroupID) (int, error)
}
