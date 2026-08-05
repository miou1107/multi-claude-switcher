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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
	"github.com/miou1107/multi-claude-switcher/internal/panelui"
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

// sentByPage lists the action names the panel can send, read from the RENDERED
// pages rather than from panelui's source.
//
// Rendering is what makes this honest. Two of the Settings buttons build their
// handler by concatenation in Go — `onclick="send('` + action + `',”)"` — so
// in the source the action name and the send( that carries it are never
// adjacent, and a source scan cannot see them. Measured: with the earlier
// source-scanning version, renaming the Settings Back up button's action left
// this test green while the button was dead on both platforms. In the finished
// HTML there is nothing left to concatenate.
func sentByPage(t *testing.T) []string {
	t.Helper()
	names := map[string]bool{}
	for _, page := range everyScreen() {
		for _, m := range sendCall.FindAllStringSubmatch(page, -1) {
			names[m[1]] = true
		}
	}
	return sortedKeys(names)
}

// everyScreen renders each screen a user can reach. The fixtures only need to
// be rich enough for the buttons to be drawn: two profiles, because a
// single-profile list renders no per-row Remove item at all, and one with
// conversations to sync.
//
// A screen missing here is a screen whose buttons go unchecked, so this list is
// asserted against the renderer count in TestEveryScreenIsRendered below.
func everyScreen() []string {
	profiles := []panelui.ProfileVM{
		{Folder: "Claude", Name: "Personal", Current: true, Convos: 12},
		{Folder: "Claude_Profile2", Name: "Work", Convos: 3},
	}
	return []string{
		panelui.RenderList(profiles, true, ""),
		panelui.RenderSettings(panelui.SettingsVM{Version: "0.0.0"}),
		panelui.RenderMore(panelui.MoreVM{}),
		panelui.RenderDebug(panelui.DebugVM{Report: "report"}),
		panelui.RenderRescan(nil, nil),
		panelui.RenderNewProfile(panelui.NewProfileVM{}),
		panelui.RenderMerge(
			panelui.MergeCandidateVM{Folder: "Claude", Name: "Personal", Current: true},
			panelui.MergeCandidateVM{Folder: "Claude_Profile2", Name: "Personal"},
			core.MergePlan{}, "", false),
		panelui.RenderSync(profiles, "", false),
		panelui.RenderRemoved(panelui.RemovedVM{Name: "Work"}),
	}
}

// TestEveryScreenIsRendered fails when a renderer is added to panelui without
// being added to everyScreen, which would leave its buttons unchecked by the
// test above while everything stayed green.
func TestEveryScreenIsRendered(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("../panelui/*.go")
	if err != nil {
		t.Fatalf("listing internal/panelui: %v", err)
	}
	renderers := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Render") {
				renderers++
			}
		}
	}
	if got := len(everyScreen()); got != renderers {
		t.Errorf("everyScreen renders %d screens but panelui exports %d Render* functions: a screen's buttons are going unchecked", got, renderers)
	}
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
