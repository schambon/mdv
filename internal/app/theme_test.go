package app

import (
	"strings"
	"testing"

	"github.com/schambon/mdv/internal/style"
)

// darkHeadingSGR and lightHeadingSGR are the heading parameters each palette
// emits; a theme swap must change which one a frame carries.
const (
	darkHeadingSGR  = "\x1b[1;36m"
	lightHeadingSGR = "\x1b[1;38;5;24m"
)

// colourApp is newApp with colour on, since the theme key is a no-op without it.
func colourApp(t *testing.T, contents string, term *fakeTerminal) *App {
	t.Helper()
	a := newApp(t, contents, term)
	a.cfg.Color = true
	a.styler = style.New(style.ThemeDark, true)
	return a
}

func TestToggleThemeSwapsPalette(t *testing.T) {
	a := colourApp(t, "# Heading\n", newFake())

	if got := a.styler.Theme(); got != style.ThemeDark {
		t.Fatalf("initial theme = %q, want dark", got)
	}

	a.handle(key('t'))
	if got := a.styler.Theme(); got != style.ThemeLight {
		t.Errorf("after one toggle theme = %q, want light", got)
	}
	if a.message != "theme: light" {
		t.Errorf("message = %q, want %q", a.message, "theme: light")
	}
	if err := a.draw(); err != nil {
		t.Fatal(err)
	}
	if frame := a.term.(*fakeTerminal).lastFrame(); !strings.Contains(frame, lightHeadingSGR) {
		t.Errorf("light frame is missing the light heading sequence")
	}

	a.handle(key('t'))
	if got := a.styler.Theme(); got != style.ThemeDark {
		t.Errorf("after two toggles theme = %q, want dark", got)
	}
	if err := a.draw(); err != nil {
		t.Fatal(err)
	}
	if frame := a.term.(*fakeTerminal).lastFrame(); !strings.Contains(frame, darkHeadingSGR) {
		t.Errorf("dark frame is missing the dark heading sequence")
	}
}

func TestToggleThemeIsANoOpWithoutColour(t *testing.T) {
	// newApp leaves Color off.
	a := newApp(t, "# Heading\n", newFake())

	before := a.styler.Theme()
	a.handle(key('t'))

	if a.styler.Theme() != before {
		t.Errorf("theme changed under --no-color: %q → %q", before, a.styler.Theme())
	}
	if a.message != "colour is off" {
		t.Errorf("message = %q, want %q", a.message, "colour is off")
	}
}

func TestResolveThemeUsesTerminalWhenAuto(t *testing.T) {
	term := newFake()
	term.bgOK, term.bgDark = true, false // a light background
	a := newApp(t, "# Heading\n", term)
	a.cfg.Theme = style.ThemeAuto
	a.cfg.Color = true

	a.resolveTheme()

	if got := a.styler.Theme(); got != style.ThemeLight {
		t.Errorf("resolved theme = %q, want light (terminal answered light)", got)
	}
	if term.queries != 1 {
		t.Errorf("queries = %d, want 1", term.queries)
	}
}

func TestResolveThemeSkipsQueryForExplicitTheme(t *testing.T) {
	term := newFake()
	term.bgOK, term.bgDark = true, false
	a := newApp(t, "# Heading\n", term)
	a.cfg.Theme = style.ThemeLight // explicit, not auto

	a.resolveTheme()

	if term.queries != 0 {
		t.Errorf("queries = %d, want 0: an explicit theme must not probe", term.queries)
	}
}
