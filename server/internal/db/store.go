// Package db provides the PostgreSQL store, migrations, and per-aggregate
// type aliases retained for backward compatibility while per-module
// repositories (audit, auth, device, notifications, session, update, amt)
// are wired through. The Store interface is retired: callers use the
// concrete *db.PostgresStore directly, and each module owns its own
// repository plus its own ErrXxxNotFound sentinel.
package db
