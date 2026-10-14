package cert

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errNoEntropy = errors.New("entropy unavailable")

// failingReader stands in for a random source that cannot be read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errNoEntropy }

// withoutRandomness points crypto/rand's shared reader at a failing source until the test ends.
func withoutRandomness(t *testing.T) {
	t.Helper()
	saved := rand.Reader
	rand.Reader = failingReader{}
	t.Cleanup(func() { rand.Reader = saved })
}

func TestSigningRefusesWhenNoSerialCanBeDrawn(t *testing.T) {
	m := newTestManager(t)
	csrDER := makeCSR(t, "device-001")
	withoutRandomness(t)

	tests := []struct {
		name string
		sign func() error
	}{
		{"agent", func() error { _, err := m.SignAgent("device-001", "test-host"); return err }},
		{"agent CSR", func() error { _, err := m.SignAgentCSR(csrDER); return err }},
		{"server", func() error { _, err := m.SignServer(); return err }},
		{"MPS", func() error { _, err := m.SignMPS(); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sign()
			require.ErrorIs(t, err, errNoEntropy)
			require.ErrorContains(t, err, "generate serial")
		})
	}
}
