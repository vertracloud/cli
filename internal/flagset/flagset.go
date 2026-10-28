// Package flagset parses the flags of a subcommand the way the app and image
// commands do: flags may come before or after the positional arguments, and
// both --name value and --name=value work.
package flagset

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// New returns a FlagSet that reports errors to the caller instead of printing.
func New(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

// Repeat is a string flag that may be given more than once.
type Repeat []string

func (r *Repeat) String() string     { return strings.Join(*r, ",") }
func (r *Repeat) Set(v string) error { *r = append(*r, v); return nil }

// Parse parses args against the flags already declared on f and returns the
// positional arguments. -y is accepted for --yes. "--" ends the flags.
func Parse(f *flag.FlagSet, args []string) ([]string, error) {
	isBool := map[string]bool{}
	f.VisitAll(func(fl *flag.Flag) {
		b, ok := fl.Value.(interface{ IsBoolFlag() bool })
		isBool["--"+fl.Name] = ok && b.IsBoolFlag()
	})
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		key, hasValue := a, false
		if n := strings.IndexByte(a, '='); n >= 0 {
			key, hasValue = a[:n], true
		}
		if key == "-y" {
			a = "--yes" + a[len(key):]
			key = "--yes"
		}
		if key == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		boolean, known := isBool[key]
		switch {
		case known && boolean:
			flags = append(flags, a)
		case known:
			flags = append(flags, a)
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s needs a value", key)
				}
				flags = append(flags, args[i+1])
				i++
			}
		case strings.HasPrefix(key, "-") && len(key) > 1:
			return nil, fmt.Errorf("unknown flag %q", args[i])
		default:
			positional = append(positional, a)
		}
	}
	if err := f.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}
