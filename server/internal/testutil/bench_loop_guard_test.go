package testutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hollowLoops returns every `for b.Loop()` whose body only calls a function value; Go keeps alive
// only what the loop body writes literally, so such a loop can time compiled-away work.
func hollowLoops(fset *token.FileSet, file *ast.File) []string {
	var hollow []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		values := variablesOf(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			loop, ok := n.(*ast.ForStmt)
			if ok && isBLoop(loop.Cond) && onlyCallsOneOf(loop.Body, values) {
				hollow = append(hollow, fset.Position(loop.Pos()).String())
			}
			return true
		})
	}
	return hollow
}

// variablesOf names fn's parameters and the variables its body declares; a call through one of
// them calls a function value.
func variablesOf(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			names[name.Name] = true
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		for _, name := range declaredBy(n) {
			names[name] = true
		}
		return true
	})
	return names
}

// declaredBy names the variables one node declares: the left of a := or the names of a var.
func declaredBy(n ast.Node) []string {
	var names []string
	switch v := n.(type) {
	case *ast.AssignStmt:
		if v.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range v.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok {
				names = append(names, ident.Name)
			}
		}
	case *ast.ValueSpec:
		for _, name := range v.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func isBLoop(cond ast.Expr) bool {
	call, ok := cond.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Loop"
}

func onlyCallsOneOf(body *ast.BlockStmt, values map[string]bool) bool {
	if len(body.List) != 1 {
		return false
	}
	stmt, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && values[ident.Name]
}

func TestHollowLoopsFindsABenchmarkThatTimesAFunctionValue(t *testing.T) {
	const source = `package p
import "testing"
func runBench(b *testing.B, op func()) {
	for b.Loop() {
		op()
	}
}
func BenchmarkLiteral(b *testing.B) {
	for b.Loop() {
		encode()
	}
}
func encode() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p_test.go", source, parser.SkipObjectResolution)
	require.NoError(t, err)

	hollow := hollowLoops(fset, file)
	require.Len(t, hollow, 1)
	assert.Equal(t, "p_test.go:4:2", hollow[0])
}

func TestNoBenchmarkLoopTimesOnlyAFunctionValue(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(here), "..", "..")

	fset := token.NewFileSet()
	read, loops := 0, 0
	var hollow []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		read++
		ast.Inspect(file, func(n ast.Node) bool {
			if loop, ok := n.(*ast.ForStmt); ok && isBLoop(loop.Cond) {
				loops++
			}
			return true
		})
		hollow = append(hollow, hollowLoops(fset, file)...)
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, loops, "the sweep read %d test files and found no benchmark loop", read)
	assert.Empty(t, hollow, "write each benchmark's operation inside its `for b.Loop()`")
}
