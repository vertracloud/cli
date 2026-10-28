package flagset

import (
	"strings"
	"testing"
)

func TestParseAcceptsFlagsAnywhereAndRepeats(t *testing.T) {
	f := New("t")
	name := f.String("name", "", "")
	yes := f.Bool("yes", false, "")
	var sets Repeat
	f.Var(&sets, "set", "")
	pos, err := Parse(f, []string{"a", "--set", "x=1", "-y", "--name=n", "b", "--set=y=-2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pos, ",") != "a,b" || *name != "n" || !*yes || strings.Join(sets, ",") != "x=1,y=-2" {
		t.Fatalf("pos=%v name=%q yes=%v sets=%v", pos, *name, *yes, sets)
	}
}

func TestParseRejectsUnknownFlagsAndMissingValues(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"--name"}, {"-z"}} {
		f := New("t")
		f.String("name", "", "")
		if _, err := Parse(f, args); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
	f := New("t")
	f.String("name", "", "")
	if pos, err := Parse(f, []string{"--", "--name"}); err != nil || len(pos) != 1 || pos[0] != "--name" {
		t.Fatalf("pos=%v err=%v", pos, err)
	}
}
