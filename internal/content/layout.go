package content

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Layout reshapes a parsed body for presentation only: the text and its order stay as written.

var kindBox = ast.NewNodeKind("Box") //nolint:gochecknoglobals // goldmark node kinds are registered once, at init

// box is a block wrapper rendered as fixed open and close markup around its children.
type box struct {
	ast.BaseBlock
	open, close string
}

func (n *box) Kind() ast.NodeKind { return kindBox }

func (n *box) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

func newBox(open, end string) *box { return &box{open: open, close: end} }

type boxRenderer struct{}

func (boxRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindBox, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		b := n.(*box)
		if entering {
			_, _ = w.WriteString(b.open)
		} else {
			_, _ = w.WriteString(b.close)
		}
		return ast.WalkContinue, nil
	})
}

// layout turns link lists and linked heading runs into cards, then groups the body into page sections.
func (b *builder) layout(doc ast.Node, src []byte, p *Page) {
	b.linkCards(doc, src)
	b.headingCards(doc)
	groups := h2Groups(doc)
	switch p.Type {
	case "home":
		for i, g := range groups {
			wrap(doc, g, `<section class="band band-`+strconv.Itoa(i+1)+`"><div class="wrap prose">`, "</div></section>\n")
		}
	case "product", "sector":
		if len(groups) == 0 {
			return
		}
		rest := groups[1:]
		wrap(doc, groups[0], `<section class="band band-intro"><div class="wrap prose lead">`, "</div></section>\n")
		if p.Type == "product" {
			n := 0
			for n < len(rest) && !hasCards(rest[n]) && !b.linkList(rest[n]) {
				n++
			}
			if n > 1 {
				tabs(doc, rest[:n])
				rest = rest[n:]
			}
		}
		flow(doc, rest)
	default:
		flow(doc, groups)
	}
}

// h2Groups splits the top level at every H2; content before the first H2 joins the first group.
func h2Groups(doc ast.Node) [][]ast.Node {
	var groups [][]ast.Node
	var lead []ast.Node
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		if h, ok := c.(*ast.Heading); ok && h.Level == 2 {
			groups = append(groups, nil)
		}
		if len(groups) == 0 {
			lead = append(lead, c)
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], c)
	}
	switch {
	case len(lead) == 0:
		return groups
	case len(groups) == 0:
		return [][]ast.Node{lead}
	}
	groups[0] = append(lead, groups[0]...)
	return groups
}

func wrap(doc ast.Node, nodes []ast.Node, open, end string) {
	b := newBox(open, end)
	doc.InsertBefore(doc, nodes[0], b)
	for _, n := range nodes {
		b.AppendChild(b, n)
	}
}

func flow(doc ast.Node, groups [][]ast.Node) {
	var nodes []ast.Node
	for _, g := range groups {
		nodes = append(nodes, g...)
	}
	if len(nodes) > 0 {
		wrap(doc, nodes, `<div class="flow"><div class="wrap prose">`, "</div></div>\n")
	}
}

// tabs shows one H2 section at a time with CSS only: each H2 links to itself and :target picks the panel.
func tabs(doc ast.Node, groups [][]ast.Node) {
	outer := newBox(`<div class="flow flow-tabs"><div class="wrap"><div class="tabs">`, "</div></div></div>\n")
	doc.InsertBefore(doc, groups[0][0], outer)
	for _, g := range groups {
		tab := newBox(`<section class="tab">`, "</section>\n")
		outer.AppendChild(outer, tab)
		h := g[0].(*ast.Heading)
		if id, ok := h.AttributeString("id"); ok {
			linkChildren(h, "#"+string(id.([]byte)), "")
		}
		tab.AppendChild(tab, h)
		body := newBox(`<div class="tab-body prose">`, "</div>\n")
		tab.AppendChild(tab, body)
		for _, n := range g[1:] {
			body.AppendChild(body, n)
		}
	}
}

func hasCards(nodes []ast.Node) bool {
	for _, n := range nodes {
		if _, ok := n.(*box); ok {
			return true
		}
		if l, ok := n.(*ast.List); ok {
			if c, _ := l.AttributeString("class"); c != nil && strings.HasPrefix(string(c.([]byte)), "cards") {
				return true
			}
		}
	}
	return false
}

// linkList reports a group holding a list whose every item links to a Mirror Page: further reading,
// which stays visible below the tabs.
func (b *builder) linkList(nodes []ast.Node) bool {
	for _, n := range nodes {
		l, ok := n.(*ast.List)
		if !ok {
			continue
		}
		all := true
		for item := l.FirstChild(); item != nil && all; item = item.NextSibling() {
			all = b.firstTarget(item) != nil
		}
		if all {
			return true
		}
	}
	return false
}

// linkChildren moves a node's inline children into one link.
func linkChildren(n ast.Node, href, class string) {
	l := ast.NewLink()
	l.Destination = []byte(href)
	if class != "" {
		l.SetAttributeString("class", []byte(class))
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		l.AppendChild(l, c)
		c = next
	}
	n.AppendChild(n, l)
}

// target resolves an internal link to the Mirror Page it points at.
func (b *builder) target(n ast.Node) *Page {
	l, ok := n.(*ast.Link)
	if !ok {
		return nil
	}
	dest := string(l.Destination)
	if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
		return nil
	}
	dest, _, _ = strings.Cut(dest, "#")
	dest, _, _ = strings.Cut(dest, "?")
	p, _ := b.c.Resolve(dest)
	return p
}

func cardKind(t string) string {
	switch t {
	case "product", "sector":
		return t
	case "article", "news", "case":
		return "post"
	case "vacancy":
		return "job"
	}
	return "page"
}

// linkCards turns a list whose every item starts with an internal page link into cards.
func (b *builder) linkCards(doc ast.Node, src []byte) {
	var lists []*ast.List
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.List); ok && entering {
			lists = append(lists, l)
		}
		return ast.WalkContinue, nil
	})
	for _, l := range lists {
		targets := b.itemTargets(l)
		if len(targets) == 0 || cardKind(targets[0].Type) == "page" {
			continue
		}
		kind := cardKind(targets[0].Type)
		l.SetAttributeString("class", []byte("cards cards-"+kind))
		i := 0
		for item := l.FirstChild(); item != nil; item = item.NextSibling() {
			item.SetAttributeString("class", []byte("card"))
			splitCardText(item, src)
			if img := b.cardImage(targets[i], kind); img != nil {
				item.InsertBefore(item, item.FirstChild(), img)
			}
			i++
		}
	}
}

// itemTargets are the pages a list's items lead with, or nil unless every item leads with one.
func (b *builder) itemTargets(l *ast.List) []*Page {
	var targets []*Page
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		first := item.FirstChild()
		if first == nil {
			return nil
		}
		_, link := leadLink(first.FirstChild())
		if link == nil || b.target(link) == nil {
			return nil
		}
		targets = append(targets, b.target(link))
	}
	return targets
}

// leadLink finds the link an inline node starts with: the node itself, or one wrapped in emphasis.
func leadLink(n ast.Node) (lead ast.Node, link *ast.Link) {
	switch v := n.(type) {
	case *ast.Link:
		return v, v
	case *ast.Emphasis:
		if l, ok := v.FirstChild().(*ast.Link); ok {
			return v, l
		}
	}
	return nil, nil
}

// splitCardText puts a card item's leading link in a title line and the rest in a text line. A ": "
// separator stays in the markup for screen readers and copied text, but is hidden visually.
func splitCardText(item ast.Node, src []byte) {
	first := item.FirstChild()
	lead, link := leadLink(first.FirstChild())
	link.SetAttributeString("class", []byte("card-link"))
	title := newBox(`<p class="card-title">`, "</p>\n")
	item.InsertBefore(item, first, title)
	title.AppendChild(title, lead)
	if first.FirstChild() != nil {
		text := newBox(`<p class="card-text">`, "</p>\n")
		item.InsertBefore(item, first, text)
		if t, ok := first.FirstChild().(*ast.Text); ok {
			v := t.Segment.Value(src)
			if trimmed := bytes.TrimLeft(v, ": "); bytes.HasPrefix(v, []byte(":")) && len(trimmed) > 0 {
				cut := len(v) - len(trimmed)
				sep := newBox(`<span class="sep">`, "</span>")
				sep.AppendChild(sep, ast.NewString(v[:cut]))
				text.AppendChild(text, sep)
				t.Segment = t.Segment.WithStart(t.Segment.Start + cut)
			}
		}
		for c := first.FirstChild(); c != nil; {
			next := c.NextSibling()
			text.AppendChild(text, c)
			c = next
		}
	}
	item.RemoveChild(item, first)
}

// cardGroup is a heading with the paragraphs after it and the page they link to.
type cardGroup struct {
	nodes  []ast.Node
	target *Page
	level  int
}

// headingCards turns a run of two or more same-level headings, each followed only by paragraphs that
// link to a Mirror Page, into a card grid.
func (b *builder) headingCards(doc ast.Node) {
	var run []cardGroup
	for c := doc.FirstChild(); c != nil; {
		h, ok := c.(*ast.Heading)
		if !ok || h.Level < 2 || h.Level > 3 {
			b.cardGrid(doc, run)
			run = nil
			c = c.NextSibling()
			continue
		}
		g, next := b.headingGroup(h)
		if g.target == nil || (len(run) > 0 && run[0].level != g.level) {
			b.cardGrid(doc, run)
			run = nil
		}
		if g.target != nil {
			run = append(run, g)
		}
		c = next
	}
	b.cardGrid(doc, run)
}

// headingGroup collects the paragraphs after h and returns the node after them.
func (b *builder) headingGroup(h *ast.Heading) (cardGroup, ast.Node) {
	g := cardGroup{nodes: []ast.Node{h}, level: h.Level}
	next := h.NextSibling()
	for ; next != nil; next = next.NextSibling() {
		para, ok := next.(*ast.Paragraph)
		if !ok {
			break
		}
		g.nodes = append(g.nodes, para)
		if g.target == nil {
			g.target = b.firstTarget(para)
		}
	}
	return g, next
}

func (b *builder) cardGrid(doc ast.Node, run []cardGroup) {
	if len(run) < 2 || cardKind(run[0].target.Type) == "page" {
		return
	}
	kind := cardKind(run[0].target.Type)
	grid := newBox(`<div class="cards cards-`+kind+`">`, "</div>\n")
	doc.InsertBefore(doc, run[0].nodes[0], grid)
	for _, g := range run {
		card := newBox(`<article class="card">`, "</article>\n")
		grid.AppendChild(grid, card)
		if img := b.cardImage(g.target, kind); img != nil {
			card.AppendChild(card, img)
		}
		linkChildren(g.nodes[0], g.target.Path, "card-link")
		for _, n := range g.nodes {
			card.AppendChild(card, n)
		}
	}
}

func (b *builder) firstTarget(n ast.Node) *Page {
	var found *Page
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && found == nil {
			found = b.target(c)
		}
		return ast.WalkContinue, nil
	})
	return found
}

// cardImage is the target page's picture as an inline image block, or nil when it has none.
func (b *builder) cardImage(p *Page, kind string) ast.Node {
	v := b.pageImage(p)
	if v == nil {
		return nil
	}
	img := ast.NewImage(ast.NewLink())
	img.Destination = []byte(v.Src)
	img.AppendChild(img, ast.NewString([]byte(v.Alt)))
	img.SetAttributeString("loading", []byte("lazy"))
	img.SetAttributeString("decoding", []byte("async"))
	if v.Width > 0 {
		img.SetAttributeString("width", []byte(strconv.Itoa(v.Width)))
		img.SetAttributeString("height", []byte(strconv.Itoa(v.Height)))
	}
	if v.Srcset != "" {
		img.SetAttributeString("srcset", []byte(v.Srcset))
		sizes := "(min-width: 62em) 30vw, (min-width: 40em) 45vw, 90vw"
		if kind == "job" {
			sizes = "160px"
		}
		img.SetAttributeString("sizes", []byte(sizes))
	}
	tb := ast.NewTextBlock()
	tb.AppendChild(tb, img)
	return tb
}
