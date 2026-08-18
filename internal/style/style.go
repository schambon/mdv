// Package style maps layout's semantic styles onto ANSI escape sequences and
// composes them with OSC 8 hyperlinks.
package style

import (
	"os"
	"strconv"
	"strings"

	"github.com/schambon/mdv/internal/layout"
	"github.com/schambon/mdv/internal/link"
)

// Theme names a palette.
type Theme string

const (
	ThemeAuto  Theme = "auto"
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

const reset = "\x1b[0m"

// palette maps every style to its SGR parameters.
type palette map[layout.Style]string

// darkPalette targets a dark background: bright foregrounds, low-contrast
// furniture.
var darkPalette = palette{
	layout.StyleNone:         "",
	layout.StyleHeading:      "1;36", // bold cyan
	layout.StyleEmphasis:     "3",    // italic
	layout.StyleStrong:       "1",    // bold
	layout.StyleStrike:       "9",    // crossed out
	layout.StyleCode:         "38;5;250",
	layout.StyleInlineCode:   "38;5;211", // pink
	layout.StyleQuote:        "38;5;245",
	layout.StyleRule:         "38;5;240",
	layout.StyleLink:         "4;34",   // underlined blue
	layout.StyleSearch:       "30;43",  // black on yellow
	layout.StyleSearchActive: "30;103", // black on bright yellow
	layout.StyleStatus:       "7",      // reverse video

	// Diff colours are backgrounds, not foregrounds: the whole line is the
	// unit of change, and a background survives the syntax colouring a line
	// may carry. The Word variants are a step brighter so the part that
	// actually differs stands out from the line holding it.
	layout.StyleDiffAdd:        "48;5;22",
	layout.StyleDiffRemove:     "48;5;52",
	layout.StyleDiffAddWord:    "48;5;28",
	layout.StyleDiffRemoveWord: "48;5;88",
	layout.StyleDiffGutter:     "38;5;240",
	layout.StyleDiffFold:       "38;5;240",
}

// lightPalette targets a light background: darker foregrounds so text stays
// legible against white.
var lightPalette = palette{
	layout.StyleNone:         "",
	layout.StyleHeading:      "1;38;5;24", // bold dark cyan
	layout.StyleEmphasis:     "3",
	layout.StyleStrong:       "1",
	layout.StyleStrike:       "9",
	layout.StyleCode:         "38;5;238",
	layout.StyleInlineCode:   "38;5;162", // deep pink
	layout.StyleQuote:        "38;5;242",
	layout.StyleRule:         "38;5;250",
	layout.StyleLink:         "4;38;5;25", // underlined dark blue
	layout.StyleSearch:       "30;43",
	layout.StyleSearchActive: "30;103",
	layout.StyleStatus:       "7",

	// Pale washes, so dark text stays legible on top of them.
	layout.StyleDiffAdd:        "48;5;194",
	layout.StyleDiffRemove:     "48;5;224",
	layout.StyleDiffAddWord:    "48;5;157",
	layout.StyleDiffRemoveWord: "48;5;217",
	layout.StyleDiffGutter:     "38;5;247",
	layout.StyleDiffFold:       "38;5;250",
}

// Styler renders spans. The zero value is not usable; call New.
type Styler struct {
	theme   Theme
	enabled bool
	palette palette
}

// New resolves a theme name and returns a Styler. Colour may be disabled
// independently of the theme; hyperlinks are emitted either way, since a link
// is still useful on a monochrome terminal. An auto theme is resolved from the
// environment alone; callers with a live terminal should resolve it through
// Detect first and pass the concrete result here.
func New(theme Theme, enabled bool) Styler {
	resolved := theme
	if theme == ThemeAuto {
		resolved = Detect(nil)
	}
	p := darkPalette
	if resolved == ThemeLight {
		p = lightPalette
	}
	return Styler{theme: resolved, enabled: enabled, palette: p}
}

// Theme reports the resolved theme, with auto already decided.
func (s Styler) Theme() Theme { return s.theme }

// Detect resolves an auto theme to a concrete one. It first asks the terminal
// through query, an OSC 11 background probe, and falls back to COLORFGBG when
// the terminal does not answer. query may be nil, which skips the probe — the
// path taken when there is no terminal to ask. Dark is the floor throughout,
// since it is the safer default for an unknown background.
func Detect(query func() (dark bool, ok bool)) Theme {
	if query != nil {
		if dark, ok := query(); ok {
			if dark {
				return ThemeDark
			}
			return ThemeLight
		}
	}
	return detectEnv()
}

// detectEnv infers the background from COLORFGBG, which terminals set as
// "foreground;background" with an ANSI colour index. Dark backgrounds are the
// safer default when the variable is absent or unparseable.
func detectEnv() Theme {
	value, ok := os.LookupEnv("COLORFGBG")
	if !ok {
		return ThemeDark
	}
	fields := strings.Split(value, ";")
	background, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return ThemeDark
	}
	// 0 and 8 are black, 1-6 are dark; 7 and 9-15 are light.
	if background == 0 || background == 8 || (background >= 1 && background <= 6) {
		return ThemeDark
	}
	return ThemeLight
}

// Apply wraps text in the SGR sequence for a style.
func (s Styler) Apply(text string, style layout.Style) string {
	if !s.enabled || text == "" {
		return text
	}
	params, ok := s.palette[style]
	if !ok || params == "" {
		return text
	}
	return "\x1b[" + params + "m" + text + reset
}

// Span renders one span: its background and style, then its hyperlink if it
// has a safe target.
func (s Styler) Span(span layout.Span) string {
	text := s.paint(span.Text, span.Background, span.Style)
	if span.LinkTarget != "" {
		text = link.Wrap(text, span.LinkTarget)
	}
	return text
}

// paint wraps text in one SGR sequence carrying the background parameters
// followed by the foreground ones.
//
// The two are emitted together rather than nested because Apply appends a
// reset, and a nested reset would clear the background halfway through the
// span. The order matters as well: a search hit's style is "30;43", which
// carries its own background in the foreground slot, so putting the
// foreground last lets a match stay visible on top of a diff band.
func (s Styler) paint(text string, background, style layout.Style) string {
	if !s.enabled || text == "" {
		return text
	}

	params := joinParams(s.palette[background], s.palette[style])
	if params == "" {
		return text
	}
	return "\x1b[" + params + "m" + text + reset
}

// joinParams concatenates SGR parameter lists, skipping empty ones so a style
// that maps to nothing cannot produce a stray separator.
func joinParams(values ...string) string {
	var set []string
	for _, v := range values {
		if v != "" {
			set = append(set, v)
		}
	}
	return strings.Join(set, ";")
}

// Line renders a whole row of spans.
func (s Styler) Line(line layout.RenderedLine) string {
	var sb strings.Builder
	for _, span := range line.Spans {
		sb.WriteString(s.Span(span))
	}
	return sb.String()
}
