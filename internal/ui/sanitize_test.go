package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeModes(t *testing.T) {
	cases := []struct{ name, in, plain, log string }{
		{"osc52 bel", "a\x1b]52;c;ZXZpbA==\x07b", "ab", "ab"},
		{"osc52 st", "a\x1b]52;c;ZXZpbA==\x1b\\b", "ab", "ab"},
		{"osc title", "x\x1b]0;pwned\x07y", "xy", "xy"},
		{"dcs", "x\x1bPq#0;2;0;0;0\x1b\\y", "xy", "xy"},
		{"csi erase", "a\x1b[2Jb", "ab", "ab"},
		{"csi cursor up", "a\x1b[1Ab", "ab", "ab"},
		{"csi private", "a\x1b[?25lb", "ab", "ab"},
		{"fake prompt", "Deploy ok\rContinue? [y/N]", "Deploy okContinue? [y/N]", "Deploy okContinue? [y/N]"},
		{"crlf", "a\r\nb", "a b", "a\nb"},
		{"backspace and del", "ab\b\bcd\x7fe", "abcde", "abcde"},
		{"c1 raw byte", "a\x9b2Jb", "a2Jb", "a2Jb"},
		{"c1 unicode csi", "a\u009b2Jb", "ab", "ab"}, // U+009B CSI plus its parameters
		{"c1 unicode other", "a\u0085b", "ab", "ab"},
		{"sgr", "\x1b[1;31mred\x1b[0m", "red", "\x1b[1;31mred\x1b[0m"},
		{"sgr with intermediate is dropped", "\x1b[1 mx", "x", "x"},
		{"sgr private is dropped", "\x1b[?1mx", "x", "x"},
		{"newline and tab", "a\n\tb", "a  b", "a\n\tb"},
		{"unicode kept", "ação 🚀 日本語", "ação 🚀 日本語", "ação 🚀 日本語"},
		{"bidi override", "a‮b", "ab", "ab"},
		{"lone esc at end", "abc\x1b", "abc", "abc"},
		{"unterminated csi", "abc\x1b[12", "abc", "abc"},
		{"unterminated osc eats the rest", "abc\x1b]52;c;AAAA", "abc", "abc"},
		{"esc inside osc starts a new sequence", "a\x1b]0;t\x1b[2Jb", "ab", "ab"},
		{"charset selection", "a\x1b(0b", "ab", "ab"},
		{"two byte escape", "a\x1bcb", "ab", "ab"},
	}
	for _, c := range cases {
		if got := Plain(c.in); got != c.plain {
			t.Errorf("%s: Plain(%q) = %q, want %q", c.name, c.in, got, c.plain)
		}
		if got := LogText(c.in); got != c.log {
			t.Errorf("%s: LogText(%q) = %q, want %q", c.name, c.in, got, c.log)
		}
	}
}

func TestRenderSanitizesServerText(t *testing.T) {
	var out bytes.Buffer
	RenderPaged(&out, map[string]any{"name": "app\x1b]52;c;QQ==\x07", "note": "x\x1b[2Jy", "created_at": "bad\x1b[1A"}, "app info", "en", 0, true)
	RenderPaged(&out, []any{map[string]any{"name": "a\x1b[2Jb", "id": "i\r1", "m\x1bk": "v"}}, "app list", "en", 0, true)
	RenderPaged(&out, "line\x1b]0;t\x07 1\n\x1b[32mgreen\x1b[0m\x1b[2K\n", "app logs", "en", 0, true)
	if strings.ContainsAny(out.String(), "\x07\r\b\x7f") || strings.Contains(out.String(), "]52") || strings.Contains(out.String(), "[2J") || strings.Contains(out.String(), "[1A") || strings.Contains(out.String(), "[2K") {
		t.Fatalf("escape reached output: %q", out.String())
	}
	if !strings.Contains(out.String(), "line 1\n") {
		t.Fatalf("log text lost: %q", out.String())
	}
}

func TestRenderKeepsSGRInLogs(t *testing.T) {
	var out bytes.Buffer
	RenderPaged(&out, "\x1b[32mok\x1b[0m\n", "app logs", "en", 0, true)
	if out.String() != "\x1b[32mok\x1b[0m\n\n" {
		t.Fatalf("got %q", out.String())
	}
}

// --json is for machines and is not sanitized: the encoder escapes C0
// controls, so a pipe never carries a raw ESC, BEL or CR.
func TestJSONEncoderEscapesControls(t *testing.T) {
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(map[string]string{"m": "a\x1b]52;c;QQ==\x07\r\x1b[2J"}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\x07\r") {
		t.Fatalf("raw control in JSON: %q", out.String())
	}
}
