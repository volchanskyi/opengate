package authority

import (
	"crypto/sha512"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// roleOrder is the order Inspect lists the files in, with the role each one holds.
var roleOrder = []struct{ file, role string }{
	{cert.IssuerCertFile, "issuer"},
	{cert.AcceptedFile, "accepted"},
	{cert.RenewOnlyFile, "renew-only"},
	{cert.VouchedOnlyFile, "vouched-only"},
	{cert.ChainFile, "chain"},
	{cert.MPSIssuerCertFile, "mps"},
	{NextCertFile, "next (staged)"},
	{NextCrossFile, "next cross (staged)"},
	{PreviousCertFile, "previous"},
}

// Inspect lists each role with its certificates' names, fingerprints and expiry.
func Inspect(files Files) ([]string, error) {
	var lines []string
	for _, r := range roleOrder {
		data := files[r.file]
		for len(data) > 0 {
			var block *pem.Block
			block, data = pem.Decode(data)
			if block == nil {
				break
			}
			c, err := cert.ParseCertificatePEM(pem.EncodeToMemory(block))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", r.file, err)
			}
			sum := sha512.Sum384(c.Raw)
			lines = append(lines, fmt.Sprintf("%-20s %-22q %x  expires %s",
				r.role, c.Subject.CommonName, sum[:6], c.NotAfter.Format(time.DateOnly)))
		}
	}
	return lines, nil
}
