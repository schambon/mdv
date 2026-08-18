package highlight

import "strings"

// language is the lexer's table for one language: the markers that open
// comments, the delimiters that open strings, and the set of reserved words.
// A zero language classifies nothing, which is the correct behaviour for an
// unknown fence info string.
type language struct {
	lineComment          []string // openers such as "//" or "#"
	commentNeedsBoundary bool     // line comment must start a word (shell "#")
	blockStart           string   // "" when the language has no block comment
	blockEnd             string
	quotes               string // single-line string delimiters
	multiQuotes          string // line-spanning delimiters (Go raw, JS template)
	tripleQuotes         bool   // Python """ and '''
	keywords             map[string]bool
}

// empty reports a language with no rules at all, so the lexer can shortcut to
// all-plain output.
func (l language) empty() bool {
	return len(l.lineComment) == 0 && l.blockStart == "" &&
		l.quotes == "" && l.multiQuotes == "" && !l.tripleQuotes &&
		len(l.keywords) == 0
}

// lookup resolves a fence info string to a language, folding aliases. An
// unrecognised name returns the zero language.
func lookup(lang string) language {
	switch lang {
	case "go", "golang":
		return goLang
	case "js", "javascript", "jsx", "mjs", "cjs":
		return jsLang
	case "ts", "typescript", "tsx":
		return tsLang
	case "python", "py":
		return pyLang
	case "c", "h":
		return cLang
	case "cpp", "c++", "cc", "cxx", "hpp":
		return cppLang
	case "java":
		return javaLang
	case "rust", "rs":
		return rustLang
	case "sh", "bash", "shell", "zsh":
		return shLang
	case "json":
		return jsonLang
	default:
		return language{}
	}
}

// words turns a whitespace-separated list into a keyword set.
func words(list string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(list) {
		set[w] = true
	}
	return set
}

var goLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	multiQuotes: "`",
	keywords: words(`break default func interface select case defer go map
		struct chan else goto package switch const fallthrough if range type
		continue for import return var nil true false iota`),
}

var jsLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	multiQuotes: "`",
	keywords: words(`break case catch class const continue debugger default
		delete do else export extends finally for function if import in
		instanceof new return super switch this throw try typeof var void
		while with yield let static async await of get set null true false
		undefined`),
}

var tsLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	multiQuotes: "`",
	keywords: words(`break case catch class const continue debugger default
		delete do else export extends finally for function if import in
		instanceof new return super switch this throw try typeof var void
		while with yield let static async await of get set null true false
		undefined interface type enum implements private public protected
		readonly abstract as namespace declare keyof infer never unknown any
		string number boolean object`),
}

var pyLang = language{
	lineComment:  []string{"#"},
	quotes:       `"'`,
	tripleQuotes: true,
	keywords: words(`False None True and as assert async await break class
		continue def del elif else except finally for from global if import
		in is lambda nonlocal not or pass raise return try while with yield
		match case self`),
}

var cLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	keywords: words(`auto break case char const continue default do double
		else enum extern float for goto if inline int long register restrict
		return short signed sizeof static struct switch typedef union unsigned
		void volatile while _Bool _Complex bool true false NULL`),
}

var cppLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	keywords: words(`alignas alignof and asm auto bool break case catch char
		char16_t char32_t class const constexpr const_cast continue decltype
		default delete do double dynamic_cast else enum explicit export extern
		false float for friend goto if inline int long mutable namespace new
		noexcept nullptr operator private protected public register
		reinterpret_cast return short signed sizeof static static_assert
		static_cast struct switch template this throw true try typedef typeid
		typename union unsigned using virtual void volatile wchar_t while`),
}

var javaLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	keywords: words(`abstract assert boolean break byte case catch char class
		const continue default do double else enum extends final finally float
		for goto if implements import instanceof int interface long native new
		package private protected public return short static strictfp super
		switch synchronized this throw throws transient try void volatile while
		true false null var record sealed permits yield`),
}

var rustLang = language{
	lineComment: []string{"//"},
	blockStart:  "/*",
	blockEnd:    "*/",
	quotes:      `"'`,
	keywords: words(`as async await break const continue crate dyn else enum
		extern false fn for if impl in let loop match mod move mut pub ref
		return self Self static struct super trait true type unsafe use where
		while union`),
}

var shLang = language{
	lineComment:          []string{"#"},
	commentNeedsBoundary: true,
	quotes:               `"'`,
	keywords: words(`if then else elif fi for while until do done case esac in
		function select time coproc return break continue local export readonly
		declare typeset unset shift echo printf read cd exit test`),
}

var jsonLang = language{
	quotes:   `"`,
	keywords: words(`true false null`),
}
