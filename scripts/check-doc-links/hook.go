package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// errOutsideRepoRoot marks a hook-supplied file_path that resolves outside the
// repository root. Such a file is not a repo doc, so hook mode skips it rather
// than treating it as a hard error.
var errOutsideRepoRoot = errors.New("is outside repository root")

type edit struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// hookEnvelope contains the Claude Code write proposal inspected by hook mode.
type hookEnvelope struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath  string `json:"file_path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Edits     []edit `json:"edits"`
	} `json:"tool_input"`
}

func (c *checker) readHookOverlay(reader io.Reader) (string, []byte, bool, error) {
	envelope, err := decodeHookEnvelope(reader)
	if err != nil {
		return "", nil, false, err
	}
	if !supportedHookTool(envelope.ToolName) {
		return "", nil, false, nil
	}

	relativePath, err := c.relativePath(envelope.ToolInput.FilePath)
	if err != nil {
		if errors.Is(err, errOutsideRepoRoot) {
			// The edited file lives outside the repository (e.g. the global
			// ~/.claude/projects/.../memory tree matches the .claude/*.md hook
			// scope but is not a repo doc). Out of scope: nothing to validate.
			return "", nil, false, nil
		}
		return "", nil, false, err
	}
	if !inScope(relativePath) {
		return "", nil, false, nil
	}

	content, err := c.proposedContent(envelope, relativePath)
	if err != nil {
		return "", nil, false, fmt.Errorf("apply proposed %s to %s: %w", envelope.ToolName, relativePath, err)
	}
	return relativePath, content, true, nil
}

func decodeHookEnvelope(reader io.Reader) (hookEnvelope, error) {
	var envelope hookEnvelope
	if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
		return hookEnvelope{}, fmt.Errorf("decode hook input: %w", err)
	}
	return envelope, nil
}

func supportedHookTool(toolName string) bool {
	return toolName == "Write" || toolName == "Edit" || toolName == "MultiEdit"
}

func (c *checker) proposedContent(envelope hookEnvelope, relativePath string) ([]byte, error) {
	switch envelope.ToolName {
	case "Write":
		return []byte(envelope.ToolInput.Content), nil
	case "Edit":
		return c.applyEdits(relativePath, []edit{{
			OldString: envelope.ToolInput.OldString,
			NewString: envelope.ToolInput.NewString,
		}})
	case "MultiEdit":
		return c.applyEdits(relativePath, envelope.ToolInput.Edits)
	}
	return nil, fmt.Errorf("unsupported hook tool %q", envelope.ToolName)
}

func (c *checker) applyEdits(relativePath string, edits []edit) ([]byte, error) {
	content, err := c.readExisting(relativePath)
	if err != nil {
		return nil, err
	}
	for _, proposedEdit := range edits {
		content, err = applyEdit(content, proposedEdit)
		if err != nil {
			return nil, err
		}
	}
	return content, nil
}

func (c *checker) relativePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("hook input has no file_path")
	}

	absolutePath := path
	if !filepath.IsAbs(absolutePath) {
		absolutePath = filepath.Join(c.root, absolutePath)
	}
	relativePath, err := filepath.Rel(c.root, filepath.Clean(absolutePath))
	if err != nil {
		return "", fmt.Errorf("make %q repository-relative: %w", path, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q %w", path, errOutsideRepoRoot)
	}
	return filepath.ToSlash(relativePath), nil
}

func (c *checker) readExisting(relativePath string) ([]byte, error) {
	if content, ok := c.overlays[relativePath]; ok {
		return append([]byte(nil), content...), nil
	}
	content, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(relativePath)))
	if err != nil {
		return nil, err
	}
	return content, nil
}

func applyEdit(content []byte, proposedEdit edit) ([]byte, error) {
	if proposedEdit.OldString == "" {
		return nil, errors.New("old_string is empty")
	}
	if !bytes.Contains(content, []byte(proposedEdit.OldString)) {
		return nil, errors.New("old_string was not found")
	}
	if proposedEdit.ReplaceAll {
		return bytes.ReplaceAll(content, []byte(proposedEdit.OldString), []byte(proposedEdit.NewString)), nil
	}
	return bytes.Replace(content, []byte(proposedEdit.OldString), []byte(proposedEdit.NewString), 1), nil
}
