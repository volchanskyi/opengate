package certledger

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// RenewalsSince counts the renewals issued for a device since a time, read from the table so the
// count survives a restart.
func (s *Store) RenewalsSince(ctx context.Context, deviceID uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM device_certificates
		  WHERE device_id = $1 AND source = 'renew' AND issued_at > $2`, deviceID, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("count renewals: %w", err)
	}
	return n, nil
}

// OldestRenewalSince is the issue time of the oldest renewal since a time, which sets when the
// next renewal is allowed.
func (s *Store) OldestRenewalSince(ctx context.Context, deviceID uuid.UUID, since time.Time) (time.Time, error) {
	var at sql.NullTime
	if err := s.db.QueryRowContext(ctx,
		`SELECT min(issued_at) FROM device_certificates
		  WHERE device_id = $1 AND source = 'renew' AND issued_at > $2`, deviceID, since).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("oldest renewal: %w", err)
	}
	return at.Time, nil
}

// Prune deletes certificates that can admit nothing: superseded, and expired past the grace.
func (s *Store) Prune(ctx context.Context, now time.Time, grace time.Duration) (int, error) {
	cutoff := now.Add(-grace)
	s.mu.RLock()
	var doomed []Fingerprint
	for _, e := range s.byPrint {
		if e.NotAfter.Before(cutoff) && s.supersededLocked(e) {
			doomed = append(doomed, e.Fingerprint)
		}
	}
	s.mu.RUnlock()
	if len(doomed) == 0 {
		return 0, nil
	}

	for _, fp := range doomed {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM device_certificates WHERE fingerprint = $1`, fp[:]); err != nil {
			return 0, fmt.Errorf("prune certificate ledger: %w", err)
		}
	}
	s.forget(doomed)
	return len(doomed), nil
}

// ForgetDevice drops a purged device's certificates from the index.
func (s *Store) ForgetDevice(deviceID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.byDevice[deviceID] {
		delete(s.byPrint, e.Fingerprint)
	}
	delete(s.byDevice, deviceID)
}

func (s *Store) forget(prints []Fingerprint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, fp := range prints {
		e, ok := s.byPrint[fp]
		if !ok {
			continue
		}
		delete(s.byPrint, fp)
		kept := s.byDevice[e.DeviceID][:0]
		for _, other := range s.byDevice[e.DeviceID] {
			if other != e {
				kept = append(kept, other)
			}
		}
		if len(kept) == 0 {
			delete(s.byDevice, e.DeviceID)
		} else {
			s.byDevice[e.DeviceID] = kept
		}
	}
}

// IssuerCount is how many machines hold their newest certificate from one master key.
type IssuerCount struct {
	IssuerHash [48]byte
	Devices    int
	// LatestNotAfter is when the last of those certificates expires.
	LatestNotAfter time.Time
}

// CensusByNewestIssuer counts machines by the master key of their newest certificate, offline
// ones included.
func (s *Store) CensusByNewestIssuer() []IssuerCount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[[48]byte]*IssuerCount{}
	for _, entries := range s.byDevice {
		newest := entries[0]
		for _, e := range entries[1:] {
			if e.later(newest) {
				newest = e
			}
		}
		c, ok := counts[newest.IssuerHash]
		if !ok {
			c = &IssuerCount{IssuerHash: newest.IssuerHash}
			counts[newest.IssuerHash] = c
		}
		c.Devices++
		if newest.NotAfter.After(c.LatestNotAfter) {
			c.LatestNotAfter = newest.NotAfter
		}
	}
	out := make([]IssuerCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Devices > out[j].Devices })
	return out
}
