//go:build linux

// tgout plan detectors — renderer construction rules + property
// validator (plan rounds 4-17).
package telegram

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

// validateRendered machine-checks the constructive-validity properties
// (plan v5/v10): only renderer tags, balanced, never nested, no empty
// tag, all & < > outside tags escaped, span budget, UTF-16 budget.
func validateRendered(t *testing.T, out string) {
	t.Helper()
	tagRe := regexp.MustCompile(`</?(b|code|pre)>`)
	locs := tagRe.FindAllStringIndex(out, -1)
	depth := 0
	open := ""
	spans := 0
	last := 0
	var outside strings.Builder
	for _, lc := range locs {
		outside.WriteString(out[last:lc[0]])
		last = lc[1]
		tag := out[lc[0]:lc[1]]
		if !strings.HasPrefix(tag, "</") {
			if depth != 0 {
				t.Fatalf("NESTED tag %s inside <%s>: %q", tag, open, out)
			}
			depth = 1
			open = strings.Trim(tag, "<>")
			spans++
		} else {
			if depth != 1 || strings.Trim(tag, "</>") != open {
				t.Fatalf("unbalanced tag %s: %q", tag, out)
			}
			depth = 0
		}
	}
	outside.WriteString(out[last:])
	if depth != 0 {
		t.Fatalf("unclosed tag <%s>: %q", open, out)
	}
	for _, c := range []string{"<", ">"} {
		if strings.Contains(outside.String(), c) {
			t.Fatalf("unescaped %q outside renderer tags: %q", c, out)
		}
	}
	if m := regexp.MustCompile(`&[^#a-zA-Z]`).FindString(outside.String()); m != "" {
		t.Fatalf("raw ampersand outside entities: %q in %q", m, out)
	}
	if strings.Contains(out, "<b></b>") || strings.Contains(out, "<code></code>") || strings.Contains(out, "<pre></pre>") {
		t.Fatalf("EMPTY tag emitted: %q", out)
	}
	if spans > renderSpanBudget {
		t.Fatalf("span budget exceeded: %d", spans)
	}
	if len(utf16.Encode([]rune(out))) > renderUTF16Budget {
		t.Fatal("UTF-16 budget exceeded on ok=true output")
	}
}

// PROPERTY VALIDATOR over an adversarial corpus (plan v10).
func TestRenderHTMLPropertyValidator(t *testing.T) {
	corpus := []string{
		"plain prose only",
		"**bold** and `code` mixed",
		"<script>alert(1)</script> typed by the model",
		"model-typed <pre>fake</pre> & ampersand",
		"**unclosed bold and `unclosed code",
		"**** empty bold and `` empty code",
		"| A | B |\n|---|---|\n| 1 | 2 |",
		"| Name | Role |\n|---|---|\n| x | admin |\n| | boss |",
		"```go\nfmt.Println(\"<hi>\")\n```",
		"```\n```",
		"| A | B |\n|---|---|\n| a \\| b | 2 |",
		"| A | B |\n|---|---|\n| `x|y` | 2 |",
		"**use `ls` now** overlapping-ish spans",
		strings.Repeat("**b** ", 200),
		strings.Repeat("x", 5000),
		"| H1 | H2 |\n|---|---|\n| v1 | v2 | v3 |",
	}
	for _, in := range corpus {
		out, ok := renderHTML(in)
		if ok {
			validateRendered(t, out)
		}
	}
}

// Per-rule unit tests (plan v2-v7).
func TestRenderTableToBullets(t *testing.T) {
	out, ok := renderHTML("| Name | Role |\n|---|---|\n| ana | admin |\n| ivo | user |")
	if !ok {
		t.Fatal("table did not render")
	}
	for _, want := range []string{"<b>ana</b>", "• Role: admin", "<b>ivo</b>", "• Role: user"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "|") {
		t.Fatalf("pipes survived rendering:\n%s", out)
	}
}

func TestRenderTableLossless(t *testing.T) {
	// empty cell -> '—'; surplus cell -> (extra)
	// 4 cells vs 2 headers: row-label consumes one, one true surplus.
	out, ok := renderHTML("| A | B |\n|---|---|\n| x | |\n| y | 2 | 3 | 4 |")
	if !ok {
		t.Fatal("table did not render")
	}
	if !strings.Contains(out, "• B: —") {
		t.Fatalf("empty cell not preserved as —:\n%s", out)
	}
	if !strings.Contains(out, "(extra): 4") {
		t.Fatalf("surplus cell dropped:\n%s", out)
	}
}

func TestRenderPipeInCellVerbatim(t *testing.T) {
	for _, in := range []string{
		"| A | B |\n|---|---|\n| a \\| b | 2 |",
		"| A | B |\n|---|---|\n| `x|y` | 2 |",
	} {
		out, ok := renderHTML(in)
		if ok && !strings.Contains(out, "|") {
			t.Fatalf("pipe-in-cell block was split (lossy): %q -> %q", in, out)
		}
	}
}

func TestRenderFenceAndSpans(t *testing.T) {
	out, ok := renderHTML("```\n<raw> & stuff\n```\nand **bold** with `c`")
	if !ok {
		t.Fatal("did not render")
	}
	for _, want := range []string{"<pre>&lt;raw&gt; &amp; stuff</pre>", "<b>bold</b>", "<code>c</code>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderEmptySpansNeverWrapped(t *testing.T) {
	out, ok := renderHTML("**** and `` but **real** stays")
	if !ok {
		t.Fatal("did not render")
	}
	if strings.Contains(out, "<b></b>") || strings.Contains(out, "<code></code>") {
		t.Fatalf("empty tag emitted:\n%s", out)
	}
	if !strings.Contains(out, "<b>real</b>") {
		t.Fatalf("real bold lost:\n%s", out)
	}
}

// BOUNDARIES (plan v5): span budget 90/91 and UTF-16 4096/4097 flip ok.
func TestRenderBudgetBoundaries(t *testing.T) {
	mk := func(n int) string { return strings.TrimSpace(strings.Repeat("**b** ", n)) }
	if _, ok := renderHTML(mk(renderSpanBudget)); !ok {
		t.Fatal("exactly-at-span-budget refused")
	}
	if _, ok := renderHTML(mk(renderSpanBudget + 1)); ok {
		t.Fatal("span budget exceeded but rendered")
	}
	// The budget applies to the RENDERED output: "**b**" renders as
	// "<b>b</b>" (8 UTF-16 units), so rendered = n + 1 + 8.
	pad := strings.Repeat("x", renderUTF16Budget-9) + " **b**"
	if _, ok := renderHTML(pad); !ok {
		t.Fatal("exactly-at-utf16-budget refused")
	}
	if _, ok := renderHTML("x" + pad); ok {
		t.Fatal("utf16 budget exceeded but rendered")
	}
}

// Plain prose short-circuits to the original path.
func TestRenderPlainProseShortCircuits(t *testing.T) {
	if _, ok := renderHTML("nothing fancy here at all"); ok {
		t.Fatal("plain prose should not take the formatted path")
	}
}
