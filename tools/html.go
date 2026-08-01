package tools

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTML → text for the LLM. Two jobs, both about spending a small character
// budget well: drop the parts of a page that are never the answer, and keep
// enough structure that what's left reads as a document instead of a wall.
//
// This parses rather than tokenizes. Regexes (`<[^>]*>`) mis-parse comments
// and attributes containing ">", and a raw token stream is worse: real pages
// have unclosed and implied end tags, so counting skip depth over tokens can
// silently swallow the rest of the document. html.Parse applies the HTML5
// error-recovery rules, and skipping a node then skips exactly its subtree.

// skippedTags never contain the answer, and their text is usually repeated
// on every page of a site.
var skippedTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true,
	atom.Svg: true, atom.Template: true, atom.Head: true,
	atom.Nav: true, atom.Footer: true, atom.Aside: true,
}

// blockTags start a new line in the output.
var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.Br: true, atom.Tr: true, atom.Blockquote: true, atom.Pre: true,
	atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Header: true, atom.Main: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
}

var headingLevel = map[atom.Atom]int{
	atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6,
}

// htmlToText renders HTML as lightweight markdown. When the page marks its
// content with <main> or <article>, only that is returned — on a link
// aggregator or a news site the surrounding chrome can easily be larger than
// the budget, and a head+tail truncation would then keep the menu and the
// footer while dropping the article.
func htmlToText(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	r := &renderer{}
	r.walk(doc)
	if content := normalizeLines(r.main.String()); len(content) > 200 {
		return content
	}
	return normalizeLines(r.all.String())
}

type renderer struct {
	all    strings.Builder
	main   strings.Builder // text inside <main>/<article>
	inMain int
}

func (r *renderer) write(s string) {
	r.all.WriteString(s)
	if r.inMain > 0 {
		r.main.WriteString(s)
	}
}

func (r *renderer) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		if text := collapseSpaces(n.Data); text != "" {
			r.write(text + " ")
		}
		return

	case html.ElementNode:
		if skippedTags[n.DataAtom] {
			return // whole subtree, no depth bookkeeping to get wrong
		}
		if n.DataAtom == atom.Title {
			// Kept even though <head> is skipped: it names the page.
			r.write("\n# " + collapseSpaces(textOf(n)) + "\n")
			return
		}
		if n.DataAtom == atom.Td || n.DataAtom == atom.Th {
			// Render into a sub-renderer so empty cells don't emit lone
			// separators (table-based layouts are otherwise all pipes),
			// while links inside cells keep their targets.
			sub := &renderer{}
			sub.walkChildren(n)
			if cell := strings.TrimSpace(collapseSpaces(sub.all.String())); cell != "" {
				r.write(" | " + cell)
			}
			return
		}
		if n.DataAtom == atom.A {
			r.writeLink(n)
			return
		}

		if n.DataAtom == atom.Main || n.DataAtom == atom.Article {
			r.inMain++
			defer func() { r.inMain-- }()
		}
		if blockTags[n.DataAtom] {
			r.write("\n")
		}
		if level := headingLevel[n.DataAtom]; level > 0 {
			r.write(strings.Repeat("#", level) + " ")
		}
		if n.DataAtom == atom.Li {
			r.write("\n- ")
		}

		r.walkChildren(n)

		if blockTags[n.DataAtom] {
			r.write("\n")
		}
		return
	}

	r.walkChildren(n)
}

func (r *renderer) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walk(c)
	}
}

// writeLink keeps the target next to the text: on a link aggregator the URLs
// are the content, and a bare list of headlines answers nothing. Relative
// and javascript: targets aren't worth their characters.
func (r *renderer) writeLink(n *html.Node) {
	text := collapseSpaces(textOf(n))
	if text != "" {
		r.write(text + " ")
	}
	for _, attr := range n.Attr {
		if attr.Key != "href" {
			continue
		}
		href := strings.TrimSpace(attr.Val)
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			r.write("(" + href + ")")
		}
		return
	}
}

// textOf returns the visible text of a subtree, skipping the same chrome.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
			return
		}
		if n.Type == html.ElementNode && skippedTags[n.DataAtom] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// normalizeLines trims each line and collapses runs of blank lines, so the
// budget goes to words rather than to the whitespace of nested markup.
func normalizeLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
