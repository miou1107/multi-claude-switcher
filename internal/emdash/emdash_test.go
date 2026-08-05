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

// skipDirs are covered better elsewhere.
//
// internal/panelui is one package of HTML and JavaScript held in Go string
// literals, so to this guard a whole screen is a single literal thousands of
// characters long, comments and all. Reporting those would mean rewording code
// comments nobody is shown, which an earlier round of this work already did
// once for no benefit. TestNoEmDashInUserFacingText renders those screens and
// reads the finished page, which separates the copy from the comments properly
// and is the stronger check of the two.
var skipDirs = []string{filepath.Join("internal", "panelui")}

func TestNoEmDashInUserFacingStrings(t *testing.T) {
	checked := 0
	for _, root := range roots {
		dir := filepath.Join("..", "..", root)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				for _, skip := range skipDirs {
					if strings.HasSuffix(filepath.Clean(path), skip) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
	// Every string literal in the file, then the exemptions. Checking only the
	// arguments of a known list of calls was tried first and was the wrong
	// shape: it needs the list to be right, and the list was not. It named
	// askConfirm, which is a JavaScript function in the panel's page source and
	// has never existed in Go, while missing confirmDialog, which is the real
	// dialog helper on both platforms. It also could not see copy that reaches
	// a user without passing through a call at all — a bare `return "…"`, an
	// assignment, or two literals joined with + — which is how the message in
	// cmd/mcs-tray/autosync.go is written.
	//
	// Inverting it means the guard is wrong only when something is exempted,
	// which is visible in the list below, rather than when something is missing
	// from a list nobody rereads.
	exempt := exemptPositions(f, fset)
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if exempt[lit.Pos()] {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.Contains(s, "—") {
			return true
		}
		t.Errorf("%s: em dash in a string a user can be shown: %q",
			fset.Position(lit.Pos()), s)
		return true
	})
}

// exemptPositions marks the literals that are not copy.
//
// Only log calls. Comments never reach this function at all, since they are not
// literals, which is what makes the inverted check tolerable: an earlier round
// of this work forced pointless rewording of code comments, and nothing here
// can do that again. Log lines are read by whoever is debugging, where a dash
// costs nothing.
func exemptPositions(f *ast.File, fset *token.FileSet) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isLogCall(call.Fun) {
			return true
		}
		// The whole call, so a literal concatenated into a log message is
		// exempt too.
		ast.Inspect(call, func(m ast.Node) bool {
			if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				out[lit.Pos()] = true
			}
			return true
		})
		return true
	})
	return out
}

func isLogCall(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "log"
}
