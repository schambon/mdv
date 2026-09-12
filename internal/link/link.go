// Package link decides which link targets are safe to make clickable and
// encodes them as OSC 8 hyperlinks, and classifies a target so the viewer can
// follow it: an external URL is handed to the browser, a relative target is
// resolved against the directory of the document it was read from.
package link

import (
	"net/url"
	"strings"
)

// Kind says how a link target can be followed.
type Kind int

const (
	// KindNone is a target mdv will not follow: empty, carrying control
	// characters, or naming a scheme that is not trusted.
	KindNone Kind = iota
	// KindExternal is an http, https or mailto URL, handed to the browser.
	KindExternal
	// KindRelative is a path, resolved against the directory of the document
	// the link was read from.
	KindRelative
)

// Classify decides how a target should be followed. For KindRelative it also
// returns the path to resolve: the fragment is dropped, since mdv has no
// in-document anchors, and the remainder is percent-decoded, so a link written
// as "my%20notes.md" names the file it actually means. That path is empty for a
// target that is nothing but a fragment.
//
// A target with any other scheme is KindNone rather than a relative path:
// "file:///etc/passwd" is not a filename, and guessing at one would be worse
// than refusing.
func Classify(target string) (Kind, string) {
	if target == "" || hasControl(target) {
		return KindNone, ""
	}
	u, err := url.Parse(target)
	if err != nil {
		return KindNone, ""
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		return KindExternal, ""
	case "":
		// No scheme means a path; fall through.
	default:
		return KindNone, ""
	}
	// A network-path reference ("//host/x") names a host, not a local file.
	if strings.HasPrefix(target, "//") {
		return KindNone, ""
	}

	path := target
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path = path[:i]
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		// A malformed escape is not a path mdv can trust.
		return KindNone, ""
	}
	return KindRelative, decoded
}

const (
	osc = "\x1b]8;;"
	st  = "\x1b\\"
)

// Valid reports whether a target may be emitted as a hyperlink. Only absolute
// http, https and mailto URLs qualify: the terminal has no idea which
// directory the document came from, so a relative target means nothing to it,
// and other schemes are not trusted.
//
// This is a narrower question than Classify's, and the two are meant to
// disagree. mdv follows a relative target itself, resolving it against the
// document's directory; it just does not ask the terminal to.
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
