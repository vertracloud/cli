package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorReportsInvalidConfigAndReservedEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte("MEMORY=50\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=3000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Doctor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Errors != 1 || got.Summary.Warnings != 2 {
		t.Fatalf("summary: %#v", got.Summary)
	}
}

func TestDoctorChecksAutoRestartValue(t *testing.T) {
	check := func(value string) Check {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte("MEMORY=256\nAUTORESTART="+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := Doctor(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range got.Checks {
			if c.ID == "config" {
				return c
			}
		}
		t.Fatal("no config check")
		return Check{}
	}
	if c := check("maybe"); c.Level != "error" || !strings.Contains(c.Message, "AUTORESTART invalid") {
		t.Fatalf("maybe: %#v", c)
	}
	for _, ok := range []string{"true", "false", "False"} {
		if c := check(ok); c.Level != "pass" {
			t.Fatalf("%s: %#v", ok, c)
		}
	}
}
