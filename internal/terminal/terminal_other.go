//go:build !darwin

package terminal

import (
	"fmt"
	"runtime"
)

// New reports that this platform has no terminal backend. mdv drives termios
// directly and has only been implemented for macOS.
func New() (Terminal, error) {
	return nil, fmt.Errorf("mdv: %s is not supported (macOS only)", runtime.GOOS)
}
