package tools

import (
	"strings"
	"testing"
)

// Site chrome repeats on every page and is never the answer. On a small
// character budget it can crowd out the content entirely.
func TestHTMLToText_DropsChromeAndScripts(t *testing.T) {
	got := htmlToText(`<html><body>
		<nav><a href="https://site.example/about">About</a><a href="https://site.example/jobs">Jobs</a></nav>
		<script>var tracking = 1;</script>
		<style>.x{color:red}</style>
		<p>the actual answer</p>
		<aside>subscribe to our newsletter</aside>
		<footer>copyright 2026</footer>
	</body></html>`)

	if !strings.Contains(got, "the actual answer") {
		t.Errorf("want the content kept, got %q", got)
	}
	for _, junk := range []string{"About", "Jobs", "tracking", "color:red", "newsletter", "copyright"} {
		if strings.Contains(got, junk) {
			t.Errorf("want %q dropped, got %q", junk, got)
		}
	}
}

// When a page marks its content, the surrounding chrome can be larger than
// the whole budget — a head+tail truncation would then keep the menu and the
// footer and drop the article.
func TestHTMLToText_PrefersMainContent(t *testing.T) {
	filler := strings.Repeat("the article body goes on. ", 20)
	got := htmlToText(`<html><body>
		<div>` + strings.Repeat("boilerplate everywhere. ", 50) + `</div>
		<main><p>` + filler + `</p></main>
	</body></html>`)

	if strings.Contains(got, "boilerplate") {
		t.Errorf("want only <main> when it holds the content, got %q", got)
	}
	if !strings.Contains(got, "the article body goes on") {
		t.Errorf("want the main content, got %q", got)
	}
}

// A near-empty <main> is a layout artifact, not the content.
func TestHTMLToText_FallsBackWhenMainIsThin(t *testing.T) {
	got := htmlToText(`<html><body>
		<main><span>Menu</span></main>
		<div>` + strings.Repeat("the real content lives out here. ", 20) + `</div>
	</body></html>`)

	if !strings.Contains(got, "the real content lives out here") {
		t.Errorf("want the fallback to the whole page, got %q", got)
	}
}

// On a link aggregator the URLs are the content — a list of bare headlines
// answers nothing.
func TestHTMLToText_KeepsLinkTargets(t *testing.T) {
	got := htmlToText(`<p><a href="https://example.com/story">Erlang at 40</a></p>
		<p><a href="/relative">relative</a> <a href="javascript:void(0)">js</a></p>`)

	if !strings.Contains(got, "Erlang at 40 (https://example.com/story)") {
		t.Errorf("want absolute href kept next to its text, got %q", got)
	}
	if strings.Contains(got, "/relative") || strings.Contains(got, "javascript") {
		t.Errorf("want relative and javascript targets dropped, got %q", got)
	}
}

// The regex approach this replaced mis-parsed ">" inside comments and
// attributes, leaking fragments into the budget as noise.
func TestHTMLToText_HandlesAngleBracketsInMarkup(t *testing.T) {
	got := htmlToText(`<html><body>
		<!-- if (a > b) { drop this } -->
		<div onclick="if (x > y) hide()">visible text</div>
	</body></html>`)

	if got != "visible text" {
		t.Errorf("want only the text node, got %q", got)
	}
}

func TestHTMLToText_StructureAndWhitespace(t *testing.T) {
	got := htmlToText(`<h2>Heading</h2><ul><li>first</li><li>second</li></ul>
		<p>para    with

		spaces</p>`)

	for _, want := range []string{"## Heading", "- first", "- second", "para with spaces"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in output, got %q", want, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("want blank-line runs collapsed, got %q", got)
	}
}
