// Builds the project website in docs/ (served by GitHub Pages) from one
// template and one file of translations, so every language says the same
// thing and none of them can fall behind:
//
//	docs/site/template.html  the page, with {{.T.key}} where words go
//	docs/site/strings.json   the words, one block per language
//
// English goes to docs/index.html and every other language to
// docs/<slug>/index.html, each listing all the others with hreflang so search
// engines show people the page in their own language. docs/sitemap.xml and
// docs/robots.txt are written alongside.
//
// Run from the repository root after changing either source file:
//
//	go run ./scripts/gen-site
//
// main_test.go fails the build when a language is missing a string or the
// committed pages no longer match what this would generate.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
)

// SiteURL is where GitHub Pages serves docs/.
const SiteURL = "https://miou1107.github.io/multi-claude-switcher/"

// Lang is one language the site is published in.
type Lang struct {
	Code string // BCP 47, for lang= and hreflang=
	Slug string // folder under docs/; "" for English at the root
	Name string // what the language calls itself, for the picker
	OG   string // Open Graph locale
}

// Langs is every published language, English first. Adding one means adding it
// here and giving it a block in strings.json; the test catches a missing block.
var Langs = []Lang{
	{"en", "", "English", "en_US"},
	{"zh-TW", "zh-tw", "繁體中文", "zh_TW"},
	{"zh-CN", "zh-cn", "简体中文", "zh_CN"},
	{"ja", "ja", "日本語", "ja_JP"},
	{"ko", "ko", "한국어", "ko_KR"},
	{"es", "es", "Español", "es_ES"},
	{"fr", "fr", "Français", "fr_FR"},
	{"de", "de", "Deutsch", "de_DE"},
	{"pt-BR", "pt-br", "Português (Brasil)", "pt_BR"},
	{"it", "it", "Italiano", "it_IT"},
	{"ru", "ru", "Русский", "ru_RU"},
	{"vi", "vi", "Tiếng Việt", "vi_VN"},
	{"th", "th", "ไทย", "th_TH"},
	{"id", "id", "Bahasa Indonesia", "id_ID"},
}

func (l Lang) url() string {
	if l.Slug == "" {
		return SiteURL
	}
	return SiteURL + l.Slug + "/"
}

// alternate is one entry in a page's language list.
type alternate struct {
	Code, Name, URL, Path string
	Current               bool
}

type page struct {
	Lang       Lang
	T          map[string]string
	URL        string
	SiteURL    string
	Base       string // prefix from this page back to docs/, for assets
	Alternates []alternate
}

// Build renders every page and the two crawler files, keyed by their path
// relative to docs/. Nothing is written, so the test can compare.
func Build(templatePath, stringsPath string) (map[string][]byte, error) {
	tmpl, err := template.New(filepath.Base(templatePath)).Option("missingkey=error").ParseFiles(templatePath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(stringsPath)
	if err != nil {
		return nil, err
	}
	var all map[string]map[string]string
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("%s: %w", stringsPath, err)
	}

	out := map[string][]byte{}
	var sitemap bytes.Buffer
	sitemap.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sitemap.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")

	for _, l := range Langs {
		t, ok := all[l.Code]
		if !ok {
			return nil, fmt.Errorf("strings.json has no block for %s", l.Code)
		}
		base, file := "", "index.html"
		if l.Slug != "" {
			base, file = "../", l.Slug+"/index.html"
		}
		p := page{Lang: l, T: t, URL: l.url(), SiteURL: SiteURL, Base: base}
		for _, o := range Langs {
			p.Alternates = append(p.Alternates, alternate{
				Code: o.Code, Name: o.Name, URL: o.url(),
				Path: base + o.Slug + map[bool]string{true: "", false: "/"}[o.Slug == ""],
				Current: o.Code == l.Code,
			})
		}
		// The root page's link to itself is "" from the root and "../" from a
		// subfolder; an empty href reloads the page, so spell it "./".
		for i := range p.Alternates {
			if p.Alternates[i].Path == "" {
				p.Alternates[i].Path = "./"
			}
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, p); err != nil {
			return nil, fmt.Errorf("%s: %w", l.Code, err)
		}
		out[file] = buf.Bytes()

		fmt.Fprintf(&sitemap, "  <url>\n    <loc>%s</loc>\n", l.url())
		for _, o := range Langs {
			fmt.Fprintf(&sitemap, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n", o.Code, o.url())
		}
		sitemap.WriteString("  </url>\n")
	}
	sitemap.WriteString("</urlset>\n")
	out["sitemap.xml"] = sitemap.Bytes()
	// docs/site/ holds the raw template, full of {{...}}; keep it out of search.
	out["robots.txt"] = []byte("User-agent: *\nAllow: /\nDisallow: /site/\n\nSitemap: " + SiteURL + "sitemap.xml\n")
	return out, nil
}

func main() {
	files, err := Build("docs/site/template.html", "docs/site/strings.json")
	if err != nil {
		log.Fatal(err)
	}
	for name, data := range files {
		path := filepath.Join("docs", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("wrote %d files under docs/\n", len(files))
}
