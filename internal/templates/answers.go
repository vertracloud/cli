package templates

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ParseSets reads repeated --set key=value pairs.
func ParseSets(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf(text("bad_set"), pair)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

func lineReader() *bufio.Reader {
	if reader == nil {
		reader = bufio.NewReader(stdin)
	}
	return reader
}

// readLine reads one answer. eof reports that the input ended before a line.
func readLine() (line string, eof bool) {
	s, err := lineReader().ReadString('\n')
	if err != nil && s == "" {
		return "", true
	}
	return strings.TrimRight(s, "\r\n"), false
}

// ReadLine reads one line from the shared input (stdin). eof reports that the
// input ended before a line. Packages that prompt next to the template
// commands use it so every answer comes from one buffered reader.
func ReadLine() (line string, eof bool) { return readLine() }

// IsInteractive reports whether stdin and stdout are a terminal.
func IsInteractive() bool { return interactive() }

// UseInput replaces the input and the terminal check, and returns the func
// that puts them back. It exists for the tests of the commands that share the
// prompts.
func UseInput(r io.Reader, tty bool) (restore func()) {
	oldIn, oldTTY, oldReader := stdin, interactive, reader
	stdin, interactive, reader = r, func() bool { return tty }, nil
	return func() { stdin, interactive, reader = oldIn, oldTTY, oldReader }
}

// Confirm asks a yes/no question on errOut and reads the answer from stdin.
func Confirm(errOut io.Writer, message string) bool {
	fmt.Fprintf(errOut, "%s %s %s ", ui.Yellow("?"), ui.Bold(message), ui.Dim("("+text("ask_yes_no")+")"))
	line, _ := readLine()
	return isYes(line)
}

func isYes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "s", "sim", "si", "sí", "true", "1":
		return true
	}
	return false
}

func isNo(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "n", "no", "não", "nao", "false", "0":
		return true
	}
	return false
}

// coerce turns the text of an answer into the value the API expects and
// checks what can be checked without the server. The server checks again.
func coerce(in vertracloud.TemplateInput, raw string) (vertracloud.TemplateInputValue, error) {
	switch in.Type {
	case vertracloud.TemplateInputTypeBoolean:
		switch {
		case isYes(raw):
			return true, nil
		case isNo(raw):
			return false, nil
		}
		return nil, errors.New(text("v_not_bool"))
	case vertracloud.TemplateInputTypeNumber:
		n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, errors.New(text("v_not_number"))
		}
		if v := in.Validation; v != nil {
			if v.Min != nil && n < *v.Min {
				return nil, fmt.Errorf(text("v_min"), *v.Min)
			}
			if v.Max != nil && n > *v.Max {
				return nil, fmt.Errorf(text("v_max"), *v.Max)
			}
		}
		if n == float64(int64(n)) {
			return int64(n), nil
		}
		return n, nil
	case vertracloud.TemplateInputTypeSelect:
		values := make([]string, 0, len(in.Options))
		for _, o := range in.Options {
			if o.Value == raw {
				return raw, nil
			}
			values = append(values, o.Value)
		}
		return nil, fmt.Errorf(text("v_not_option"), strings.Join(values, ", "))
	}
	if v := in.Validation; v != nil {
		n := utf8.RuneCountInString(raw)
		if v.MinLength != nil && n < *v.MinLength {
			return nil, fmt.Errorf(text("v_min_length"), *v.MinLength)
		}
		if v.MaxLength != nil && n > *v.MaxLength {
			return nil, fmt.Errorf(text("v_max_length"), *v.MaxLength)
		}
		if v.Regex != "" {
			if re, err := regexp.Compile(v.Regex); err == nil && !re.MatchString(raw) {
				return nil, errors.New(text("v_regex"))
			}
		}
	}
	return raw, nil
}

// whenHolds reports whether the question applies, given the answers so far
// (and the defaults of the questions not answered yet).
func whenHolds(in vertracloud.TemplateInput, answers map[string]vertracloud.TemplateInputValue, byID map[string]vertracloud.TemplateInput) bool {
	if in.When == nil {
		return true
	}
	current, ok := answers[in.When.Input]
	if !ok {
		current = byID[in.When.Input].Default
	}
	return fmt.Sprint(current) == fmt.Sprint(in.When.Equals)
}

// generated reports a secret the platform generates for each person: it is
// not asked unless the author lets the person edit it.
func generated(in vertracloud.TemplateInput) bool {
	return in.Generate != "" && (in.Editable == nil || !*in.Editable)
}

// CollectAnswers builds the answers to the questions of a template. Values in
// set (from --set) win; with prompt true the rest is asked on errOut and read
// from stdin (secrets without echo); otherwise a required question with no
// default and no value is an error that lists every missing one.
func CollectAnswers(inputs []vertracloud.TemplateInput, set map[string]string, prompt bool, errOut io.Writer) (map[string]vertracloud.TemplateInputValue, error) {
	byID := make(map[string]vertracloud.TemplateInput, len(inputs))
	for _, in := range inputs {
		byID[in.ID] = in
	}
	var unknown []string
	for k := range set {
		if _, ok := byID[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		known := make([]string, 0, len(inputs))
		for _, in := range inputs {
			known = append(known, in.ID)
		}
		return nil, fmt.Errorf(text("unknown_inputs"), strings.Join(unknown, ", "), orNone(known))
	}
	answers := map[string]vertracloud.TemplateInputValue{}
	var missing []string
	for _, in := range inputs {
		if !whenHolds(in, answers, byID) {
			continue
		}
		if raw, ok := set[in.ID]; ok {
			value, err := coerce(in, raw)
			if err != nil {
				return nil, fmt.Errorf("--set %s: %w", in.ID, err)
			}
			answers[in.ID] = value
			continue
		}
		if generated(in) {
			continue
		}
		if !prompt {
			if in.Required && in.Default == nil {
				missing = append(missing, in.ID)
			}
			continue
		}
		value, ok, err := ask(in, errOut)
		if err != nil {
			return nil, err
		}
		if ok {
			answers[in.ID] = value
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(text("missing_answers"), strings.Join(missing, ", "))
	}
	return answers, nil
}

func orNone(list []string) string {
	if len(list) == 0 {
		return text("stack_none")
	}
	return strings.Join(list, ", ")
}

// ask prompts for one question until the answer is acceptable. ok is false
// when the person leaves it empty and there is a default (the server applies it).
func ask(in vertracloud.TemplateInput, errOut io.Writer) (value vertracloud.TemplateInputValue, ok bool, err error) {
	label := ui.Plain(I18n(in.Label))
	if in.Help != nil {
		if help := I18n(*in.Help); help != "" {
			fmt.Fprintf(errOut, "%s\n", ui.Dim(ui.Plain(help)))
		}
	}
	hasDefault := in.Default != nil && fmt.Sprint(in.Default) != ""
	suffix := ""
	if hasDefault && in.Type != vertracloud.TemplateInputTypeSecret {
		suffix = " " + ui.Dim("["+fmt.Sprint(in.Default)+"]")
	} else if in.Required {
		suffix = " " + ui.Dim("("+text("label_required")+")")
	}
	if in.Type == vertracloud.TemplateInputTypeSelect {
		for n, o := range in.Options {
			name := ui.Plain(o.Value)
			if o.Label != nil {
				name = ui.Plain(I18n(*o.Label)) + " " + ui.Dim("("+ui.Plain(o.Value)+")")
			}
			fmt.Fprintf(errOut, "  %d) %s\n", n+1, name)
		}
	}
	for {
		var raw string
		var eof bool
		if in.Type == vertracloud.TemplateInputTypeSecret {
			s, serr := readSecret(fmt.Sprintf("%s %s%s: ", ui.Yellow("?"), ui.Bold(label), suffix))
			raw, eof = s, serr != nil && s == ""
		} else {
			fmt.Fprintf(errOut, "%s %s%s: ", ui.Yellow("?"), ui.Bold(label), suffix)
			raw, eof = readLine()
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			if hasDefault {
				return nil, false, nil
			}
			if !in.Required {
				return nil, false, nil
			}
			if eof {
				return nil, false, fmt.Errorf(text("missing_answers"), in.ID)
			}
			fmt.Fprintln(errOut, ui.Red(text("ask_required")))
			continue
		}
		if in.Type == vertracloud.TemplateInputTypeSelect {
			if n, perr := strconv.Atoi(raw); perr == nil && n >= 1 && n <= len(in.Options) {
				raw = in.Options[n-1].Value
			}
		}
		v, cerr := coerce(in, raw)
		if cerr != nil {
			if eof {
				return nil, false, fmt.Errorf("%s: %w", in.ID, cerr)
			}
			fmt.Fprintln(errOut, ui.Red(fmt.Sprintf(text("ask_invalid"), cerr.Error())))
			continue
		}
		return v, true, nil
	}
}
