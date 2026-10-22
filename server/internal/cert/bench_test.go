package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"github.com/google/uuid"
)

func benchManager(b *testing.B) *Manager {
	b.Helper()
	mgr, err := NewManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	return mgr
}

func runSigning(b *testing.B, sign func(*Manager) error) {
	b.Helper()
	mgr := benchManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := sign(mgr); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManager_IssueDevice(b *testing.B) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		b.Fatal(err)
	}
	csr, err := ParseDeviceRequest(der)
	if err != nil {
		b.Fatal(err)
	}
	id := uuid.New()
	runSigning(b, func(m *Manager) error {
		_, err := m.IssueDevice(csr, id)
		return err
	})
}

func BenchmarkManager_SignServer(b *testing.B) {
	runSigning(b, func(m *Manager) error {
		_, err := m.SignServer()
		return err
	})
}

func BenchmarkNewManager_Generate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewManager(b.TempDir()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewManager_Load(b *testing.B) {
	dir := b.TempDir()
	if _, err := NewManager(dir); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewManager(dir); err != nil {
			b.Fatalf("iteration %d: %v", i, err)
		}
	}
}
