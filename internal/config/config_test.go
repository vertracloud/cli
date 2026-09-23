package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAPIKeyEnvironmentWins(t *testing.T) {
	t.Setenv("VERTRA_API_KEY", "from-env")
	if got := APIKey(Config{APIKey: "from-file"}); got != "from-env" {
		t.Fatalf("got %q", got)
	}
}

func TestLocalePriority(t *testing.T) {
	t.Setenv("VERTRA_LANG", "es-MX")
	if got := Locale(Config{Locale: "pt"}, ""); got != "es" {
		t.Fatalf("locale: %q", got)
	}
	if got := Locale(Config{Locale: "pt"}, "en-US"); got != "en" {
		t.Fatalf("explicit locale: %q", got)
	}
}

func TestSaveTightensExistingFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	t.Setenv("HOME", t.TempDir())
	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{APIKey: "key"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}
