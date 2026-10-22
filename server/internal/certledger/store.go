// Package certledger records every certificate the server issues and answers, from memory, whether
// a presented one was issued here, is current, or was superseded.
package certledger

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Source is why a certificate was issued.
type Source string

const (
	// SourceEnroll is a certificate issued at enrolment.
	SourceEnroll Source = "enroll"
	// SourceRenew is a certificate issued to renew an existing one.
	SourceRenew Source = "renew"
)

// Fingerprint is the SHA-256 of a certificate's DER.
type Fingerprint [32]byte

// FingerprintOf hashes a certificate's DER.
func FingerprintOf(der []byte) Fingerprint {
	return sha256.Sum256(der)
}

// Entry is what admission reads about one certificate.
type Entry struct {
	Fingerprint Fingerprint
	DeviceID    uuid.UUID
	IssuerHash  [48]byte
	NotBefore   time.Time
	NotAfter    time.Time
	Source      Source
	IssuedAt    time.Time
	// Seen marks a certificate installed or presented, which makes it current and supersedes
	// every earlier one for its device.
	Seen bool
}

// later orders a device's certificates by validity start, then by issue time.
func (e *Entry) later(other *Entry) bool {
	if !e.NotBefore.Equal(other.NotBefore) {
		return e.NotBefore.After(other.NotBefore)
	}
	return e.IssuedAt.After(other.IssuedAt)
}

// Store is the ledger table behind an index held in memory, warmed at start-up and updated on
// every issuance, so admission never waits on the database.
type Store struct {
	db     *sql.DB
	logger *slog.Logger

	mu       sync.RWMutex
	byPrint  map[Fingerprint]*Entry
	byDevice map[uuid.UUID][]*Entry
}

// New returns a ledger over db; Warm loads what it already holds.
func New(db *sql.DB, logger *slog.Logger) *Store {
	return &Store{
		db:       db,
		logger:   logger,
		byPrint:  map[Fingerprint]*Entry{},
		byDevice: map[uuid.UUID][]*Entry{},
	}
}

// Warm loads every recorded certificate into the index.
func (s *Store) Warm(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT fingerprint, device_id, issuer_hash, not_before, not_after, source, issued_at,
		        first_seen_at IS NOT NULL
		   FROM device_certificates`)
	if err != nil {
		return fmt.Errorf("warm certificate ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()

	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var e Entry
		var fp, issuer []byte
		if err := rows.Scan(&fp, &e.DeviceID, &issuer, &e.NotBefore, &e.NotAfter, &e.Source,
			&e.IssuedAt, &e.Seen); err != nil {
			return fmt.Errorf("scan certificate ledger: %w", err)
		}
		copy(e.Fingerprint[:], fp)
		copy(e.IssuerHash[:], issuer)
		s.indexLocked(&e)
	}
	return rows.Err()
}

func (s *Store) indexLocked(e *Entry) {
	s.byPrint[e.Fingerprint] = e
	s.byDevice[e.DeviceID] = append(s.byDevice[e.DeviceID], e)
}

// Record inserts a certificate issuerHash's master key issued, before it is sent; an insert that
// fails leaves the index unchanged, so the certificate must not be sent.
func (s *Store) Record(ctx context.Context, leaf *x509.Certificate, issuerHash [48]byte, source Source, issuedAt time.Time) (Entry, error) {
	deviceID, err := uuid.Parse(leaf.Subject.CommonName)
	if err != nil {
		return Entry{}, fmt.Errorf("record certificate: common name %q: %w", leaf.Subject.CommonName, err)
	}
	e := Entry{
		Fingerprint: FingerprintOf(leaf.Raw),
		DeviceID:    deviceID,
		IssuerHash:  issuerHash,
		NotBefore:   leaf.NotBefore,
		NotAfter:    leaf.NotAfter,
		Source:      source,
		IssuedAt:    issuedAt,
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO device_certificates
		   (fingerprint, device_id, issuer_hash, serial, not_before, not_after, source, issued_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (fingerprint) DO NOTHING`,
		e.Fingerprint[:], e.DeviceID, e.IssuerHash[:], leaf.SerialNumber.String(),
		e.NotBefore, e.NotAfter, string(e.Source), e.IssuedAt); err != nil {
		return Entry{}, fmt.Errorf("record certificate: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byPrint[e.Fingerprint]; ok {
		return *existing, nil
	}
	s.indexLocked(&e)
	return e, nil
}

// Lookup returns the entry for a fingerprint, from memory.
func (s *Store) Lookup(fp Fingerprint) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byPrint[fp]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Superseded reports whether a later certificate for the same device has been seen.
func (s *Store) Superseded(fp Fingerprint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byPrint[fp]
	return ok && s.supersededLocked(e)
}

func (s *Store) supersededLocked(e *Entry) bool {
	for _, other := range s.byDevice[e.DeviceID] {
		if other.Seen && other.later(e) {
			return true
		}
	}
	return false
}

// HasDevice reports whether any certificate was ever issued for the device number.
func (s *Store) HasDevice(deviceID uuid.UUID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byDevice[deviceID]) > 0
}

// MarkSeen makes a presented certificate current in memory at once and on disk behind the
// caller, and returns the certificates it superseded.
func (s *Store) MarkSeen(fp Fingerprint, at time.Time) []Fingerprint {
	superseded, changed := s.see(fp)
	if changed {
		go s.persistSeen(fp, at)
	}
	return superseded
}

// MarkInstalled makes a renewed certificate current once the machine reports it saved, and
// returns the certificates it superseded.
func (s *Store) MarkInstalled(ctx context.Context, fp Fingerprint, at time.Time) ([]Fingerprint, error) {
	superseded, changed := s.see(fp)
	if !changed {
		return superseded, nil
	}
	return superseded, s.writeSeen(ctx, fp, at)
}

// see marks fp seen and lists the device's other certificates that are now superseded.
func (s *Store) see(fp Fingerprint) ([]Fingerprint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byPrint[fp]
	if !ok || e.Seen {
		return nil, false
	}
	e.Seen = true
	var superseded []Fingerprint
	for _, other := range s.byDevice[e.DeviceID] {
		if other != e && e.later(other) {
			superseded = append(superseded, other.Fingerprint)
		}
	}
	return superseded, true
}

// persistSeenBudget bounds the write that follows an admission.
const persistSeenBudget = 30 * time.Second

func (s *Store) persistSeen(fp Fingerprint, at time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), persistSeenBudget)
	defer cancel()
	if err := s.writeSeen(ctx, fp, at); err != nil {
		s.logger.Warn("record first sighting of a certificate", "error", err, "fingerprint", fmt.Sprintf("%x", fp[:6]))
	}
}

func (s *Store) writeSeen(ctx context.Context, fp Fingerprint, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE device_certificates SET first_seen_at = $2
		  WHERE fingerprint = $1 AND first_seen_at IS NULL`, fp[:], at); err != nil {
		return fmt.Errorf("record certificate sighting: %w", err)
	}
	return nil
}
