package web

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// localImages turns images that are not served by this site into plain links, so a
// wiki page never makes visitors' browsers load third-party images (tracking pixels).
type localImages struct{}

func (localImages) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	var external []*ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := n.(*ast.Image); ok && entering {
			dest := string(img.Destination)
			if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") || strings.HasPrefix(dest, "/\\") {
				external = append(external, img)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, img := range external {
		link := ast.NewLink()
		link.Destination, link.Title = img.Destination, img.Title
		for c := img.FirstChild(); c != nil; {
			next := c.NextSibling()
			link.AppendChild(link, c)
			c = next
		}
		img.Parent().ReplaceChild(img.Parent(), img, link)
	}
}

// wikiMarkdown renders wiki pages: GFM (tables, task lists, strikethrough, autolinks) in
// goldmark's safe mode (raw HTML and javascript: links are dropped), with heading anchors.
var wikiMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(util.Prioritized(localImages{}, 100)),
	),
)

var externalLinkRe = regexp.MustCompile(`<a href="(https?:)?//`)

// wikiHTML renders a wiki page body; links to other sites open in a new tab
func wikiHTML(body string) template.HTML {
	var buf bytes.Buffer
	if err := wikiMarkdown.Convert([]byte(body), &buf); err != nil {
		return template.HTML("<pre>" + html.EscapeString(body) + "</pre>")
	}
	out := externalLinkRe.ReplaceAllStringFunc(buf.String(), func(m string) string {
		return `<a rel="noopener noreferrer nofollow" target="_blank" href="` + strings.TrimPrefix(m, `<a href="`)
	})
	return template.HTML(out)
}
