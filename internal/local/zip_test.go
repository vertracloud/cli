package local

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipExcludesIgnoredFilesAndItself(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".vertraignore"), []byte("*.secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token.secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("API_KEY=secret"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "project.zip")
	if err := Zip(dir, dest); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "main.go" {
		t.Fatalf("zip contents: %#v", z.File)
	}
}
