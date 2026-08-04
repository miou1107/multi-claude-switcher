package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoEmDashInErrorStrings pins the same project rule panelui's
// TestNoEmDashInUserFacingText pins, on the half of the copy that renderer
// cannot see.
//
// Every error core returns can end up on a screen: RemoveProfile's goes into
// RemovedVM.Err and is drawn verbatim by RenderRemoved, and the rest reach the
// panel's status line. The panelui guard only ever sees the error text a
// fixture chose to hand it, so an em dash written here is invisible to it
// unless somebody thought to copy the exact string across. That is precisely
// how ArchiveProfile's "couldn't archive %s — ..." shipped: the fixture used a
// shortened message with the em dash clause cut out.
//
// This reads the package's own source instead, so no fixture has to be kept in
// step. It checks the constructors that produce user-visible text
// (fmt.Errorf, errors.New, fmt.Sprintf) and leaves comments and log.Printf
// alone: comments are not shown to anyone, and log lines are read by whoever
// is debugging, where a dash costs nothing.
func TestNoEmDashInErrorStrings(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isUserFacingStringCall(call.Fun) {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(s, "—") {
					continue
				}
				t.Errorf("%s: em dash in a string a user can be shown: %q",
					fset.Position(lit.Pos()), s)
			}
			return true
		})
	}
}

// isUserFacingStringCall reports whether fun builds a string a user may read:
// fmt.Errorf, errors.New, fmt.Sprintf.
func isUserFacingStringCall(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name + "." + sel.Sel.Name {
	case "fmt.Errorf", "errors.New", "fmt.Sprintf":
		return true
	}
	return false
}
