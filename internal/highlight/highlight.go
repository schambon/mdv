// Package highlight is a small, deliberately bounded syntax highlighter for
// fenced code blocks. It is not a parser: a single generic C-family lexer,
// driven by a per-language table of comment markers, string delimiters and
// keywords, splits a block of code into coarse tokens — keyword, string,
// comment, number, or plain. An unknown language yields all-plain tokens, so
// the renderer draws it exactly as it would have without highlighting.
//
// It sits below layout and depends on nothing but the standard library.
package highlight

import "strings"

// TokenKind is the visual role of a token. Plain covers everything the lexer
// does not classify, including punctuation and identifiers.
type TokenKind int

const (
	Plain TokenKind = iota
	Keyword
	String
	Comment
	Number
)

// Token is a run of code sharing one kind. A token never contains a newline;
// line boundaries are represented by the slicing in Lines.
type Token struct {
	Kind TokenKind
	Text string
}

// Lines tokenises text into one token slice per physical line, splitting on
// "\n" exactly as strings.Split would. State — an open block comment or a
// multiline string — carries across line boundaries, so a token that spans a
// newline is cut at it and continues on the next line's slice.
//
// The concatenation of a line's token texts equals that line, so a renderer can
// rebuild the source from the tokens alone. An unknown or empty language maps
// every line to a single Plain token.
func Lines(lang, text string) [][]Token {
	l := &lexer{cfg: lookup(lang)}
	l.scan(text)
	return l.finish()
}

// lexer scans a whole block, accumulating tokens for the current line in cur
// and finalising a line into lines at each newline.
type lexer struct {
	cfg   language
	cur   []Token
	lines [][]Token
}

func (l *lexer) scan(text string) {
	// The all-plain fast path: a language with no rules cannot classify
	// anything, so every line is one Plain token.
	if l.cfg.empty() {
		l.pushPlain(text)
		return
	}

	n := len(text)
	for i := 0; i < n; {
		c := text[i]
		switch {
		case c == '\n':
			l.newline()
			i++
		case l.cfg.blockStart != "" && strings.HasPrefix(text[i:], l.cfg.blockStart):
			i = l.consumeBlockComment(text, i)
		case l.lineCommentAt(text, i):
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				end = n - i
			}
			l.push(Comment, text[i:i+end])
			i += end
		case l.cfg.tripleQuotes && tripleAt(text, i) != "":
			i = l.consumeTriple(text, i, tripleAt(text, i))
		case strings.IndexByte(l.cfg.multiQuotes, c) >= 0:
			i = l.consumeString(text, i, c, false, true)
		case strings.IndexByte(l.cfg.quotes, c) >= 0:
			i = l.consumeString(text, i, c, true, false)
		case isDigit(c) || (c == '.' && i+1 < n && isDigit(text[i+1])):
			j := scanNumber(text, i)
			l.push(Number, text[i:j])
			i = j
		case isIdentStart(c):
			j := scanIdent(text, i)
			word := text[i:j]
			if l.cfg.keywords[word] {
				l.push(Keyword, word)
			} else {
				l.push(Plain, word)
			}
			i = j
		default:
			l.push(Plain, text[i:i+1])
			i++
		}
	}
}

// lineCommentAt reports whether a line comment opens at i. Shell's "#" only
// starts a comment at the beginning of a word, so with needsBoundary set it
// must sit at column zero or follow whitespace — otherwise "$#" or "a#b" would
// swallow the rest of the line.
func (l *lexer) lineCommentAt(text string, i int) bool {
	matched := false
	for _, m := range l.cfg.lineComment {
		if strings.HasPrefix(text[i:], m) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	if l.cfg.commentNeedsBoundary && i > 0 {
		switch text[i-1] {
		case ' ', '\t', '\n':
		default:
			return false
		}
	}
	return true
}

// consumeString reads a delimited string starting at its opening quote. With
// escapes set, a backslash escapes the next byte. A single-line string that
// reaches a newline is closed there (an unterminated literal), while a
// multiline one carries the newline into the token, to be cut by push.
func (l *lexer) consumeString(text string, i int, quote byte, escapes, multiline bool) int {
	n := len(text)
	start := i
	i++ // opening quote
	for i < n {
		c := text[i]
		switch {
		case escapes && c == '\\' && i+1 < n:
			i += 2
		case c == '\n' && !multiline:
			l.push(String, text[start:i])
			return i
		case c == quote:
			i++
			l.push(String, text[start:i])
			return i
		default:
			i++
		}
	}
	l.push(String, text[start:i])
	return i
}

// consumeTriple reads a Python triple-quoted string, which always spans lines
// until the matching triple appears.
func (l *lexer) consumeTriple(text string, i int, q string) int {
	n := len(text)
	start := i
	i += len(q)
	for i < n {
		if text[i] == '\\' && i+1 < n {
			i += 2
			continue
		}
		if strings.HasPrefix(text[i:], q) {
			i += len(q)
			l.push(String, text[start:i])
			return i
		}
		i++
	}
	l.push(String, text[start:i])
	return i
}

// consumeBlockComment reads from a block-comment opener to its closer, or to
// the end of the text if the comment is never closed.
func (l *lexer) consumeBlockComment(text string, i int) int {
	end := strings.Index(text[i:], l.cfg.blockEnd)
	if end < 0 {
		l.push(Comment, text[i:])
		return len(text)
	}
	stop := i + end + len(l.cfg.blockEnd)
	l.push(Comment, text[i:stop])
	return stop
}

// push appends text of one kind to the current line, splitting at every newline
// so no token straddles a line boundary.
func (l *lexer) push(kind TokenKind, s string) {
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			if s != "" {
				l.add(kind, s)
			}
			return
		}
		if i > 0 {
			l.add(kind, s[:i])
		}
		l.newline()
		s = s[i+1:]
	}
}

// pushPlain emits the whole text as Plain tokens, one line at a time.
func (l *lexer) pushPlain(text string) {
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			if text != "" {
				l.add(Plain, text)
			}
			return
		}
		if i > 0 {
			l.add(Plain, text[:i])
		}
		l.newline()
		text = text[i+1:]
	}
}

// add appends a token, coalescing with the previous one when they share a kind
// so adjacent plain bytes do not fragment.
func (l *lexer) add(kind TokenKind, s string) {
	if n := len(l.cur); n > 0 && l.cur[n-1].Kind == kind {
		l.cur[n-1].Text += s
		return
	}
	l.cur = append(l.cur, Token{Kind: kind, Text: s})
}

func (l *lexer) newline() {
	l.lines = append(l.lines, l.cur)
	l.cur = nil
}

// finish flushes the final line, which has no trailing newline to close it.
func (l *lexer) finish() [][]Token {
	l.lines = append(l.lines, l.cur)
	l.cur = nil
	return l.lines
}

func tripleAt(text string, i int) string {
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(text[i:], q) {
			return q
		}
	}
	return ""
}

// scanNumber consumes a numeric literal: digit groups, a radix prefix, a
// fractional point, digit separators, and an exponent sign. It is generous on
// purpose — every byte it swallows is still coloured as a number.
func scanNumber(text string, i int) int {
	n := len(text)
	j := i
	for j < n {
		c := text[j]
		switch {
		case isDigit(c) || isHexLetter(c) || c == '.' || c == '_' ||
			c == 'x' || c == 'X' || c == 'o' || c == 'O' || c == 'b' || c == 'B':
			j++
		case (c == '+' || c == '-') && j > i && (text[j-1] == 'e' || text[j-1] == 'E'):
			j++
		default:
			return j
		}
	}
	return j
}

func scanIdent(text string, i int) int {
	j := i
	for j < len(text) && isIdentPart(text[j]) {
		j++
	}
	return j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexLetter(c byte) bool {
	return (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }
