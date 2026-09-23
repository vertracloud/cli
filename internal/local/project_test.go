package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetProjectIDPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectFile)
	if err := os.WriteFile(path, []byte("NAME=old\nID=one\nMEMORY=512\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetProjectID(dir, "two"); err != nil {
		t.Fatal(err)
	}
	if err := SetProjectID(dir, ""); err != nil {
		t.Fatal(err)
	}
	values, err := ReadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if values["ID"] != "" || values["NAME"] != "old" || values["MEMORY"] != "512" {
		t.Fatalf("config changed: %#v", values)
	}
}
