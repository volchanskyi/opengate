package cert

import (
	"testing"
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

func BenchmarkManager_SignAgent(b *testing.B) {
	runSigning(b, func(m *Manager) error {
		_, err := m.SignAgent("device-001", "test-host")
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
