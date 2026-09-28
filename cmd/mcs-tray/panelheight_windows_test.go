//go:build windows

package main

import "testing"

// TestFittedPanelHeight pins how the panel window follows its content: it
// shrinks to a short screen, never below the floor that keeps the confirm
// dialog and progress card whole, never above the old fixed height, and scales
// with display scaling through the window-over-inner ratio.
func TestFittedPanelHeight(t *testing.T) {
	cases := []struct {
		name                string
		cur, content, inner int32
		want                int32
		ok                  bool
	}{
		{"short list shrinks the window", 540, 360, 540, 360, true},
		{"never below the floor", 540, 200, 540, panelMinHeightCSS, true},
		{"never above the old height", 360, 900, 360, panelHeight, true},
		{"150% scaling: 540 window px show 360 CSS px", 540, 330, 360, 495, true},
		{"within a pixel is left alone", 400, 401, 400, 400, false},
		{"a bad report changes nothing", 540, 0, 540, 540, false},
	}
	for _, tc := range cases {
		got, ok := fittedPanelHeight(tc.cur, tc.content, tc.inner)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: fittedPanelHeight(%d,%d,%d) = (%d,%v), want (%d,%v)",
				tc.name, tc.cur, tc.content, tc.inner, got, ok, tc.want, tc.ok)
		}
	}
}
