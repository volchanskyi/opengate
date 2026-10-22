package authority

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// Adopt copies the master key from the secret it was kept in beside the database passwords,
// once: an existing master key is never replaced.
func Adopt(current, legacy Files) (Files, bool, error) {
	if len(current[cert.IssuerKeyFile]) > 0 {
		return current, false, nil
	}
	if len(legacy[cert.IssuerKeyFile]) == 0 || len(legacy[cert.IssuerCertFile]) == 0 {
		return nil, false, ErrNoIssuer
	}
	out := copyFiles(current)
	out[cert.IssuerCertFile], out[cert.IssuerKeyFile] = legacy[cert.IssuerCertFile], legacy[cert.IssuerKeyFile]
	return out, true, nil
}

// Stage copies the role files a secret volume holds into the folder the server reads, owner-only,
// since a secret volume's files are readable by the pod's group.
func Stage(from, to string) error {
	src, err := os.OpenRoot(from)
	if err != nil {
		return fmt.Errorf("open %s: %w", from, err)
	}
	defer src.Close()
	dst, err := os.OpenRoot(to)
	if err != nil {
		return fmt.Errorf("open %s: %w", to, err)
	}
	defer dst.Close()
	entries, err := fs.ReadDir(src.FS(), ".")
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	for _, e := range entries {
		// A secret volume keeps its versions in hidden folders and links each file to the current.
		if strings.HasPrefix(e.Name(), "..") || e.IsDir() {
			continue
		}
		data, err := src.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := dst.WriteFile(e.Name()+".tmp", data, 0o400); err != nil {
			return fmt.Errorf("stage %s: %w", e.Name(), err)
		}
		if err := dst.Rename(e.Name()+".tmp", e.Name()); err != nil {
			return fmt.Errorf("stage %s: %w", e.Name(), err)
		}
	}
	return nil
}
