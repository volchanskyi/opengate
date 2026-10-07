package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"
)

const (
	langGo         = "go"
	langGoMod      = "gomod"
	langRust       = "rust"
	langTS         = "typescript"
	langShell      = "shell"
	langYAML       = "yaml"
	langWorkflow   = "workflow"
	langHelm       = "helm"
	langTemplate   = "template"
	langMake       = "make"
	langDocker     = "docker"
	langTOML       = "toml"
	langHCL        = "hcl"
	langSQL        = "sql"
	langRego       = "rego"
	langPython     = "python"
	langProperties = "properties"
	langHash       = "hash"
	langCSS        = "css"
	langHTML       = "html"
)

var baseNames = map[string]string{
	"Makefile":       langMake,
	"GNUmakefile":    langMake,
	"Dockerfile":     langDocker,
	"go.mod":         langGoMod,
	"CODEOWNERS":     langHash,
	".gitignore":     langHash,
	".gitattributes": langHash,
	".dockerignore":  langHash,
	".helmignore":    langHash,
	".semgrepignore": langHash,
	".trivyignore":   langHash,
	".shellcheckrc":  langHash,
	".editorconfig":  langHash,
	".npmrc":         langHash,
	".env":           langHash,
}

var extensions = map[string]string{
	".go":         langGo,
	".rs":         langRust,
	".ts":         langTS,
	".tsx":        langTS,
	".mts":        langTS,
	".cts":        langTS,
	".js":         langTS,
	".jsx":        langTS,
	".mjs":        langTS,
	".cjs":        langTS,
	".json":       langTS,
	".sh":         langShell,
	".bash":       langShell,
	".yml":        langYAML,
	".yaml":       langYAML,
	".toml":       langTOML,
	".tf":         langHCL,
	".hcl":        langHCL,
	".tfvars":     langHCL,
	".tfbackend":  langHCL,
	".sql":        langSQL,
	".rego":       langRego,
	".py":         langPython,
	".properties": langProperties,
	".css":        langCSS,
	".html":       langHTML,
	".dockerfile": langDocker,
	".exceptions": langHash,
	".lock":       langHash,
	".tpl":        langTemplate,
}

func classify(rel string) (string, bool) {
	base := path.Base(rel)
	if lang, ok := classifyGitHub(rel, base); ok {
		return lang, true
	}
	if isHelmTemplate(rel) {
		switch path.Ext(base) {
		case ".yaml", ".yml":
			return langHelm, true
		case ".tpl", ".txt":
			return langTemplate, true
		}
	}
	name := strings.TrimSuffix(base, ".example")
	if lang, ok := baseNames[name]; ok {
		return lang, true
	}
	lang, ok := extensions[strings.ToLower(path.Ext(name))]
	return lang, ok
}

func classifyGitHub(rel, base string) (string, bool) {
	yaml := strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")
	if !yaml {
		return "", false
	}
	if strings.HasPrefix(rel, ".github/workflows/") {
		return langWorkflow, true
	}
	if strings.HasPrefix(rel, ".github/actions/") && strings.TrimSuffix(strings.TrimSuffix(base, ".yml"), ".yaml") == "action" {
		return langWorkflow, true
	}
	return "", false
}

func isHelmTemplate(rel string) bool {
	parts := strings.Split(rel, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "helm" && i+2 < len(parts) && parts[i+2] == "templates" {
			return true
		}
	}
	return false
}

type scopeEntry struct {
	pattern string
	reason  string
}

type scope struct {
	entries []scopeEntry
}

func loadScope(file string) (scope, error) {
	handle, err := os.Open(file)
	if err != nil {
		return scope{}, err
	}
	defer handle.Close()
	var loaded scope
	scanner := bufio.NewScanner(handle)
	number := 0
	for scanner.Scan() {
		number++
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		pattern, reason, found := strings.Cut(line, "\t")
		if !found || strings.TrimSpace(reason) == "" || strings.TrimSpace(pattern) == "" {
			return scope{}, fmt.Errorf("%s:%d: an entry is a pattern, a tab and its reason", file, number)
		}
		loaded.entries = append(loaded.entries, scopeEntry{pattern: strings.TrimSpace(pattern), reason: strings.TrimSpace(reason)})
	}
	return loaded, scanner.Err()
}

func (s scope) excluded(rel string) bool {
	for _, entry := range s.entries {
		if matchGlob(entry.pattern, rel) {
			return true
		}
	}
	return false
}

func (s scope) unmatched(files []string) []string {
	var stale []string
	for _, entry := range s.entries {
		found := false
		for _, file := range files {
			if matchGlob(entry.pattern, file) {
				found = true
				break
			}
		}
		if !found {
			stale = append(stale, entry.pattern)
		}
	}
	return stale
}

func matchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		if len(pattern) == 1 {
			return len(name) > 0
		}
		for skip := 0; skip <= len(name); skip++ {
			if matchSegments(pattern[1:], name[skip:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], name[0])
	if err != nil || !matched {
		return false
	}
	return matchSegments(pattern[1:], name[1:])
}
