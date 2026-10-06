package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const organizationFilterParam = "organization_id"

type specOperation struct {
	OperationID string `yaml:"operationId"`
	Parameters  []struct {
		Name string `yaml:"name"`
		In   string `yaml:"in"`
	} `yaml:"parameters"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema struct {
				Ref   string `yaml:"$ref"`
				Type  string `yaml:"type"`
				Items struct {
					Ref string `yaml:"$ref"`
				} `yaml:"items"`
			} `yaml:"schema"`
		} `yaml:"content"`
	} `yaml:"responses"`
}

type apiSpec struct {
	Paths map[string]map[string]specOperation `yaml:"paths"`
}

func TestFleetReadsOfferTheOrganizationFilter(t *testing.T) {
	t.Parallel()
	spec := loadAPISpec(t)

	checked := 0
	for path, methods := range spec.Paths {
		for method, op := range methods {
			if !answersWithAFleetRead(op) {
				continue
			}
			checked++
			assert.Truef(t, hasQueryParam(op, organizationFilterParam),
				"%s %s (%s) answers with a fleet read and must offer the %s query parameter",
				method, path, op.OperationID, organizationFilterParam)
		}
	}
	require.Positive(t, checked, "the contract found no fleet reads to check — the response-shape rule has drifted")
}

func answersWithAFleetRead(op specOperation) bool {
	ok, found := op.Responses["200"]
	if !found {
		return false
	}
	for _, body := range ok.Content {
		if body.Schema.Type == "array" && body.Schema.Items.Ref == "#/components/schemas/Device" {
			return true
		}
		if body.Schema.Ref == "#/components/schemas/DeviceSummary" {
			return true
		}
	}
	return false
}

func hasQueryParam(op specOperation, name string) bool {
	for _, p := range op.Parameters {
		if p.In == "query" && p.Name == name {
			return true
		}
	}
	return false
}

func loadAPISpec(t *testing.T) apiSpec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	require.NoError(t, err, "the contract reads the specification the handlers are generated from")
	var spec apiSpec
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	require.NotEmpty(t, spec.Paths)
	return spec
}
