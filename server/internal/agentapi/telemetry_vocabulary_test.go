package agentapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/metrics"
)

// The drop reasons and ingested message types are spelled where the connection
// records them, and published at zero from start-up by the metrics package. A
// reason added at a call site without a zero there is invisible to a rate until
// its second occurrence after every start, and the production drop-ratio rule
// reads nothing until something is dropped. So this reads every call site from
// the package's own source, the way a reviewer would, and holds the two lists
// equal.

// constStrings maps each string constant a directory's source declares to its
// value.
func constStrings(t *testing.T, dir string) map[string]string {
	t.Helper()
	values := map[string]string{}
	for _, file := range parseDir(t, dir) {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if i < len(spec.Values) {
					if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						value, err := strconv.Unquote(lit.Value)
						require.NoError(t, err)
						values[name.Name] = value
					}
				}
			}
			return true
		})
	}
	return values
}

// parseDir parses a directory's shipped source, its tests left out.
func parseDir(t *testing.T, dir string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	require.NotEmpty(t, files, "no source read in %s", dir)
	return files
}

// recordedValues returns, for each named method, the value every call outside
// the method itself passes at the given argument position. A value the source
// does not spell as a constant is a refusal: nothing could say what it is.
func recordedValues(t *testing.T, methods map[string]int) map[string][]string {
	t.Helper()
	local := constStrings(t, ".")
	protocolConsts := constStrings(t, filepath.Join("..", "protocol"))

	found := map[string][]string{}
	for _, file := range parseDir(t, ".") {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				position, ok := methods[sel.Sel.Name]
				if !ok || len(call.Args) <= position {
					return true
				}
				// The funnels pass their own parameter on to the counter.
				if _, isFunnel := methods[fn.Name.Name]; isFunnel {
					return true
				}
				var value string
				switch arg := call.Args[position].(type) {
				case *ast.BasicLit:
					unquoted, err := strconv.Unquote(arg.Value)
					require.NoError(t, err)
					value = unquoted
				case *ast.Ident:
					value, ok = local[arg.Name]
					require.True(t, ok, "%s: %s passes %s, which is not a string constant of this package",
						fn.Name.Name, sel.Sel.Name, arg.Name)
				case *ast.SelectorExpr:
					pkg, isPkg := arg.X.(*ast.Ident)
					require.True(t, isPkg && pkg.Name == "protocol", "%s: %s passes a value nothing can read",
						fn.Name.Name, sel.Sel.Name)
					value, ok = protocolConsts[arg.Sel.Name]
					require.True(t, ok, "%s: protocol.%s is not a string constant", fn.Name.Name, arg.Sel.Name)
				default:
					require.Failf(t, "a recorded value nothing can read",
						"%s: %s passes %T rather than a constant", fn.Name.Name, sel.Sel.Name, arg)
				}
				found[sel.Sel.Name] = append(found[sel.Sel.Name], value)
				return true
			})
		}
	}
	return found
}

func distinct(groups ...[]string) []string {
	seen := map[string]bool{}
	for _, group := range groups {
		for _, value := range group {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func TestEveryRecordedDropReasonIsPublishedAtZero(t *testing.T) {
	t.Parallel()

	found := recordedValues(t, map[string]int{"dropTelemetry": 0, "dropTelemetryN": 1})
	recorded := distinct(found["dropTelemetry"], found["dropTelemetryN"])
	require.NotEmpty(t, recorded, "the source was read and no drop was found in it")
	require.Equal(t, recorded, sorted(metrics.EdgeTelemetryDropReasons()),
		"every reason a connection records is published at zero, and nothing else is")
}

func TestEveryIngestedMessageTypeIsPublishedAtZero(t *testing.T) {
	t.Parallel()

	found := recordedValues(t, map[string]int{"acceptTelemetry": 0, "acceptedTelemetry": 0})
	recorded := distinct(found["acceptTelemetry"], found["acceptedTelemetry"])
	require.NotEmpty(t, recorded, "the source was read and no ingest was found in it")
	require.Equal(t, recorded, sorted(metrics.EdgeTelemetryIngestTypes()),
		"every message type a connection counts as ingested is published at zero, and nothing else is")
}
