package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The test is a simple parse: it does not follow a status through a function call or a
// slices.Contains / strings.EqualFold argument, nor through a "var x = t.Status" declaration.

// kindConsts - the kind constants of package config (spec 0.9.0 section 6).
var kindConsts = map[string]bool{"Idea": true, "Open": true, "InProgress": true, "Blocked": true, "Closed": true}

// guardSkip - the packages that may compare a status: the owners, and tui until slice 4.
var guardSkip = []string{
	"internal/config", "internal/core",
	"internal/tui", // slice 4
}

func TestS26_NoStatusComparedOutsideConfigAndCore(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found, err := offences(path)
		for _, o := range found {
			bad = append(bad, rel+": "+o)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Errorf("ask the config for the kind (KindOf) instead:\n%s", strings.Join(bad, "\n"))
	}
}

func isStatus(e ast.Expr, vars map[string]bool) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "Status"
	case *ast.Ident:
		return vars[x.Name]
	}
	return false
}

// isFixed - a string literal or a kind constant of config.
func isFixed(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == "config" && kindConsts[x.Sel.Name]
	}
	return false
}

// skipDir: hidden folders (worktrees) and the packages of guardSkip.
func skipDir(rel, name string) bool {
	return rel != "." && strings.HasPrefix(name, ".") || slices.Contains(guardSkip, rel)
}

// offences lists the comparisons of a task status with a literal or a kind constant, and the
// switches on a status, in one file. A local variable set from .Status counts as a status.
// A kind constant is fine as a kind: against the result of KindOf, or as an argument.
func offences(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}
	vars := statusVars(f)
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SwitchStmt:
			if x.Tag != nil && isStatus(x.Tag, vars) {
				out = append(out, "switch on a status")
			}
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) &&
				(isStatus(x.X, vars) && isFixed(x.Y) || isStatus(x.Y, vars) && isFixed(x.X)) {
				out = append(out, "a status compared with a literal or a kind constant")
			}
		}
		return true
	})
	return out, nil
}

// statusVars - the names of the local variables set from .Status.
func statusVars(f *ast.File) map[string]bool {
	vars := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok || len(a.Lhs) != len(a.Rhs) {
			return true
		}
		for i, r := range a.Rhs {
			if id, ok := a.Lhs[i].(*ast.Ident); ok && isStatus(r, nil) {
				vars[id.Name] = true
			}
		}
		return true
	})
	return vars
}
