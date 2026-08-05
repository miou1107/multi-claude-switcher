// Package hostparity holds no code. It exists for the test below, which reads
// both hosts' sources and fails when they stop offering the same set of panel
// actions.
//
// The panel is one HTML page, rendered by internal/panelui and shown by two
// hosts: cmd/mcs-menubar on macOS and cmd/mcs-tray on Windows. Every button in
// that page sends an action name back to whichever host is running, and each
// host answers with its own switch. The page cannot tell them apart, so an
// action handled by only one host is a button that does nothing on the other
// platform, silently, with no build error and no failing test.
//
// That has happened. This repository has already shipped a platform difference
// of exactly this kind, and the review that prompted this file recorded that
// the only thing keeping the two switches in step was someone reading them side
// by side. Reading is not a mechanism. This is.
//
// It deliberately checks names only, not behaviour. Two hosts can answer the
// same action differently for good reasons (one has a popover, the other a
// window), and the decisions worth sharing are extracted into internal/panelui
// where they are tested directly. What must never differ is which actions exist
// at all.
package hostparity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The two dispatch functions, and the parameter each switches on.
const (
	macOSHost   = "../../cmd/mcs-menubar/main.go"
	macOSFunc   = "goPanelAction"
	windowsHost = "../../cmd/mcs-tray/panel_windows.go"
	windowsFunc = "dispatchAction"
	// Both switch on a variable of this name. Reading the switch by subject
	// rather than by position means an unrelated switch added to either
	// function does not silently become the one under test.
	subject = "action"
)

func TestBothHostsHandleTheSamePanelActions(t *testing.T) {
	mac := actionsIn(t, macOSHost, macOSFunc)
	win := actionsIn(t, windowsHost, windowsFunc)

	// A host with no arms at all means the parse found the wrong thing, which
	// would make this test pass by knowing nothing.
	if len(mac) == 0 {
		t.Fatalf("no action arms found in %s (%s): the test is not reading what it thinks it is", macOSHost, macOSFunc)
	}
	if len(win) == 0 {
		t.Fatalf("no action arms found in %s (%s): the test is not reading what it thinks it is", windowsHost, windowsFunc)
	}

	for _, a := range missing(mac, win) {
		t.Errorf("action %q is handled on macOS but not on Windows: the same button in the shared panel does nothing there", a)
	}
	for _, a := range missing(win, mac) {
		t.Errorf("action %q is handled on Windows but not on macOS: the same button in the shared panel does nothing there", a)
	}
}

// TestEveryActionThePageSendsIsHandled catches the other direction: a button
// wired to an action name neither host answers, or answers under a typo.
func TestEveryActionThePageSendsIsHandled(t *testing.T) {
	mac := actionsIn(t, macOSHost, macOSFunc)
	sent := sentByPage(t)
	// Without this the test passes by finding nothing, which is exactly how its
	// first version passed. The page has well over a dozen buttons; a handful
	// means the scan stopped matching.
	if len(sent) < 10 {
		t.Fatalf("only %d action names found in the page source (%v): the scan is not reading what it thinks it is", len(sent), sent)
	}
	for _, a := range sent {
		if !mac[a] {
			t.Errorf("the panel sends %q but no host handles it: the button is dead on both platforms", a)
		}
	}
}

// sendCall matches the page's own bridge call, send('<action>', …), with a
// literal name. Calls whose name is a variable (the confirmation dialog
// replaying a deferred action) are skipped: there is no name to check.
var sendCall = regexp.MustCompile(`send\('([A-Za-z][A-Za-z0-9]*)'`)

// sentByPage lists the action names internal/panelui's rendered page can send.
//
// It scans the package source as TEXT, not as a syntax tree. The calls live
// inside Go string literals holding the page's HTML and JavaScript, so to the
// Go parser they are not calls at all. An earlier version of this walked the
// AST for them, found nothing, and passed every run by knowing nothing, which
// is why the count assertion in the caller exists.
func sentByPage(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../panelui/*.go")
	if err != nil {
		t.Fatalf("listing internal/panelui: %v", err)
	}
	names := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for _, m := range sendCall.FindAllSubmatch(src, -1) {
			names[string(m[1])] = true
		}
	}
	return sortedKeys(names)
}

// actionsIn returns the set of case values of the switch on `subject` inside
// the named function.
func actionsIn(t *testing.T, path, fn string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	// ParseFile does not care about build tags, which is the point: the Windows
	// host is behind //go:build windows and would otherwise be unreadable from
	// the macOS and Linux test legs, i.e. from every leg where the drift would
	// matter most.
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	found := map[string]bool{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			id, ok := sw.Tag.(*ast.Ident)
			if !ok || id.Name != subject {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					if s, ok := stringLit(e); ok {
						found[s] = true
					}
				}
			}
			return true
		})
	}
	return found
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// missing returns the members of a that b does not have, sorted so a failure
// reads the same way every run.
func missing(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
