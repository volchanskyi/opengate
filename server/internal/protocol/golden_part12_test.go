package protocol

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The certificate fixtures match the constants in certificate_messages_test.rs.
var (
	goldenCSR    = []byte{0x30, 0x81, 0x9a, 0x02, 0x01, 0x00, 0xc5, 0x9e}
	goldenCert   = []byte{0x30, 0x82, 0x01, 0x4b, 0x30, 0x81, 0xf1, 0xa0, 0x03}
	goldenBundle = "-----BEGIN CERTIFICATE-----\nZ29sZGVuLWJ1bmRsZQ==\n-----END CERTIFICATE-----\n"
)

func TestGoldenControlCertificateSigningRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_certificate_signing_request.bin")

	assert.Equal(t, MsgCertificateSigningRequest, msg.Type)
	assert.Equal(t, goldenCSR, msg.CSR)
	assertControlSurvivesReencode(t, msg)
}

func TestGoldenControlCertificateInstalled(t *testing.T) {
	msg := decodeControlFrame(t, "control_certificate_installed.bin")

	assert.Equal(t, &ControlMessage{Type: MsgCertificateInstalled}, msg)
}

func TestGenerateCertificateReverseGoldens(t *testing.T) {
	dir := goldenWriteDir(t)
	codec := &Codec{}

	writeReverseControlFrame(t, dir, codec, "control_renew_certificate", &ControlMessage{
		Type:   MsgRenewCertificate,
		Reason: "authority-change",
	})
	writeReverseControlFrame(t, dir, codec, "control_renew_certificate_min", &ControlMessage{
		Type: MsgRenewCertificate,
	})
	writeReverseControlFrame(t, dir, codec, "control_certificate_issued", &ControlMessage{
		Type:     MsgCertificateIssued,
		Cert:     goldenCert,
		CABundle: goldenBundle,
	})
	writeReverseControlFrame(t, dir, codec, "control_certificate_refused", &ControlMessage{
		Type:       MsgCertificateRefused,
		Reason:     "rate-limited",
		RetryAfter: 3600,
	})
	writeReverseControlFrame(t, dir, codec, "control_certificate_refused_min", &ControlMessage{
		Type: MsgCertificateRefused,
	})
	writeReverseControlFrame(t, dir, codec, "control_trust_bundle", &ControlMessage{
		Type:     MsgTrustBundle,
		CABundle: goldenBundle,
	})

	if !shouldGenerateGolden() {
		for _, variant := range []string{
			"control_renew_certificate", "control_renew_certificate_min",
			"control_certificate_issued", "control_certificate_refused",
			"control_certificate_refused_min", "control_trust_bundle",
		} {
			name := "go_" + variant + ".bin"
			require.Equal(t, readGolden(t, name), readFile(t, dir, name),
				"%s drifted from what the encoder writes; regenerate with GENERATE_GOLDEN=1", name)
		}
	}
}

// readFile reads a file the generator wrote into dir.
func readFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return data
}
