// Command website builds the documentation site into ./dist.
//
//	go run . build          # write the site to ./dist
//	go run . serve [:8080]  # build, then serve ./dist locally
//
// Pages are Markdown files in ./content with a small front matter block.
// The sidebar order lives in the nav variable below.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

const (
	siteURL  = "https://laravel-queue-go.reno.sh"
	repoURL  = "https://github.com/rjp2525/laravel-queue-go"
	siteName = "laravel-queue-go"
)

// section is a folder in the sidebar; slugs are files under content/docs.
type section struct {
	Name  string
	Slugs []string
}

var nav = []section{
	{"getting-started", []string{"introduction", "installation", "quick-start"}},
	{"consuming", []string{"workers", "handlers", "retries", "middleware", "events"}},
	{"dispatching", []string{"dispatching", "chains"}},
	{"laravel", []string{"laravel-versions", "failed-jobs", "batches"}},
	{"operations", []string{"configuration", "lqworker", "going-to-production"}},
	{"reference", []string{"how-it-works", "php-serialization", "limitations", "upgrading"}},
}

type heading struct {
	Level int
	ID    string
	Text  string
}

type page struct {
	Slug        string // "workers"; "" for the home page
	Section     string
	Title       string
	Description string
	URL         string
	Source      string // path of the Markdown file, for the "edit" link
	Body        template.HTML
	Headings    []heading
	Text        string // plain text, for search
	Image       string // social preview image, e.g. "/og/workers.png"
	Prev, Next  *page
}

type navItem struct {
	Title, URL string
	Active     bool
}

type navSection struct {
	Name  string
	Open  bool
	Items []navItem
}

type view struct {
	Page     *page
	Nav      []navSection
	Home     bool
	SiteURL  string
	RepoURL  string
	SiteName string
	Year     int
}

func main() {
	log.SetFlags(0)
	cmd := "build"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := build("dist"); err != nil {
		log.Fatal(err)
	}
	if cmd == "serve" {
		addr := "127.0.0.1:8080"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		log.Printf("serving ./dist on http://%s", addr)
		log.Fatal(http.ListenAndServe(addr, http.FileServer(http.Dir("dist"))))
	}
}

func build(out string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	md := newMarkdown()
	tmpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		return err
	}

	// Load the docs pages in sidebar order.
	var pages []*page
	for _, sec := range nav {
		for _, slug := range sec.Slugs {
			p, err := loadPage(md, filepath.Join("content", "docs", slug+".md"))
			if err != nil {
				return err
			}
			p.Slug, p.Section, p.URL = slug, sec.Name, "/docs/"+slug+"/"
			pages = append(pages, p)
		}
	}
	for i, p := range pages {
		if i > 0 {
			p.Prev = pages[i-1]
		}
		if i < len(pages)-1 {
			p.Next = pages[i+1]
		}
	}

	render := func(name, dest string, p *page) error {
		v := view{Page: p, Home: name == "home.html", SiteURL: siteURL, RepoURL: repoURL, SiteName: siteName, Year: time.Now().Year()}
		for _, sec := range nav {
			ns := navSection{Name: sec.Name}
			for _, slug := range sec.Slugs {
				for _, q := range pages {
					if q.Slug == slug {
						active := p != nil && q.Slug == p.Slug
						ns.Open = ns.Open || active
						ns.Items = append(ns.Items, navItem{Title: q.Title, URL: q.URL, Active: active})
					}
				}
			}
			v.Nav = append(v.Nav, ns)
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, v); err != nil {
			return fmt.Errorf("%s: %w", dest, err)
		}
		dest = filepath.Join(out, dest)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, buf.Bytes(), 0o644)
	}

	// Social preview images.
	fonts, err := loadOGFonts()
	if err != nil {
		return err
	}
	for _, p := range pages {
		p.Image = "/og/" + p.Slug + ".png"
		tab := "_docs"
		switch p.Slug {
		case "laravel-versions":
			tab = "_laravel"
		case "upgrading":
			tab = "_upgrading"
		}
		card := ogCard{Kicker: "// docs / " + p.Section, Title: p.Title, Description: p.Description, Tab: tab}
		if err := writeOGImage(fonts, filepath.Join(out, filepath.FromSlash(p.Image)), card); err != nil {
			return err
		}
	}
	const homeImage = "/og/home.png"
	if err := writeOGImage(fonts, filepath.Join(out, filepath.FromSlash(homeImage)), ogCard{
		Kicker:      "// hi laravel. meet go.",
		Title:       siteName,
		Description: "> work your Laravel queues from Go. Same jobs, same retries, same failed-jobs table.",
		Tab:         "_hello",
	}); err != nil {
		return err
	}

	for _, p := range pages {
		if err := render("doc.html", filepath.Join("docs", p.Slug, "index.html"), p); err != nil {
			return err
		}
	}

	home, err := loadPage(md, filepath.Join("content", "home.md"))
	if err != nil {
		return err
	}
	home.URL, home.Image = "/", homeImage
	if err := render("home.html", "index.html", home); err != nil {
		return err
	}
	notFound := &page{Title: "Not found", Description: "That page does not exist.", URL: "/404.html", Image: homeImage}
	if err := render("404.html", "404.html", notFound); err != nil {
		return err
	}
	// /docs/ sends people to the first page.
	redirect := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>Docs</title><meta http-equiv="refresh" content="0; url=%s"><link rel="canonical" href="%s%s">`, pages[0].URL, siteURL, pages[0].URL)
	if err := os.WriteFile(filepath.Join(out, "docs", "index.html"), []byte(redirect), 0o644); err != nil {
		return err
	}

	if err := writeSearchIndex(out, pages); err != nil {
		return err
	}
	if err := writeSitemap(out, pages); err != nil {
		return err
	}
	if err := copyDir("static", out); err != nil {
		return err
	}
	if err := checkLinks(out, pages); err != nil {
		return err
	}
	log.Printf("built %d pages into %s", len(pages)+2, out)
	return nil
}

func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
				highlighting.WithWrapperRenderer(func(w util.BufWriter, ctx highlighting.CodeBlockContext, entering bool) {
					if !entering {
						_, _ = w.WriteString("</div>")
						return
					}
					lang := "text"
					if l, ok := ctx.Language(); ok && len(l) > 0 {
						lang = string(l)
					}
					fmt.Fprintf(w, `<div class="code"><div class="code-head"><span>%s</span><button type="button" class="copy" aria-label="Copy code">copy</button></div>`, template.HTMLEscapeString(lang))
				}),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
}

var (
	headingRe = regexp.MustCompile(`(?s)<h([23]) id="([^"]+)">(.*?)</h[23]>`)
	tagRe     = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe   = regexp.MustCompile(`\s+`)
	// GitHub-style alerts: a blockquote whose first line is [!NOTE] etc.
	alertRe = regexp.MustCompile(`(?s)<blockquote>\s*<p>\[!(NOTE|TIP|WARNING)\]\s*(.*?)</blockquote>`)
	tableRe = regexp.MustCompile(`(?s)<table>.*?</table>`)
)

func loadPage(md goldmark.Markdown, path string) (*page, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := &page{Source: path}
	body := string(raw)
	if rest, ok := strings.CutPrefix(body, "---\n"); ok {
		front, content, found := strings.Cut(rest, "\n---\n")
		if !found {
			return nil, fmt.Errorf("%s: unterminated front matter", path)
		}
		body = content
		for line := range strings.SplitSeq(front, "\n") {
			key, value, _ := strings.Cut(line, ":")
			switch strings.TrimSpace(key) {
			case "title":
				p.Title = strings.TrimSpace(value)
			case "description":
				p.Description = strings.TrimSpace(value)
			}
		}
	}
	if p.Title == "" {
		return nil, fmt.Errorf("%s: missing title", path)
	}

	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := buf.String()
	out = alertRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := alertRe.FindStringSubmatch(m)
		kind := strings.ToLower(sub[1])
		return fmt.Sprintf(`<div class="callout callout-%s"><span class="callout-label">// %s</span><p>%s</div>`, kind, kind, sub[2])
	})
	out = tableRe.ReplaceAllStringFunc(out, func(m string) string { return `<div class="table-wrap">` + m + `</div>` })
	out = headingRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := headingRe.FindStringSubmatch(m)
		level := 2
		if sub[1] == "3" {
			level = 3
		}
		p.Headings = append(p.Headings, heading{Level: level, ID: sub[2], Text: stripTags(sub[3])})
		return fmt.Sprintf(`<h%s id="%s"><a class="anchor" href="#%s" aria-hidden="true">#</a>%s</h%s>`, sub[1], sub[2], sub[2], sub[3], sub[1])
	})
	p.Body = template.HTML(out)
	p.Text = stripTags(out)
	return p, nil
}

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&#39;", "'").Replace(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

func writeSearchIndex(out string, pages []*page) error {
	type entry struct {
		Title    string   `json:"t"`
		URL      string   `json:"u"`
		Section  string   `json:"s"`
		Headings []string `json:"h"`
		Text     string   `json:"x"`
	}
	entries := make([]entry, 0, len(pages))
	for _, p := range pages {
		e := entry{Title: p.Title, URL: p.URL, Section: p.Section, Text: p.Text}
		for _, h := range p.Headings {
			e.Headings = append(e.Headings, h.ID+"|"+h.Text)
		}
		entries = append(entries, e)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "search.json"), data, 0o644)
}

func writeSitemap(out string, pages []*page) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	fmt.Fprintf(&b, "  <url><loc>%s/</loc></url>\n", siteURL)
	for _, p := range pages {
		fmt.Fprintf(&b, "  <url><loc>%s%s</loc></url>\n", siteURL, p.URL)
	}
	b.WriteString("</urlset>\n")
	return os.WriteFile(filepath.Join(out, "sitemap.xml"), []byte(b.String()), 0o644)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

var hrefRe = regexp.MustCompile(`href="(/[^"#]*)(#[^"]*)?"`)

// checkLinks fails the build when a page links to an internal URL or anchor
// that does not exist, so a renamed page cannot leave dead links behind.
func checkLinks(out string, pages []*page) error {
	anchors := map[string]map[string]bool{}
	for _, p := range pages {
		ids := map[string]bool{}
		for _, h := range p.Headings {
			ids[h.ID] = true
		}
		anchors[p.URL] = ids
	}
	var broken []string
	err := filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range hrefRe.FindAllStringSubmatch(string(data), -1) {
			target := filepath.Join(out, filepath.FromSlash(m[1]))
			if strings.HasSuffix(m[1], "/") {
				target = filepath.Join(target, "index.html")
			}
			if _, err := os.Stat(target); err != nil {
				broken = append(broken, fmt.Sprintf("%s -> %s", path, m[1]))
				continue
			}
			if ids, ok := anchors[m[1]]; ok && len(m[2]) > 1 && !ids[m[2][1:]] {
				broken = append(broken, fmt.Sprintf("%s -> %s%s (no such heading)", path, m[1], m[2]))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(broken) > 0 {
		return fmt.Errorf("broken links:\n  %s", strings.Join(broken, "\n  "))
	}
	return nil
}
