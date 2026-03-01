package cert

import (
	"fmt"
	"testing"
)

func BenchmarkManager_SignAgent(b *testing.B) {
	mgr, err := NewManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := mgr.SignAgent("device-001", "test-host")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManager_SignServer(b *testing.B) {
	mgr, err := NewManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := mgr.SignServer()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewManager_Generate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dir := b.TempDir()
		_, err := NewManager(dir)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewManager_Load(b *testing.B) {
	dir := b.TempDir()
	_, err := NewManager(dir) // generate once
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewManager(dir) // load from disk
		if err != nil {
			b.Fatal(fmt.Sprintf("iteration %d: %v", i, err))
		}
	}
}
