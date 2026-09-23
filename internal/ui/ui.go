package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Choice struct{ ID, Label, Hint string }

// Locale is the CLI language (pt, en or es), set once by main.
var Locale = "en"

// Presentation keeps machine output unchanged while allowing richer human output.
type Presentation struct{ JSON, Human any }

func IsInteractive() bool { return stdinTTY() && stdoutTTY() }

func stdinTTY() bool {
	if os.Getenv("CI") != "" {
		return false
	}
	file, err := os.Stdin.Stat()
	return err == nil && file.Mode()&os.ModeCharDevice != 0
}

func isTTY(f *os.File) bool {
	if os.Getenv("CI") != "" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// colorOn follows the NO_COLOR convention and only colors real terminals, so
// pipes, files and tests receive plain text.
var colorOn = isTTY(os.Stdout) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

func paint(code, s string) string {
	if !colorOn || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string   { return paint("1", s) }
func Dim(s string) string    { return paint("2", s) }
func Green(s string) string  { return paint("32", s) }
func Red(s string) string    { return paint("31", s) }
func Yellow(s string) string { return paint("33", s) }
func Cyan(s string) string   { return paint("36", s) }

// Success prints "✓ message"; Failure prints "✗ message" and an optional hint.
func Success(w io.Writer, message string) { fmt.Fprintf(w, "%s %s\n", Green("✓"), message) }

func Failure(w io.Writer, message, hint string) {
	fmt.Fprintf(w, "%s %s\n", Red("✗"), message)
	if hint != "" {
		fmt.Fprintf(w, "  %s\n", Dim(hint))
	}
}

// Spin shows a spinner on stderr while a slow call runs. It is a no-op when
// stderr is not a terminal. The returned func stops it and clears the line.
func Spin(label string) func() {
	if !isTTY(os.Stderr) {
		return func() {}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(os.Stderr, "\r\x1b[2K%s %s", Cyan(frames[i%len(frames)]), label)
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\x1b[2K")
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); wg.Wait() }) }
}

func PickID(label string, items []Choice) (string, error) {
	if len(items) == 0 {
		return "", errors.New("no options available")
	}
	if !IsInteractive() {
		return "", errors.New("interactive selection requires a terminal")
	}
	state, err := terminalState()
	if err != nil {
		return "", err
	}
	defer restoreTerminal(state)
	width := 0
	for _, item := range items {
		width = max(width, len([]rune(item.Label)))
	}
	cursor, lines := 0, len(items)+2
	// The terminal is in raw mode here, so every line ends with an explicit \r\n.
	paintList := func() {
		fmt.Fprintf(os.Stdout, "%s %s\r\n", Cyan("?"), Bold(label))
		for i, item := range items {
			text := item.Label + strings.Repeat(" ", width-len([]rune(item.Label)))
			if i == cursor {
				fmt.Fprintf(os.Stdout, "%s %s  %s\r\n", Cyan("❯"), Cyan(text), Dim(item.Hint))
			} else {
				fmt.Fprintf(os.Stdout, "  %s  %s\r\n", text, Dim(item.Hint))
			}
		}
		fmt.Fprint(os.Stdout, Dim(text(Locale, "picker_hint"))+"\r\n")
	}
	clear := func() {
		fmt.Fprintf(os.Stdout, "\x1b[%dA\x1b[J", lines)
	}
	paintList()
	for {
		key, err := readTerminalKey()
		if err != nil {
			return "", err
		}
		switch key {
		case "up":
			cursor = (cursor - 1 + len(items)) % len(items)
		case "down":
			cursor = (cursor + 1) % len(items)
		case "enter":
			clear()
			fmt.Fprintf(os.Stdout, "%s %s %s\r\n", Green("✓"), Bold(label), Cyan(items[cursor].Label))
			return items[cursor].ID, nil
		case "cancel":
			clear()
			return "", errors.New("selection cancelled")
		default:
			continue
		}
		clear()
		paintList()
	}
}

func Confirm(message string) (bool, error) {
	if !stdinTTY() {
		return false, errors.New("destructive command requires --yes when stdin is not a terminal")
	}
	fmt.Fprintf(os.Stdout, "%s %s %s ", Yellow("?"), Bold(message), Dim("(y/N)"))
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes" || answer == "s" || answer == "sim" || answer == "si", nil
}

func ReadMasked(prompt string) (string, error) {
	if !stdinTTY() {
		return "", errors.New("token prompt requires a terminal; use --token")
	}
	setEcho := func(enabled bool) error {
		arg := "-echo"
		if enabled {
			arg = "echo"
		}
		cmd := exec.Command("stty", arg)
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}
	if err := setEcho(false); err != nil {
		return "", fmt.Errorf("disable terminal echo: %w", err)
	}
	defer setEcho(true)
	fmt.Fprint(os.Stdout, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Fprintln(os.Stdout)
	return strings.TrimSpace(line), err
}

func stdoutTTY() bool { return isTTY(os.Stdout) }

func ReadPageKey() (string, error) {
	if !stdinTTY() {
		return "quit", nil
	}
	state, err := terminalState()
	if err != nil {
		return "", err
	}
	defer restoreTerminal(state)
	key, err := readTerminalKey()
	if err != nil {
		return "", err
	}
	switch key {
	case "right", "down", "enter", "next":
		return "next", nil
	case "left", "up", "prev":
		return "prev", nil
	default:
		return "quit", nil
	}
}

func terminalState() (string, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	state, err := cmd.Output()
	if err != nil {
		return "", err
	}
	set := exec.Command("stty", "raw", "-echo")
	set.Stdin = os.Stdin
	if err := set.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(state)), nil
}

func restoreTerminal(state string) {
	cmd := exec.Command("stty", state)
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
}

func readTerminalKey() (string, error) {
	var key [1]byte
	if _, err := os.Stdin.Read(key[:]); err != nil {
		return "", err
	}
	switch key[0] {
	case 27:
		var seq [2]byte
		if _, err := os.Stdin.Read(seq[:]); err != nil {
			return "", err
		}
		switch seq {
		case [2]byte{'[', 'A'}:
			return "up", nil
		case [2]byte{'[', 'B'}:
			return "down", nil
		case [2]byte{'[', 'C'}:
			return "right", nil
		case [2]byte{'[', 'D'}:
			return "left", nil
		}
		return "cancel", nil
	case '\r', '\n':
		return "enter", nil
	case 'q', 'Q', 3:
		return "cancel", nil
	case 'n', 'N':
		return "next", nil
	case 'p', 'P':
		return "prev", nil
	}
	return "", nil
}
