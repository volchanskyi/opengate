package rules

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed catalogue/*.yaml catalogue/catalogue.lock
var catalogueFS embed.FS

const (
	catalogueDir = "catalogue"
	lockPath     = "catalogue/catalogue.lock"
)

// Lock maps a definition's (id, version) key to the digest it was committed with.
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

// VerifyEmbeddedLock proves every shipped definition has a lock line; Embedded alone does not.
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

func packBody(data []byte, name string) ([]byte, error) {
	trimmed := bytes.TrimLeft(data, "\n")
	header := []byte("rules:\n")
	if !bytes.HasPrefix(trimmed, header) {
		return nil, fmt.Errorf("catalogue %s must begin with a rules: block", name)
	}
	body := bytes.TrimSuffix(trimmed[len(header):], []byte("\n"))
	return append(body, '\n'), nil
}

// Each lock line reads `<id> <version> <sha256>`; `#` comments and blank lines are ignored.
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
