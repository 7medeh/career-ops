package main

import (
	"strings"
	"testing"
)

// readEmbedded pulls a file out of the same embed.FS the built app serves, so
// these tests assert on what actually ships -- not on what is sitting in
// frontend/dist at the moment. A frontend edit that never reaches the binary
// (wrong path, wrong dir, stale build wiring) fails here.
func readEmbedded(t *testing.T, name string) string {
	t.Helper()
	b, err := assets.ReadFile(name)
	if err != nil {
		t.Fatalf("embedded asset %s missing from the binary: %v", name, err)
	}
	return string(b)
}

// TestApplyBarIsWiredEndToEnd checks the paste-a-link control across all three
// frontend files at once. Each piece is useless without the others: markup with
// no listener is a dead input, a listener with no markup is a no-op, and either
// one silently does nothing at runtime rather than failing loudly.
func TestApplyBarIsWiredEndToEnd(t *testing.T) {
	t.Parallel()

	html := readEmbedded(t, "frontend/dist/index.html")
	js := readEmbedded(t, "frontend/dist/app.js")
	css := readEmbedded(t, "frontend/dist/style.css")

	for _, want := range []string{
		`id="paste-url"`,      // the input
		`id="paste-apply"`,    // the button
		`class="applybar"`,    // its container
		`Open Claude session`, // the button label
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html is missing %s", want)
		}
	}

	// The bar must sit outside <main>, otherwise it is scoped to one tab's
	// .view and disappears when the user switches tabs.
	//
	// Comments are stripped first: prose explaining the layout naturally
	// mentions the very tags being located, and matching inside a comment
	// reports a bogus ordering failure.
	markup := stripHTMLComments(html)
	mainIdx := strings.Index(markup, "<main>")
	barIdx := strings.Index(markup, `class="applybar"`)
	if mainIdx < 0 || barIdx < 0 {
		t.Fatal("could not locate <main> and the apply bar in index.html")
	}
	if barIdx > mainIdx {
		t.Error("apply bar is inside <main>; it must precede it to stay visible on every tab")
	}

	for _, want := range []string{
		`$("#paste-url")`,   // input is looked up
		`$("#paste-apply")`, // button is looked up
		"applyPastedURL",    // the handler exists
		"backend().Apply(",  // and calls the Go binding
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js is missing %s", want)
		}
	}

	for _, want := range []string{".applybar", ".url-input"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing a rule for %s", want)
		}
	}
}

// TestApplyBarHandlersAreRegistered guards the wiring that makes the control
// respond at all: without these listeners the input renders but does nothing.
func TestApplyBarHandlersAreRegistered(t *testing.T) {
	t.Parallel()

	js := readEmbedded(t, "frontend/dist/app.js")
	for _, want := range []string{
		`pasteBtn.addEventListener("click", applyPastedURL)`,
		`pasteInput.addEventListener("input", refreshPasteState)`,
		`pasteInput.addEventListener("keydown"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js is missing listener registration: %s", want)
		}
	}
}

// stripHTMLComments removes <!-- ... --> blocks so tag-position assertions see
// only real markup. Unterminated comments drop the rest of the document, which
// is the conservative choice: a malformed comment would swallow markup in a
// real browser too.
func stripHTMLComments(s string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:start])
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			return b.String()
		}
		s = s[start+end+3:]
	}
}
