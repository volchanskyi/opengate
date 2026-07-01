package auth_test

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"time"
)

type observerCall struct {
	op       string
	duration time.Duration
	ok       bool
}

func (f *fakeObserver) Observe(op string, d time.Duration, ok bool) {
	f.calls = append(f.calls, observerCall{op: op, duration: d, ok: ok})
}

// memSG is an in-memory SecurityGroupRepository for testing the Instrumented
// decorator without needing Postgres.
type memSG struct {
	failEvery bool
}

func (m *memSG) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}

func (m *memSG) Create(_ context.Context, _ *auth.SecurityGroup) error { return m.maybeFail() }

func (m *memSG) Get(_ context.Context, _ auth.SecurityGroupID) (*auth.SecurityGroup, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return &auth.SecurityGroup{ID: auth.AdminGroupID, Name: "Administrators"}, nil
}

func (m *memSG) List(_ context.Context) ([]*auth.SecurityGroup, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (m *memSG) Delete(_ context.Context, _ auth.SecurityGroupID) error { return m.maybeFail() }

func (m *memSG) AddMember(_ context.Context, _ auth.SecurityGroupID, _ uuid.UUID) error {
	return m.maybeFail()
}

func (m *memSG) RemoveMember(_ context.Context, _ auth.SecurityGroupID, _ uuid.UUID) error {
	return m.maybeFail()
}

func (m *memSG) ListMembers(_ context.Context, _ auth.SecurityGroupID) ([]*auth.Member, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (m *memSG) IsUserInGroup(_ context.Context, _ uuid.UUID, _ auth.SecurityGroupID) (bool, error) {
	if err := m.maybeFail(); err != nil {
		return false, err
	}
	return true, nil
}

func (m *memSG) CountMembers(_ context.Context, _ auth.SecurityGroupID) (int, error) {
	if err := m.maybeFail(); err != nil {
		return 0, err
	}
	return 0, nil
}
