package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const (
	templatePath = "../../docs/site/template.html"
	stringsPath  = "../../docs/site/strings.json"
)

// Every language must carry exactly the strings English has. A missing one
// would stop the build anyway (missingkey=error); an empty or leftover one would
// not, and is what this catches.
func TestEveryLanguageHasEveryString(t *testing.T) {
	raw, err := os.ReadFile(stringsPath)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]map[string]string
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	en := all["en"]
	if len(en) == 0 {
		t.Fatal("no English strings")
	}
	for _, l := range Langs {
		got, ok := all[l.Code]
		if !ok {
			t.Errorf("%s: no block in strings.json", l.Code)
			continue
		}
		for k := range en {
			if got[k] == "" {
				t.Errorf("%s: missing or empty %q", l.Code, k)
			}
		}
		for k := range got {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %q is not an English key", l.Code, k)
			}
		}
	}
	listed := map[string]bool{}
	for _, l := range Langs {
		listed[l.Code] = true
	}
	for code := range all {
		if !listed[code] {
			t.Errorf("strings.json has %s, which Langs does not publish", code)
		}
	}
}

// The pages in docs/ are generated files committed for GitHub Pages. This fails
// when someone edits the template or the strings and forgets to regenerate, or
// edits a generated page by hand.
func TestCommittedPagesAreUpToDate(t *testing.T) {
	files, err := Build(templatePath, stringsPath)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		on, err := os.ReadFile(filepath.Join("../../docs", n))
		if err != nil {
			t.Errorf("docs/%s: %v (run: go run ./scripts/gen-site)", n, err)
			continue
		}
		// Both sides lose their CRs: a Windows checkout turns LF into CRLF in the
		// committed pages AND in the template they are rendered from.
		if !bytes.Equal(lf(on), lf(files[n])) {
			t.Errorf("docs/%s is out of date (run: go run ./scripts/gen-site)", n)
		}
	}
}

func lf(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }
