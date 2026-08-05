// Package emdash holds no code. It exists for the test below, which reads
// every package's source and fails on an em dash in a string a user can be
// shown.
//
// The project writes user-facing copy without em dashes. Three guards enforce
// it, at different distances from the reader:
//
//   - internal/panelui's TestNoEmDashInUserFacingText renders every screen and
//     reads the finished page, which is the closest to what a user actually
//     sees and the only one that catches copy assembled from parts.
//   - This one reads source, so it covers text no fixture happens to render:
//     an error message only produced when a particular call fails, a message
//     box only shown when a runtime is missing.
//   - core/emdash_test.go used to do this for core alone. It was the whole
//     coverage of the source side, and it is why "couldn't archive %s — ..."
//     was caught. Everything outside core had nothing, and by the time this
//     package was written there were sixteen live em dashes out there in
//     platform/ and both hosts, none of them visible to any guard.
//
// The rendering guard cannot replace this one: it only ever sees the error
// text a fixture chose to hand it. This one cannot replace the rendering
// guard: it sees literals, not the sentences they are assembled into.
package emdash

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

// roots are scanned recursively from the repository root. Test files are
// skipped: a fixture may legitimately contain an em dash in order to prove a
// guard catches one, and this file itself is the obvious example.
var roots = []string{"core", "platform", "cmd", "internal"}

func TestNoEmDashInUserFacingStrings(t *testing.T) {
	checked := 0
	for _, root := range roots {
		dir := filepath.Join("..", "..", root)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			checked++
			check(t, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	// A path typo or a moved directory would otherwise leave this passing
	// while reading nothing, which is the failure mode a guard must not have.
	if checked < 20 {
		t.Fatalf("only %d source files scanned: the walk is not reading what it thinks it is", checked)
	}
}

func check(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	// ParseFile ignores build tags on purpose: the Windows-only files hold
	// user-facing copy too, and they must be checked from the macOS and Linux
	// legs rather than only from the one leg that compiles them.
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
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

// isUserFacingStringCall reports whether fun builds or delivers a string a user
// may read.
//
// Comments and log.Printf are deliberately absent: comments are shown to
// nobody, and log lines are read by whoever is debugging, where a dash costs
// nothing. Flagging either is what forced pointless code-comment rewording in
// an earlier round of this work.
//
// The bare identifiers are this project's own message helpers. They take copy
// straight to a dialog or a status line without any fmt call in between, which
// is how several of the sixteen escaped: the string was a plain literal
// argument, not something built by fmt.
func isUserFacingStringCall(fun ast.Expr) bool {
	if id, ok := fun.(*ast.Ident); ok {
		switch id.Name {
		case "notify", "setStatus", "askText", "askConfirm",
			"panelSetStatus", "setBusyStatus", "panelSetBusy":
			return true
		}
		return false
	}
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
