package highlight

import (
	"strings"
	"testing"
)

// concat rebuilds the text of one tokenised line.
func concat(line []Token) string {
	var sb strings.Builder
	for _, t := range line {
		sb.WriteString(t.Text)
	}
	return sb.String()
}

// The core invariant: the tokens of each line reconstruct that line exactly,
// and the line count matches strings.Split. Checked across every language for a
// spread of real code so multiline strings and block comments are exercised.
func TestReconstruction(t *testing.T) {
	samples := map[string]string{
		"go":     "package main\n\nimport \"fmt\"\n\n// greet says hello\nfunc main() {\n\ts := `raw\nstring`\n\tfmt.Println(s, 42, 0x1F)\n}\n",
		"js":     "const x = `template\nliteral`;\n// comment\nlet n = 3.14e2; /* block\ncomment */ foo('bar');\n",
		"python": "def f(x):\n    \"\"\"doc\n    string\"\"\"\n    return x + 1  # trailing\n",
		"c":      "int main(void) {\n\t/* multi\n\tline */ char *s = \"hi\";\n\treturn 0;\n}\n",
		"rust":   "fn main() {\n    let s = \"x\";\n    let c = 'a';\n    println!(\"{}\", 1_000);\n}\n",
		"sh":     "#!/bin/sh\nfor f in *.go; do\n\techo \"$f\" # note\ndone\n",
		"json":   "{\n  \"key\": [true, false, null, 1.5],\n  \"s\": \"v\"\n}\n",
		"":       "arbitrary text\nwith no language\n",
		"cobol":  "unknown language\nfalls through\n",
	}
	for lang, text := range samples {
		lines := Lines(lang, text)
		src := strings.Split(text, "\n")
		if len(lines) != len(src) {
			t.Fatalf("%s: got %d lines, want %d", lang, len(lines), len(src))
		}
		for i, line := range lines {
			if got := concat(line); got != src[i] {
				t.Errorf("%s line %d: reconstructed %q, want %q", lang, i, got, src[i])
			}
		}
	}
}

// An unknown language, and the empty language, must yield exactly one Plain
// token per non-empty line — the renderer relies on this to draw such code
// identically to the pre-highlighting behaviour.
func TestUnknownIsAllPlain(t *testing.T) {
	for _, lang := range []string{"", "cobol", "brainfuck"} {
		lines := Lines(lang, "int x = 1; // not a comment here\nreturn\n")
		for i, line := range lines {
			for _, tok := range line {
				if tok.Kind != Plain {
					t.Errorf("lang %q line %d: token %q kind %v, want Plain", lang, i, tok.Text, tok.Kind)
				}
			}
		}
	}
}

// kindsOf reports the kind assigned to a given substring on the first line.
func firstLineKinds(lang, text string) []Token {
	return Lines(lang, text)[0]
}

func hasToken(line []Token, text string, kind TokenKind) bool {
	for _, t := range line {
		if t.Text == text && t.Kind == kind {
			return true
		}
	}
	return false
}

func TestClassification(t *testing.T) {
	cases := []struct {
		lang, code, text string
		kind             TokenKind
	}{
		{"go", "func main() {", "func", Keyword},
		{"go", "x := 42", "42", Number},
		{"go", `s := "hi"`, `"hi"`, String},
		{"go", "// note", "// note", Comment},
		{"python", "def f():", "def", Keyword},
		{"python", "x = 0xFF", "0xFF", Number},
		{"json", "[true]", "true", Keyword},
		{"rust", "let x = 1;", "let", Keyword},
		{"sh", "if true; then", "if", Keyword},
	}
	for _, c := range cases {
		line := firstLineKinds(c.lang, c.code)
		if !hasToken(line, c.text, c.kind) {
			t.Errorf("%s %q: expected token %q of kind %v, got %+v", c.lang, c.code, c.text, c.kind, line)
		}
	}
}

// Shell's "#" only opens a comment at a word boundary, so "$#" keeps its hash.
func TestShellHashBoundary(t *testing.T) {
	line := firstLineKinds("sh", `echo "$#" # real comment`)
	for _, tok := range line {
		if tok.Kind == Comment && strings.Contains(tok.Text, "$#") {
			t.Errorf("$# wrongly swallowed into a comment: %+v", line)
		}
	}
	if !hasToken(line, "# real comment", Comment) {
		t.Errorf("real comment not detected: %+v", line)
	}
}

// A block comment that never closes runs to the end of the block without
// panicking, and every line still reconstructs.
func TestUnterminatedBlockComment(t *testing.T) {
	text := "code\n/* open\nnever closed\n"
	lines := Lines("go", text)
	src := strings.Split(text, "\n")
	for i, line := range lines {
		if concat(line) != src[i] {
			t.Fatalf("line %d mismatch", i)
		}
	}
}
