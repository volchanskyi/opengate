package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type edit struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type hookEnvelope struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath   string `json:"file_path"`
		Content    string `json:"content"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
		Edits      []edit `json:"edits"`
	} `json:"tool_input"`
}

func applyEdits(content []byte, edits []edit) ([]byte, error) {
	for _, item := range edits {
		if item.OldString == "" {
			return nil, errors.New("old_string is empty")
		}
		if !bytes.Contains(content, []byte(item.OldString)) {
			return nil, errors.New("old_string was not found")
		}
		if item.ReplaceAll {
			content = bytes.ReplaceAll(content, []byte(item.OldString), []byte(item.NewString))
		} else {
			content = bytes.Replace(content, []byte(item.OldString), []byte(item.NewString), 1)
		}
	}
	return content, nil
}

func repositoryRoot(file string) (string, bool) {
	dir := filepath.Dir(filepath.Clean(file))
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func onlyNew(proposed, current []violation) []violation {
	counts := map[string]int{}
	for _, item := range current {
		counts[item.key()]++
	}
	var added []violation
	for _, item := range proposed {
		if counts[item.key()] > 0 {
			counts[item.key()]--
			continue
		}
		added = append(added, item)
	}
	return added
}

type hookPlan struct {
	root     string
	rel      string
	current  []byte
	proposed []byte
}

func readHookPlan(reader io.Reader, defaultRoot string) (hookPlan, bool, error) {
	var envelope hookEnvelope
	if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
		return hookPlan{}, false, err
	}
	switch envelope.ToolName {
	case "Write", "Edit", "MultiEdit":
	default:
		return hookPlan{}, false, nil
	}
	file := envelope.ToolInput.FilePath
	if file == "" {
		return hookPlan{}, false, errors.New("hook input has no file_path")
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(defaultRoot, file)
	}
	root, ok := repositoryRoot(file)
	if !ok {
		return hookPlan{}, false, nil
	}
	rel, err := filepath.Rel(root, filepath.Clean(file))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return hookPlan{}, false, nil
	}
	plan := hookPlan{root: root, rel: filepath.ToSlash(rel)}
	if existing, err := os.ReadFile(file); err == nil {
		plan.current = existing
	}
	switch envelope.ToolName {
	case "Write":
		plan.proposed = []byte(envelope.ToolInput.Content)
	case "Edit":
		plan.proposed, err = applyEdits(plan.current, []edit{{
			OldString:  envelope.ToolInput.OldString,
			NewString:  envelope.ToolInput.NewString,
			ReplaceAll: envelope.ToolInput.ReplaceAll,
		}})
	case "MultiEdit":
		plan.proposed, err = applyEdits(plan.current, envelope.ToolInput.Edits)
	}
	if err != nil {
		return hookPlan{}, false, nil
	}
	return plan, true, nil
}
