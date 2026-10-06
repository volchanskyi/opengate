package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// SecurityGroupID uniquely identifies a security group.
type SecurityGroupID = uuid.UUID

// AdminGroupID is the UUID of the built-in Administrators group.
// Membership in it sets users.is_admin.
var AdminGroupID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// ErrSecurityGroupNotFound is returned when an operation targets a missing security group.
var ErrSecurityGroupNotFound = errors.New("security group not found")

// ErrSystemGroup is returned when a delete targets a system group.
var ErrSystemGroup = errors.New("cannot delete system group")

// ErrLastAdmin is returned when a removal would empty the Administrators group.
var ErrLastAdmin = errors.New("cannot remove last administrator")

// ErrMemberNotFound is returned when a removal targets a missing membership.
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

// Member is a user as listed in a security group; it carries no password hash.
type Member struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SecurityGroupRepository is the persistence port for security groups and their memberships.
// AddMember and RemoveMember on the Administrators group also update users.is_admin.
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
