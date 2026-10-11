package ui

import (
	"strings"
	"unicode/utf8"
)

// Text that comes from the server (app logs, commit messages, author names,
// variable names, error messages) is attacker-controlled. Printed raw it can
// rewrite the screen, forge a prompt or write to the clipboard (OSC 52).
// Plain and LogText are the only way such text reaches the terminal.

// Plain is for one-line fields: it drops every escape sequence and control
// character and turns newlines and tabs into spaces.
func Plain(s string) string { return sanitize(s, false) }

// LogText is for multi-line log content: it keeps newlines, tabs and SGR color
// sequences (ESC [ digits/; m) and drops everything else Plain drops.
func LogText(s string) string { return sanitize(s, true) }

func sanitize(s string, logMode bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b || (r >= 0x80 && r <= 0x9f):
			end, sgr := scanSequence(s, i)
			if sgr && logMode {
				b.WriteString(s[i:end])
			}
			i = end
			continue
		case r == utf8.RuneError && n == 1: // stray byte, including raw 8-bit C1
		case r == '\n' || r == '\t':
			if logMode {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r < 0x20 || r == 0x7f: // C0 and DEL; a lone \r vanishes, so \r\n becomes \n
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069: // bidi overrides that reorder visible text
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// scanSequence returns the index just past the escape sequence starting at s[i]
// (ESC or a C1 control) and whether it is a plain SGR color sequence. An
// unterminated sequence consumes the rest of the string, as a terminal would.
func scanSequence(s string, i int) (end int, sgr bool) {
	r, n := utf8.DecodeRuneInString(s[i:])
	j := i + n
	csi, str := r == 0x9b, r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f
	if r == 0x1b {
		if j >= len(s) {
			return j, false
		}
		switch c := s[j]; {
		case c == '[':
			csi = true
			j++
		case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
			str = true
			j++
		case c >= 0x20 && c <= 0x2f: // ESC + intermediates + final (charset selection etc.)
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
				j++
			}
			if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
				j++
			}
			return j, false
		case c >= 0x30 && c <= 0x7e:
			return j + 1, false
		default:
			return j, false
		}
	}
	switch {
	case csi:
		params := j
		for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
			j++
		}
		paramEnd := j
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j >= len(s) {
			return len(s), false
		}
		if s[j] < 0x40 || s[j] > 0x7e {
			return j, false // malformed: drop what was read, keep the offending byte for the caller
		}
		sgr = r == 0x1b && s[j] == 'm' && j == paramEnd && strings.Trim(s[params:paramEnd], "0123456789;") == ""
		return j + 1, sgr
	case str:
		for ; j < len(s); j++ {
			switch {
			case s[j] == 0x07 || s[j] == 0x18 || s[j] == 0x1a:
				return j + 1, false
			case s[j] == 0x1b:
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2, false
				}
				return j, false // a new escape aborts the string
			case s[j] == 0xc2 && j+1 < len(s) && s[j+1] == 0x9c: // U+009C ST
				return j + 2, false
			}
		}
		return len(s), false
	}
	return j, false
}
