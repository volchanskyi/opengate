package dbtx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// ErrTenantRequired is returned when a tenant-scoped query runs without scope.
var ErrTenantRequired = errors.New("tenant scope required")

// Scoped runs fn inside a transaction with tenant RLS GUCs set locally.
func Scoped(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tenant, ok := TenantFromContext(ctx)
	if !ok {
		return ErrTenantRequired
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tenant tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // harmless after Commit

	if err := SetLocal(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant tx: %w", err)
	}
	return nil
}

// SetLocal sets tenant GUCs for the lifetime of tx.
func SetLocal(ctx context.Context, tx *sql.Tx, tenant Tenant) error {
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('app.current_org', $1, true), set_config('app.is_admin', $2, true)`,
		tenant.OrgID.String(), strconv.FormatBool(tenant.IsAdmin)); err != nil {
		return fmt.Errorf("set tenant scope: %w", err)
	}
	return nil
}
