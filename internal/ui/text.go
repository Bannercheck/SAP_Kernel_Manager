package ui

import (
	"io"
	"strings"
	"unicode/utf8"
)

// CleanInput strips terminal control bytes from a typed line: escape
// sequences (arrow keys, Esc), other C0 controls and DEL. It reports whether
// an Esc was present so the caller can treat the line as "cancel". The raw
// line must never be echoed back to a terminal: "ESC n" alone switches the
// terminal's character set and garbles everything typed afterwards.
func CleanInput(line string) (clean string, esc bool) {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == 0x1b:
			esc = true
			// swallow CSI (ESC [ ... final) and SS3 (ESC O x) sequences
			if i+1 < len(line) && (line[i+1] == '[' || line[i+1] == 'O') {
				i++
				for i+1 < len(line) {
					i++
					if line[i] >= 0x40 && line[i] <= 0x7e {
						break
					}
				}
			}
		case c < 0x20 || c == 0x7f:
			if c == '\t' {
				b.WriteByte(' ')
			}
		default:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String()), esc
}

// asciiGlyphs maps the decorative characters the screens use to ASCII for
// terminals that are not in a UTF-8 locale.
var asciiGlyphs = map[rune]string{
	'·': "-", '→': "->", '←': "<-", '…': "...", '—': "-", '–': "-", '›': ">", '‹': "<",
	'✔': "+", '✘': "x", '●': "*", '○': "o", '⬤': "#", '◯': "o", '│': "|", '├': "|", '└': "`", '─': "-",
}

// ASCIIWriter returns a writer that transliterates the decorative glyphs to
// ASCII; other text passes through unchanged. Use it when the palette has
// Unicode off so every screen, not only the palette helpers, stays readable.
func ASCIIWriter(w io.Writer) io.Writer { return &asciiWriter{w: w} }

type asciiWriter struct {
	w    io.Writer
	rest []byte // incomplete trailing UTF-8 sequence from the previous Write
}

func (a *asciiWriter) Write(p []byte) (int, error) {
	buf := append(a.rest, p...)
	a.rest = nil
	out := make([]byte, 0, len(buf))
	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 && !utf8.FullRune(buf) {
			a.rest = append([]byte(nil), buf...) // wait for the rest of the rune
			break
		}
		if s, ok := asciiGlyphs[r]; ok {
			out = append(out, s...)
		} else {
			out = append(out, buf[:size]...)
		}
		buf = buf[size:]
	}
	if _, err := a.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
