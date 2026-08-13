// Package link decides which link targets are safe to make clickable and
// encodes them as OSC 8 hyperlinks. mdv never opens a link itself; it only
// hands the target to the terminal.
package link

import (
	"net/url"
	"strings"
)

const (
	osc = "\x1b]8;;"
	st  = "\x1b\\"
)

// Valid reports whether a target may be emitted as a hyperlink. Only absolute
// http, https and mailto URLs qualify: relative targets are not resolved
// against the source directory, and other schemes are not trusted.
func Valid(target string) bool {
	if target == "" || hasControl(target) {
		return false
	}
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// hasControl reports whether s contains a C0 control character or DEL, either
// of which could let a target escape the hyperlink sequence.
func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7F
	})
}

// Wrap makes label clickable if target is safe, and returns label unchanged
// otherwise.
func Wrap(label, target string) string {
	if !Valid(target) {
		return label
	}
	return Open(target) + label + Close()
}

// Open starts a hyperlink. Callers must pair it with Close.
func Open(target string) string {
	return osc + target + st
}

// Close ends a hyperlink.
func Close() string {
	return osc + st
}
