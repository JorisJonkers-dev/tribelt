package content

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

func markdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}

// GoLink wraps an Official Page URL in the Outbound Click endpoint.
func GoLink(to, from string) string {
	return "/go?" + url.Values{"to": {to}, "from": {from}}.Encode()
}

// renderMarkdown renders a Markdown body to HTML, routing tribelt.nl links through /go.
func renderMarkdown(md goldmark.Markdown, src, fromPath string) (string, error) {
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.Link); ok && entering && isOfficialURL(string(l.Destination)) {
			l.Destination = []byte(GoLink(string(l.Destination), fromPath))
			l.SetAttributeString("rel", []byte("nofollow"))
		}
		if img, ok := n.(*ast.Image); ok && entering {
			img.SetAttributeString("loading", []byte("lazy"))
		}
		return ast.WalkContinue, nil
	})
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// plainText concatenates the text under n.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// WordCount counts the words of a Markdown body.
func WordCount(src string) int { return len(strings.Fields(PlainBody(src))) }

// PlainBody returns the body's prose without Markdown syntax.
func PlainBody(src string) string {
	source := []byte(src)
	doc := markdown().Parser().Parse(text.NewReader(source))
	var parts []string
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		parts = append(parts, blockText(c, source)...)
	}
	return strings.Join(parts, "\n")
}

func blockText(n ast.Node, src []byte) []string {
	switch n.Kind() {
	case ast.KindParagraph, ast.KindHeading, ast.KindTextBlock:
		return []string{plainText(n, src)}
	}
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Type() == ast.TypeBlock {
			out = append(out, blockText(c, src)...)
		} else {
			return []string{plainText(n, src)}
		}
	}
	return out
}
