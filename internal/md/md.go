// Package md implements a deliberately bounded Markdown parser. It is not
// CommonMark and not GitHub Flavored Markdown: nested block structure is not
// modeled, there is no escape processing, and anything outside the supported
// subset survives as literal text.
package md

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/schambon/mdv/internal/doc"
)

// listPattern matches an unordered or ordered list item. Captured indentation
// is recognized but not preserved in the rendered prefix.
var listPattern = regexp.MustCompile(`^(\s*)([-+*]|[0-9]+[.)])\s+(.*)$`)

// quotePrefix is the display prefix a renderer repeats for quoted lines.
const quotePrefix = "│ "

// line is one logical source line with the byte offset at which it begins.
type line struct {
	text  string
	start int
}

// Parse converts source bytes into a semantic document.
func Parse(src []byte) doc.Document {
	lines := splitLines(string(src))

	d := doc.Document{Size: len(src), Lines: make([]int, len(lines))}
	for i, ln := range lines {
		d.Lines[i] = ln.start
	}

	p := &parser{lines: lines, doc: d}
	p.parseBlocks()
	d.Blocks = p.blocks
	return d
}

// splitLines breaks src on LF, stripping the LF and an optional preceding CR
// while recording each line's original byte offset. A trailing LF produces an
// empty final element, which is dropped.
func splitLines(src string) []line {
	pieces := strings.SplitAfter(src, "\n")
	if len(pieces) > 1 && pieces[len(pieces)-1] == "" {
		pieces = pieces[:len(pieces)-1]
	}

	lines := make([]line, len(pieces))
	offset := 0
	for i, piece := range pieces {
		text := strings.TrimSuffix(piece, "\n")
		text = strings.TrimSuffix(text, "\r")
		lines[i] = line{text: text, start: offset}
		offset += len(piece)
	}
	return lines
}

type parser struct {
	lines  []line
	doc    doc.Document
	blocks []doc.Block
	i      int
}

// parseBlocks runs the ordered block scanner. The order of these checks is the
// grammar: it decides, for example, that an indented list item is a list rather
// than indented code.
func (p *parser) parseBlocks() {
	for p.i < len(p.lines) {
		switch {
		case p.blank():
		case p.fencedCode():
		case p.rule():
		case p.heading():
		case p.quote():
		case p.list():
		case p.table():
		case p.indentedCode():
		default:
			p.paragraph()
		}
	}
}

// emit appends a block spanning source lines [from, to).
func (p *parser) emit(b doc.Block, from, to int) {
	start := p.lines[from].start
	end := p.lines[to-1].start + len(p.lines[to-1].text)
	b.Source = p.doc.Range(start, end)
	p.blocks = append(p.blocks, b)
}

// inlines parses body, whose first byte sits at the given source offset.
func (p *parser) inlines(body string, offset int) []doc.Inline {
	return ParseInline(body, offset, p.doc.Position)
}

// text wraps a body as a single literal inline, used for code and table cells
// that carry no parsed markup of their own.
func (p *parser) text(body string, start, end int) []doc.Inline {
	return []doc.Inline{{
		Kind:   doc.InlineText,
		Text:   body,
		Source: p.doc.Range(start, end),
	}}
}

func (p *parser) blank() bool {
	if strings.TrimSpace(p.lines[p.i].text) != "" {
		return false
	}
	p.emit(doc.Block{Kind: doc.BlockBlank}, p.i, p.i+1)
	p.i++
	return true
}

// fencedCode consumes a ``` or ~~~ fence. Only the first three characters of
// the opener select the closing marker, and the language label is ignored.
func (p *parser) fencedCode() bool {
	marker, ok := fenceMarker(p.lines[p.i].text)
	if !ok {
		return false
	}

	start := p.i
	p.i++
	var body []string
	for p.i < len(p.lines) {
		if strings.HasPrefix(strings.TrimSpace(p.lines[p.i].text), marker) {
			p.i++ // consume the closer
			break
		}
		body = append(body, p.lines[p.i].text)
		p.i++
	}

	// The fence markers themselves are not part of the code.
	contentStart := p.lines[start].start + len(p.lines[start].text)
	contentEnd := contentStart
	if len(body) > 0 {
		contentEnd = p.lines[start+len(body)].start + len(p.lines[start+len(body)].text)
	}

	p.emit(doc.Block{
		Kind:    doc.BlockCode,
		Inlines: p.text(strings.Join(body, "\n"), contentStart, contentEnd),
	}, start, p.i)
	return true
}

func fenceMarker(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 3 {
		return "", false
	}
	switch trimmed[:3] {
	case "```", "~~~":
		return trimmed[:3], true
	}
	return "", false
}

// rule matches three or more of a single -, * or _ once spaces and tabs are
// removed.
func (p *parser) rule() bool {
	if !isRule(p.lines[p.i].text) {
		return false
	}
	p.emit(doc.Block{Kind: doc.BlockRule}, p.i, p.i+1)
	p.i++
	return true
}

func isRule(text string) bool {
	stripped := strings.NewReplacer(" ", "", "\t", "").Replace(text)
	if len(stripped) < 3 {
		return false
	}
	switch stripped[0] {
	case '-', '*', '_':
	default:
		return false
	}
	return strings.Count(stripped, string(stripped[0])) == len(stripped)
}

// heading matches one to six # characters at byte zero followed by a space, so
// any leading indentation prevents recognition.
func (p *parser) heading() bool {
	text := p.lines[p.i].text
	level := 0
	for level < len(text) && text[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(text) || text[level] != ' ' {
		return false
	}

	body := text[level+1:]
	p.emit(doc.Block{
		Kind:    doc.BlockHeading,
		Level:   level,
		Prefix:  text[:level+1],
		Inlines: p.inlines(body, p.lines[p.i].start+level+1),
	}, p.i, p.i+1)
	p.i++
	return true
}

// quote strips one > from a line whose first non-space character is >. Nested
// and multi-line quotes are not modeled: each line stands alone.
func (p *parser) quote() bool {
	text := p.lines[p.i].text
	trimmed := strings.TrimLeftFunc(text, isSpace)
	if !strings.HasPrefix(trimmed, ">") {
		return false
	}

	indent := len(text) - len(trimmed)
	body := strings.TrimPrefix(trimmed, ">")
	bodyOffset := p.lines[p.i].start + indent + 1
	if strings.HasPrefix(body, " ") {
		body = body[1:]
		bodyOffset++
	}

	p.emit(doc.Block{
		Kind:    doc.BlockQuote,
		Prefix:  quotePrefix,
		Inlines: p.inlines(body, bodyOffset),
	}, p.i, p.i+1)
	p.i++
	return true
}

// list matches a list item, gathering any soft-wrapped continuation lines the
// way paragraph does: subsequent lines that are neither blank nor a construct
// special recognizes (a new item, heading, fence, rule, or quote) belong to the
// item and are reflowed with it. A task marker immediately after the list marker
// is folded into the displayed prefix rather than kept as body text.
func (p *parser) list() bool {
	start := p.i
	m := listPattern.FindStringSubmatch(p.lines[p.i].text)
	if m == nil {
		return false
	}

	marker, body := m[2], m[3]
	consumed := len(p.lines[p.i].text) - len(body)
	prefix := marker + " "

	if task, rest, ok := taskMarker(body); ok {
		prefix += task
		consumed += len(body) - len(rest)
		body = rest
	}
	bodyOffset := p.lines[start].start + consumed

	parts := []string{body}
	p.i++
	for p.i < len(p.lines) {
		text := p.lines[p.i].text
		if strings.TrimSpace(text) == "" || special(text) {
			break
		}
		parts = append(parts, strings.TrimSpace(text))
		p.i++
	}

	p.emit(doc.Block{
		Kind:    doc.BlockListItem,
		Prefix:  prefix,
		Inlines: p.inlines(strings.Join(parts, " "), bodyOffset),
	}, start, p.i)
	return true
}

// taskMarker recognizes a leading "[ ] " or case-insensitive "[x] ".
func taskMarker(body string) (marker, rest string, ok bool) {
	if len(body) < 4 || body[0] != '[' || body[2] != ']' || body[3] != ' ' {
		return "", body, false
	}
	switch body[1] {
	case ' ', 'x', 'X':
		return body[:4], body[4:], true
	}
	return "", body, false
}

// table starts when a line containing a pipe is followed by a delimiter row.
// The delimiter row is consumed but not represented.
func (p *parser) table() bool {
	if p.i+1 >= len(p.lines) {
		return false
	}
	if !strings.Contains(p.lines[p.i].text, "|") || !isDelimiterRow(p.lines[p.i+1].text) {
		return false
	}

	p.emitTableRow(p.i, true)
	p.i += 2 // header plus delimiter

	for p.i < len(p.lines) &&
		strings.TrimSpace(p.lines[p.i].text) != "" &&
		strings.Contains(p.lines[p.i].text, "|") {
		p.emitTableRow(p.i, false)
		p.i++
	}
	return true
}

// emitTableRow stores each cell as a literal inline; layout re-parses cell
// contents with ParseInline once it knows the column widths.
func (p *parser) emitTableRow(index int, header bool) {
	ln := p.lines[index]
	start, end := ln.start, ln.start+len(ln.text)

	var inlines []doc.Inline
	for _, cell := range splitCells(ln.text) {
		inlines = append(inlines, doc.Inline{
			Kind:   doc.InlineText,
			Text:   cell,
			Source: p.doc.Range(start, end),
		})
	}

	p.emit(doc.Block{Kind: doc.BlockTableRow, Header: header, Inlines: inlines}, index, index+1)
}

// splitCells discards empty fields, which is what makes leading and trailing
// pipes optional.
func splitCells(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == '|' })
	cells := make([]string, 0, len(fields))
	for _, f := range fields {
		cells = append(cells, strings.TrimSpace(f))
	}
	return cells
}

// isDelimiterRow reports whether every cell is at least three hyphens with
// optional surrounding colons. Alignment colons are recognized but ignored.
func isDelimiterRow(text string) bool {
	if !strings.Contains(text, "|") {
		return false
	}
	cells := splitCells(text)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		body := strings.TrimSuffix(strings.TrimPrefix(cell, ":"), ":")
		if len(body) < 3 || strings.Count(body, "-") != len(body) {
			return false
		}
	}
	return true
}

// indentedCode consumes consecutive lines beginning with four literal spaces.
func (p *parser) indentedCode() bool {
	if !strings.HasPrefix(p.lines[p.i].text, "    ") {
		return false
	}

	start := p.i
	var body []string
	for p.i < len(p.lines) && strings.HasPrefix(p.lines[p.i].text, "    ") {
		body = append(body, p.lines[p.i].text[4:])
		p.i++
	}

	contentStart := p.lines[start].start + 4
	last := p.lines[p.i-1]
	p.emit(doc.Block{
		Kind:    doc.BlockCode,
		Inlines: p.text(strings.Join(body, "\n"), contentStart, last.start+len(last.text)),
	}, start, p.i)
	return true
}

// paragraph consumes lines until a blank line or a construct that special
// recognizes. A table or indented code does not interrupt a paragraph.
func (p *parser) paragraph() {
	start := p.i
	var parts []string
	for p.i < len(p.lines) {
		text := p.lines[p.i].text
		if strings.TrimSpace(text) == "" || (p.i > start && special(text)) {
			break
		}
		parts = append(parts, strings.TrimSpace(text))
		p.i++
	}

	first := p.lines[start]
	body := strings.Join(parts, " ")
	bodyOffset := first.start + (len(first.text) - len(strings.TrimLeftFunc(first.text, isSpace)))

	p.emit(doc.Block{
		Kind:    doc.BlockParagraph,
		Inlines: p.inlines(body, bodyOffset),
	}, start, p.i)
}

// special reports whether a line interrupts a paragraph. Tables and indented
// code are deliberately absent: they are recognized only at the start of a
// block scan.
func special(text string) bool {
	if _, ok := fenceMarker(text); ok {
		return true
	}
	if isRule(text) {
		return true
	}
	if strings.HasPrefix(strings.TrimLeftFunc(text, isSpace), ">") {
		return true
	}
	if listPattern.MatchString(text) {
		return true
	}
	// Heading: one to six # at byte zero followed by a space.
	level := 0
	for level < len(text) && text[level] == '#' {
		level++
	}
	return level > 0 && level <= 6 && level < len(text) && text[level] == ' '
}

func isSpace(r rune) bool { return unicode.IsSpace(r) }
