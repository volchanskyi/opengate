package rules

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// The shipped pack and the digests that make a published definition immutable.

//go:embed catalogue/*.yaml catalogue/catalogue.lock
var catalogueFS embed.FS

const (
	// catalogueDir holds the shipped pack and its lock.
	catalogueDir = "catalogue"
	// lockPath is the committed digest of every shipped (rule_id, version).
	lockPath = "catalogue/catalogue.lock"
)

// Lock maps a definition's (id, version) key to the digest it was committed
// with. It is what makes immutability an assertion rather than a convention.
type Lock map[string]string

// DigestCatalogue returns the lock a pack would be committed with.
func DigestCatalogue(data []byte) (Lock, error) {
	defs, err := parseDefinitions(data)
	if err != nil {
		return nil, err
	}
	lock := make(Lock, len(defs))
	for _, def := range defs {
		digest, err := def.Digest()
		if err != nil {
			return nil, err
		}
		lock[def.Key()] = digest
	}
	return lock, nil
}

// Embedded returns the shipped catalogue, validated against its committed lock.
func Embedded() (*Catalogue, error) {
	data, err := embeddedPack()
	if err != nil {
		return nil, err
	}
	lock, err := embeddedLock()
	if err != nil {
		return nil, err
	}
	return LoadCatalogue(data, lock)
}

// VerifyEmbeddedLock additionally proves every shipped definition is actually
// locked. Embedded refuses a definition that drifted from its digest, but a rule
// with no lock line at all would slip past it, so the gate is completed here.
func VerifyEmbeddedLock() error {
	cat, err := Embedded()
	if err != nil {
		return err
	}
	lock, err := embeddedLock()
	if err != nil {
		return err
	}
	for _, def := range cat.All() {
		if _, ok := lock[def.Key()]; !ok {
			return fmt.Errorf("rule %s has no line in %s", def.Key(), lockPath)
		}
	}
	return nil
}

// embeddedPack concatenates every YAML file in the pack directory.
func embeddedPack() ([]byte, error) {
	entries, err := catalogueFS.ReadDir(catalogueDir)
	if err != nil {
		return nil, fmt.Errorf("read catalogue: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yaml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	var merged bytes.Buffer
	merged.WriteString("rules:\n")
	for _, name := range names {
		data, err := catalogueFS.ReadFile(catalogueDir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("read catalogue %s: %w", name, err)
		}
		body, err := packBody(data, name)
		if err != nil {
			return nil, err
		}
		merged.Write(body)
	}
	return merged.Bytes(), nil
}

// packBody strips a pack file's own `rules:` header so several files merge into
// one document.
func packBody(data []byte, name string) ([]byte, error) {
	trimmed := bytes.TrimLeft(data, "\n")
	header := []byte("rules:\n")
	if !bytes.HasPrefix(trimmed, header) {
		return nil, fmt.Errorf("catalogue %s must begin with a rules: block", name)
	}
	body := bytes.TrimSuffix(trimmed[len(header):], []byte("\n"))
	return append(body, '\n'), nil
}

// embeddedLock parses the committed digests: `<id> <version> <sha256>` a line,
// with `#` comments and blank lines ignored.
func embeddedLock() (Lock, error) {
	data, err := catalogueFS.ReadFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", lockPath, err)
	}
	lock := make(Lock)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s line %d: want `<id> <version> <sha256>`", lockPath, line)
		}
		lock[fields[0]+"@"+fields[1]] = fields[2]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", lockPath, err)
	}
	return lock, nil
}
