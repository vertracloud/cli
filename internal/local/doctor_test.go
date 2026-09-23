package local

import (
	"os"
	"path/filepath"
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
