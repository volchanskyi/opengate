package agentapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// packageASTFiles parses this package's non-test sources so the guard below can
// read the dispatch switch and the ingest call sites out of the tree itself.
func packageASTFiles(t *testing.T) []*ast.File {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoError(t, err)
		files = append(files, f)
	}
	require.NotEmpty(t, files)
	return files
}

// protocolIdent returns the Msg… identifier of a protocol.MsgX selector.
func protocolIdent(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "protocol" || !strings.HasPrefix(sel.Sel.Name, "Msg") {
		return ""
	}
	return sel.Sel.Name
}

// collectCaseIdents adds every protocol.Msg… identifier a switch's case clauses
// name to into.
func collectCaseIdents(sw *ast.SwitchStmt, into map[string]bool) {
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			if ident := protocolIdent(expr); ident != "" {
				into[ident] = true
			}
		}
	}
}

// funcDecls returns every top-level function declaration with the given name.
func funcDecls(files []*ast.File, name string) []*ast.FuncDecl {
	var found []*ast.FuncDecl
	for _, f := range files {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
				found = append(found, fn)
			}
		}
	}
	return found
}

// dispatchedIdents collects every control type handleControl's switch names.
func dispatchedIdents(t *testing.T, files []*ast.File) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, fn := range funcDecls(files, "handleControl") {
		ast.Inspect(fn, func(n ast.Node) bool {
			if sw, ok := n.(*ast.SwitchStmt); ok {
				collectCaseIdents(sw, found)
			}
			return true
		})
	}
	require.NotEmpty(t, found, "no dispatch cases found — the guard is not reading handleControl")
	return found
}

// ingestCallIdents collects every control type passed to the ingest counter.
func ingestCallIdents(t *testing.T, files []*ast.File) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "acceptTelemetry" && sel.Sel.Name != "acceptedTelemetry") {
				return true
			}
			if ident := protocolIdent(call.Args[0]); ident != "" {
				found[ident] = true
			}
			return true
		})
	}
	require.NotEmpty(t, found, "no ingest call sites found — the guard is not reading the handlers")
	return found
}

// TestCountedIngestTypesMatchDispatch keeps TestTelemetryAccountingInvariant from
// rotting: a control type added to the dispatch switch, or a handler newly wired
// to the ingest counter, fails here until it is classified and given a row in
// the accounting table.
func TestCountedIngestTypesMatchDispatch(t *testing.T) {
	t.Parallel()
	files := packageASTFiles(t)

	classified := map[string]bool{}
	for ident := range countedIngestByIdent {
		classified[ident] = true
	}
	for _, ident := range dispatchNonIngestIdents {
		require.False(t, classified[ident], "%s is classified twice", ident)
		classified[ident] = true
	}

	dispatched := dispatchedIdents(t, files)
	for ident := range dispatched {
		assert.True(t, classified[ident],
			"%s is dispatched but unclassified: add it to countedIngestByIdent (with an "+
				"accountingCases row) or to dispatchNonIngestIdents", ident)
	}
	for ident := range classified {
		assert.True(t, dispatched[ident], "%s is classified but no longer dispatched", ident)
	}

	counted := ingestCallIdents(t, files)
	for ident := range counted {
		_, ok := countedIngestByIdent[ident]
		assert.True(t, ok, "%s increments the ingest counter but has no ledger row", ident)
	}
	for ident := range countedIngestByIdent {
		assert.True(t, counted[ident], "%s no longer increments the ingest counter", ident)
	}

	covered := map[protocol.ControlMessageType]bool{}
	for _, tc := range accountingCases(time.Now().Unix()) {
		for _, msg := range tc.msgs {
			covered[msg.Type] = true
		}
	}
	for ident, msgType := range countedIngestByIdent {
		assert.True(t, covered[msgType], "%s has no row in accountingCases", ident)
	}
}
