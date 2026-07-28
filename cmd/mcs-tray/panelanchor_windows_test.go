//go:build windows

package main

import (
	"testing"
	"time"
)

// The WebView2 panel itself needs a display and cannot run in CI, so the two
// pieces of logic that decide *whether* and *where* it opens are kept as pure
// functions and tested here.

func TestShouldSpawnPanel(t *testing.T) {
	tests := []struct {
		name       string
		shown      bool
		sinceClose time.Duration
		want       bool
	}{
		{"panel on screen, click is the toggle-shut", true, time.Hour, false},
		{"panel just parked from this same click", false, 0, false},
		{"panel parked well within the guard", false, panelReopenGuard / 2, false},
		{"exactly at the guard boundary reopens", false, panelReopenGuard, true},
		{"long after a park, click reopens", false, time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSpawnPanel(tt.shown, tt.sinceClose); got != tt.want {
				t.Errorf("shouldSpawnPanel(%v, %v) = %v, want %v", tt.shown, tt.sinceClose, got, tt.want)
			}
		})
	}
}

func TestSincePanelClosedBeforeFirstShow(t *testing.T) {
	panelHiddenAtNano.Store(0)
	if got := sincePanelClosed(time.Now()); got < panelReopenGuard {
		t.Errorf("with the panel never shown, sincePanelClosed() = %v, want >= %v", got, panelReopenGuard)
	}
}

func TestSincePanelClosedAfterPark(t *testing.T) {
	now := time.Now()
	panelHiddenAtNano.Store(now.Add(-150 * time.Millisecond).UnixNano())
	defer panelHiddenAtNano.Store(0)

	got := sincePanelClosed(now)
	if got < 140*time.Millisecond || got > 160*time.Millisecond {
		t.Errorf("sincePanelClosed() = %v, want about 150ms", got)
	}
}

func TestParseAnchorPoint(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   point
		wantOK bool
	}{
		{"plain pair", "1893,1049", point{X: 1893, Y: 1049}, true},
		{"negative coords", "-408,484", point{X: -408, Y: 484}, true},
		{"spaces tolerated", " 12 , 34 ", point{X: 12, Y: 34}, true},
		{"no comma", "1893", point{}, false},
		{"not numeric", "abc,12", point{}, false},
		{"empty", "", point{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAnchorPoint(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseAnchorPoint(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseAnchorArg(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   point
		wantOK bool
	}{
		{
			name:   "anchor passed by the tray",
			args:   []string{"mcs-tray.exe", "--panel", "--anchor", "1893,1049"},
			want:   point{X: 1893, Y: 1049},
			wantOK: true,
		},
		{
			name:   "negative coords on a monitor left of the primary",
			args:   []string{"mcs-tray.exe", "--panel", "--anchor", "-408,484"},
			want:   point{X: -408, Y: 484},
			wantOK: true,
		},
		{
			name:   "surrounding spaces are tolerated",
			args:   []string{"--panel", "--anchor", " 12 , 34 "},
			want:   point{X: 12, Y: 34},
			wantOK: true,
		},
		{"no anchor flag at all", []string{"mcs-tray.exe", "--panel"}, point{}, false},
		{"flag present but value missing", []string{"--panel", "--anchor"}, point{}, false},
		{"value has no comma", []string{"--anchor", "1893"}, point{}, false},
		{"value is not numeric", []string{"--anchor", "abc,12"}, point{}, false},
		{"only one side numeric", []string{"--anchor", "12,xyz"}, point{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAnchorArg(tt.args)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseAnchorArg(%q) = %+v, %v; want %+v, %v", tt.args, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestPanelPlacementStaysInsideWorkArea(t *testing.T) {
	const w, h = 400, 540

	tests := []struct {
		name   string
		anchor point
		work   rect
		wantX  int32
		wantY  int32
	}{
		{
			// 1920x1080 with the usual bottom taskbar. The cursor is on the
			// taskbar itself, below the work area.
			name:   "bottom taskbar, tray at bottom right",
			anchor: point{X: 1893, Y: 1049},
			work:   rect{Left: 0, Top: 0, Right: 1920, Bottom: 1032},
			wantX:  1920 - w - trayIconMarginPx, // pulled back from the right edge
			wantY:  1032 - h - trayIconMarginPx, // sits above the taskbar
		},
		{
			name:   "top taskbar opens downward",
			anchor: point{X: 1893, Y: 20},
			work:   rect{Left: 0, Top: 48, Right: 1920, Bottom: 1080},
			wantX:  1920 - w - trayIconMarginPx,
			wantY:  48 + trayIconMarginPx,
		},
		{
			name:   "left taskbar, panel clears the taskbar",
			anchor: point{X: 30, Y: 1050},
			work:   rect{Left: 72, Top: 0, Right: 1920, Bottom: 1080},
			wantX:  72 + trayIconMarginPx,
			wantY:  1080 - h - trayIconMarginPx,
		},
		{
			// Monitor sitting to the left of the primary one: the work area
			// has negative coordinates and the panel must follow it there.
			name:   "secondary monitor with negative coordinates",
			anchor: point{X: -100, Y: 1049},
			work:   rect{Left: -1920, Top: 0, Right: 0, Bottom: 1032},
			wantX:  0 - w - trayIconMarginPx,
			wantY:  1032 - h - trayIconMarginPx,
		},
		{
			// Centring on the click only applies when there is room for it.
			name:   "click in the middle centres the panel",
			anchor: point{X: 900, Y: 1049},
			work:   rect{Left: 0, Top: 0, Right: 1920, Bottom: 1032},
			wantX:  900 - w/2,
			wantY:  1032 - h - trayIconMarginPx,
		},
		{
			// Degenerate: work area smaller than the panel. Clamp to the
			// top-left corner rather than emitting an off-screen position.
			name:   "work area smaller than the panel",
			anchor: point{X: 290, Y: 290},
			work:   rect{Left: 0, Top: 0, Right: 300, Bottom: 300},
			wantX:  trayIconMarginPx,
			wantY:  trayIconMarginPx,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := panelPlacement(tt.anchor, tt.work, w, h)
			if x != tt.wantX || y != tt.wantY {
				t.Errorf("panelPlacement(%+v, %+v) = (%d, %d), want (%d, %d)", tt.anchor, tt.work, x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

// TestPanelPlacementNeverOverflows is the property the individual cases above
// are all instances of: for a work area at least as large as the panel, the
// panel always lands fully inside it.
func TestPanelPlacementNeverOverflows(t *testing.T) {
	const w, h = 400, 540
	works := []rect{
		{Left: 0, Top: 0, Right: 1920, Bottom: 1032},
		{Left: 0, Top: 48, Right: 1920, Bottom: 1080},
		{Left: 72, Top: 0, Right: 1920, Bottom: 1080},
		{Left: -1920, Top: 0, Right: 0, Bottom: 1032},
		{Left: 0, Top: 0, Right: 800, Bottom: 600},
	}
	anchors := []point{{X: -3000, Y: -3000}, {X: 0, Y: 0}, {X: 960, Y: 540}, {X: 5000, Y: 5000}}

	for _, work := range works {
		for _, a := range anchors {
			x, y := panelPlacement(a, work, w, h)
			if x < work.Left || y < work.Top || x+w > work.Right || y+h > work.Bottom {
				t.Errorf("panel at (%d,%d)+%dx%d escapes work area %+v (anchor %+v)", x, y, w, h, work, a)
			}
		}
	}
}
