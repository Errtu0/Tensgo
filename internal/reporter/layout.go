package reporter

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const (
	DefaultTermWidth = 80
	NarrowBreakpoint = 95
	WideBreakpoint   = 115
)

// TerminalWidth returns the live terminal column width using golang.org/x/term,
// falling back to the COLUMNS environment variable or DefaultTermWidth (80).
func TerminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if cols := strings.TrimSpace(os.Getenv("COLUMNS")); cols != "" {
		if w, err := strconv.Atoi(cols); err == nil && w > 0 {
			return w
		}
	}
	return DefaultTermWidth
}

// ClampBoxWidth clamps a desired box width so it never exceeds termWidth - 2
// and stays at least minWidth when the terminal permits.
func ClampBoxWidth(termWidth, desiredMax, minWidth int) int {
	if termWidth <= 0 {
		termWidth = TerminalWidth()
	}
	maxAllowed := termWidth - 2
	if maxAllowed < 20 {
		maxAllowed = 20
	}
	w := desiredMax
	if w <= 0 || w > maxAllowed {
		w = maxAllowed
	}
	if w < minWidth && minWidth <= maxAllowed {
		w = minWidth
	}
	return w
}

// TruncateRunes truncates s to at most maxRunes characters, appending "..." if shortened.
func TruncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-3]) + "..."
}

// PadRunes truncates or right-pads s with spaces to exact width in runes.
func PadRunes(s string, width int) string {
	if width <= 0 {
		return ""
	}
	t := TruncateRunes(s, width)
	n := len([]rune(t))
	if n >= width {
		return t
	}
	return t + strings.Repeat(" ", width-n)
}
