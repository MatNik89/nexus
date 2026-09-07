//go:build linux

// render.go — outbound Telegram HTML rendering (tgout plan v17).
//
// The SHAPE is stolen from the owner's local hermes-agent
// (gateway/platforms/helpers.py convert_table_to_bullets +
// _render_table_block): every GFM pipe table becomes bold-heading +
// bullet row groups. Deliberate deviations (plan): parse_mode=HTML
// instead of MarkdownV2 (3 escapes, no placeholder engine), lossless
// cells ('—' for empty, '(extra)' bullets for surplus; hermes pads and
// truncates), and NO second send — renderHTML returns (html, true)
// only when the result is constructively valid and within budgets;
// (_, false) means "send the ORIGINAL exactly as today".
package telegram

import (
	"html"
	"strings"
	"unicode/utf16"
)

const (
	renderSpanBudget = 90 // OPENING tags — a LOCAL renderer-complexity
	// budget, NOT a claim about any Telegram entity ceiling
	// (auto-detected entities exist and are not counted here).
	renderUTF16Budget = 4096 // Bot API sendMessage bound (UTF-16 units)
)

// renderHTML renders original to Telegram HTML. ok=false -> caller
// sends the ORIGINAL with no parse_mode (today's path, unchanged).
func renderHTML(original string) (string, bool) {
	if strings.TrimSpace(original) == "" {
		return "", false
	}
	out, spans, formatted := renderBlocks(original)
	if !formatted {
		return "", false // nothing to format: plain prose short-circuits
	}
	if spans > renderSpanBudget {
		return "", false
	}
	if len(utf16.Encode([]rune(out))) > renderUTF16Budget {
		return "", false
	}
	return out, true
}

// renderBlocks handles fences and tables at block level, spans inside.
func renderBlocks(src string) (string, int, bool) {
	lines := strings.Split(src, "\n")
	var out []string
	spans := 0
	formatted := false
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		// Fenced code block -> <pre> (content escaped, verbatim).
		if strings.HasPrefix(trimmed, "```") {
			j := i + 1
			var body []string
			for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				body = append(body, lines[j])
				j++
			}
			if j < len(lines) { // closed fence
				content := strings.Join(body, "\n")
				if content != "" { // EMPTY SPANS ARE NEVER WRAPPED
					out = append(out, "<pre>"+html.EscapeString(content)+"</pre>")
					spans++
					formatted = true
				} else {
					out = append(out, html.EscapeString(strings.Join(lines[i:j+1], "\n")))
				}
				i = j + 1
				continue
			}
			// unclosed fence: literal escaped text
			out = append(out, html.EscapeString(line))
			i++
			continue
		}
		// Table block: header | divider | rows.
		if strings.Contains(line, "|") && i+1 < len(lines) && isTableDivider(lines[i+1]) {
			block := []string{line, lines[i+1]}
			j := i + 2
			for j < len(lines) && looksLikeTableRow(lines[j]) {
				block = append(block, lines[j])
				j++
			}
			rendered, n, ok := renderTableBlock(block)
			if ok {
				out = append(out, rendered)
				spans += n
				formatted = true
				i = j
				continue
			}
			// PIPES-IN-CELL or unparseable: whole block verbatim,
			// escaped (lossless beats pretty).
			out = append(out, html.EscapeString(strings.Join(block, "\n")))
			i = j
			continue
		}
		rendered, n := renderSpans(line)
		if n > 0 {
			formatted = true
		}
		spans += n
		out = append(out, rendered)
		i++
	}
	return strings.Join(out, "\n"), spans, formatted
}

func isTableDivider(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "-") || !strings.Contains(t, "|") {
		return false
	}
	for _, c := range t {
		switch c {
		case '|', '-', ':', ' ':
		default:
			return false
		}
	}
	return true
}

func looksLikeTableRow(line string) bool {
	return strings.Contains(line, "|") && strings.TrimSpace(line) != ""
}

func splitCells(row string) []string {
	s := strings.TrimSpace(row)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// renderTableBlock: bold-heading + bullet groups (hermes shape,
// lossless deviations). ok=false -> caller emits the block verbatim.
func renderTableBlock(block []string) (string, int, bool) {
	if len(block) < 3 {
		return "", 0, false
	}
	joined := strings.Join(block, "\n")
	// PIPES INSIDE CELL CONTENT (plan r6 codex MED): escaped pipe or a
	// backtick span containing '|' -> do not split, verbatim.
	if strings.Contains(joined, `\|`) || backtickSpanHasPipe(joined) {
		return "", 0, false
	}
	headers := splitCells(block[0])
	if len(headers) < 2 {
		return "", 0, false
	}
	spans := 0
	var groups []string
	for idx, row := range block[2:] {
		cells := splitCells(row)
		heading := ""
		headingIdx := -1 // index INTO data of the cell promoted to heading
		var data []string
		if len(cells) == len(headers)+1 && cells[0] != "" {
			heading, data = cells[0], cells[1:]
		} else {
			for hi, c := range cells {
				if c != "" {
					heading, headingIdx = c, hi
					break
				}
			}
			data = cells
		}
		if heading == "" {
			heading = "Row " + itoa(idx+1)
		}
		var b strings.Builder
		b.WriteString("<b>" + html.EscapeString(heading) + "</b>\n")
		spans++
		for ci, cell := range data {
			label := "(extra)"
			if ci < len(headers) {
				label = headers[ci]
			}
			val := cell
			if val == "" {
				val = "—" // lossless: empty cell -> placeholder
			}
			if ci == headingIdx {
				continue // skip ONLY the promoted heading cell itself
				// (impl codex #2: equal VALUES elsewhere are real data)
			}
			b.WriteString("• " + html.EscapeString(label) + ": " + html.EscapeString(val) + "\n")
		}
		groups = append(groups, strings.TrimRight(b.String(), "\n"))
	}
	if len(groups) == 0 {
		return "", 0, false
	}
	return strings.Join(groups, "\n"), spans, true
}

func backtickSpanHasPipe(s string) bool {
	in := false
	for _, c := range s {
		switch {
		case c == '`':
			in = !in
		case c == '|' && in:
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// renderSpans: tokenizer precedence inline code > bold, first-match
// wins, spans never overlap; empty spans never wrapped; everything
// else escaped. Returns rendered line + opening-tag count.
func renderSpans(line string) (string, int) {
	var b strings.Builder
	spans := 0
	rest := line
	for rest != "" {
		ci := strings.Index(rest, "`")
		bi := strings.Index(rest, "**")
		switch {
		case ci >= 0 && (bi < 0 || ci <= bi):
			end := strings.Index(rest[ci+1:], "`")
			if end < 0 {
				b.WriteString(html.EscapeString(rest))
				return b.String(), spans
			}
			content := rest[ci+1 : ci+1+end]
			b.WriteString(html.EscapeString(rest[:ci]))
			if content == "" { // empty span: markers become literal
				b.WriteString(html.EscapeString("``"))
			} else {
				b.WriteString("<code>" + html.EscapeString(content) + "</code>")
				spans++
			}
			rest = rest[ci+1+end+1:]
		case bi >= 0:
			end := strings.Index(rest[bi+2:], "**")
			if end < 0 {
				b.WriteString(html.EscapeString(rest))
				return b.String(), spans
			}
			content := rest[bi+2 : bi+2+end]
			b.WriteString(html.EscapeString(rest[:bi]))
			if content == "" {
				b.WriteString(html.EscapeString("****"))
			} else {
				b.WriteString("<b>" + html.EscapeString(content) + "</b>")
				spans++
			}
			rest = rest[bi+2+end+2:]
		default:
			b.WriteString(html.EscapeString(rest))
			return b.String(), spans
		}
	}
	return b.String(), spans
}
