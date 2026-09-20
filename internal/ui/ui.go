// Package ui renders colours, traffic lights and check marks for terminal
// output. Colour is used only on an interactive terminal (or when forced),
// Unicode glyphs only when the locale can show them; otherwise ASCII stands in.
package ui

import (
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// Colour identifies a semantic colour.
type Colour int

const (
	Plain Colour = iota
	Green
	Yellow
	Red
	Dim
	Cyan
	Magenta
	Bold
)

var sgr = map[Colour]string{
	Green: "32", Yellow: "33", Red: "31", Dim: "90", Cyan: "36", Magenta: "35", Bold: "1",
}

// Palette says what the current output can display.
type Palette struct {
	Colour  bool
	Unicode bool
}

// Detect inspects stdout and the environment. Overrides:
// <PREFIX>COLOR=always|never, <PREFIX>UNICODE=1|0, NO_COLOR, TERM=dumb.
func Detect(f *os.File) Palette {
	p := Palette{}
	tty := IsTerminal(f)
	switch strings.ToLower(os.Getenv(version.EnvPrefix + "COLOR")) {
	case "always", "1", "true":
		p.Colour = true
	case "never", "0", "false":
		p.Colour = false
	default:
		p.Colour = tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	if p.Colour && tty {
		p.Colour = enableVirtualTerminal(f) // no-op except on Windows
	}
	switch os.Getenv(version.EnvPrefix + "UNICODE") {
	case "1", "true":
		p.Unicode = true
	case "0", "false":
		p.Unicode = false
	default:
		p.Unicode = runtime.GOOS != "windows" && localeIsUTF8()
	}
	return p
}

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func localeIsUTF8() bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(os.Getenv(k)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// Paint wraps s in the colour's escape sequence when colour is enabled.
func (p Palette) Paint(c Colour, s string) string {
	code, ok := sgr[c]
	if !p.Colour || !ok || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Light returns a traffic-light glyph in the given colour: ● / ○ or ASCII.
func (p Palette) Light(c Colour) string {
	if p.Unicode {
		if c == Dim {
			return p.Paint(Dim, "○")
		}
		return p.Paint(c, "●")
	}
	switch c {
	case Green:
		return p.Paint(c, "(+)")
	case Yellow:
		return p.Paint(c, "(~)")
	case Red:
		return p.Paint(c, "(x)")
	default:
		return p.Paint(Dim, "(-)")
	}
}

// Check is the "done" marker, Cross the "failed" marker.
func (p Palette) Check() string {
	if p.Unicode {
		return p.Paint(Green, "✔")
	}
	return p.Paint(Green, "OK")
}

func (p Palette) Cross() string {
	if p.Unicode {
		return p.Paint(Red, "✘")
	}
	return p.Paint(Red, "!!")
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Strip removes escape sequences (for width calculations and plain logs).
func Strip(s string) string { return ansiRe.ReplaceAllString(s, "") }

// TabSafe brackets escape sequences with tabwriter.Escape so that
// text/tabwriter (with the StripEscape flag) ignores them when aligning.
func TabSafe(s string) string {
	return ansiRe.ReplaceAllStringFunc(s, func(m string) string { return "\xff" + m + "\xff" })
}
