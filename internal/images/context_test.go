package images

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree writes files (path -> content) under a new temp dir.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func packed(t *testing.T, dir string) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := PackDir(dir, &buf); err != nil {
		t.Fatalf("PackDir: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestPackDirKeepsEverythingWithoutDockerignore(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n", "src/main.go": "x", "README.md": "r"})
	got := packed(t, dir)
	want := []string{"Dockerfile", "README.md", "src/main.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDockerignoreNodeModulesAndNegation(t *testing.T) {
	dir := tree(t, map[string]string{
		"Dockerfile":                "FROM node\n",
		".dockerignore":             "# deps\nnode_modules\n\n*.log\n!keep.log\n!node_modules/keep.js\n",
		"index.js":                  "x",
		"node_modules/a/index.js":   "x",
		"node_modules/keep.js":      "x",
		"debug.log":                 "x",
		"keep.log":                  "x",
		"sub/other.log":             "x",
		"# not a comment in a name": "x",
	})
	got := packed(t, dir)
	for _, must := range []string{"Dockerfile", "index.js", "keep.log", "node_modules/keep.js", ".dockerignore"} {
		if !has(got, must) {
			t.Errorf("%s missing from %v", must, got)
		}
	}
	for _, mustNot := range []string{"node_modules/a/index.js", "debug.log"} {
		if has(got, mustNot) {
			t.Errorf("%s should be ignored: %v", mustNot, got)
		}
	}
	// "*.log" has no slash, so like Docker it only matches at the root.
	if !has(got, "sub/other.log") {
		t.Errorf("sub/other.log must stay: %v", got)
	}
}

func TestDockerignoreAnchoredDoubleStarAndDirSlash(t *testing.T) {
	dir := tree(t, map[string]string{
		"Dockerfile":     "FROM scratch\n",
		".dockerignore":  "/build\n**/*.tmp\ntemp/\n",
		"build/out.bin":  "x",
		"src/build/x.go": "x",
		"a.tmp":          "x",
		"deep/b/c.tmp":   "x",
		"temp/file":      "x",
		"src/main.go":    "x",
	})
	got := packed(t, dir)
	for _, mustNot := range []string{"build/out.bin", "a.tmp", "deep/b/c.tmp", "temp/file"} {
		if has(got, mustNot) {
			t.Errorf("%s should be ignored: %v", mustNot, got)
		}
	}
	for _, must := range []string{"src/build/x.go", "src/main.go"} {
		if !has(got, must) {
			t.Errorf("%s missing: %v", must, got)
		}
	}
}

func TestDockerfileAlwaysKept(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n", ".dockerignore": "*\n", "a": "x"})
	got := packed(t, dir)
	if !has(got, "Dockerfile") || has(got, "a") {
		t.Fatalf("got %v", got)
	}
}

func TestEnvFilesAlwaysExcluded(t *testing.T) {
	dir := tree(t, map[string]string{
		"Dockerfile":      "FROM scratch\n",
		".dockerignore":   "!.env\n!.env.local\n!**/.env\n",
		".env":            "SECRET=1",
		".env.local":      "SECRET=1",
		".env.production": "SECRET=1",
		"sub/.env":        "SECRET=1",
		"app/.env.test":   "SECRET=1",
		"env.go":          "x",
	})
	got := packed(t, dir)
	for _, n := range got {
		if strings.HasPrefix(filepath.Base(n), ".env") {
			t.Errorf("%s must never be sent: %v", n, got)
		}
	}
	if !has(got, "env.go") || !has(got, "Dockerfile") {
		t.Fatalf("got %v", got)
	}
}

func TestMissingDockerfileIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{"index.js": "x"})
	var buf bytes.Buffer
	err := PackDir(dir, &buf)
	if !errors.Is(err, ErrNoDockerfile) {
		t.Fatalf("got %v", err)
	}
}

func TestSymlinksAreSkipped(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	outside := tree(t, map[string]string{"secret": "x"})
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	got := packed(t, dir)
	if has(got, "link") {
		t.Fatalf("symlink was packed: %v", got)
	}
}

func TestOversizedContextIsRefusedWhilePacking(t *testing.T) {
	if MaxContextBytes != 100<<20 {
		t.Fatalf("limit is %d, want 100 MB", MaxContextBytes)
	}
	old := MaxContextBytes
	MaxContextBytes = 64 << 10
	defer func() { MaxContextBytes = old }()
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	blob := make([]byte, 200<<10)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := PackDir(dir, &buf)
	if !errors.Is(err, ErrContextTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenContextDirPacksToTempAndCleansUp(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n", ".env": "S=1"})
	src, err := OpenContext(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(src.Name, ".zip") {
		t.Errorf("name %q", src.Name)
	}
	data, _ := io.ReadAll(src.Reader)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "Dockerfile" {
		t.Fatalf("zip: %v %v", err, zr)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if src.path != "" {
		if _, err := os.Stat(src.path); !os.IsNotExist(err) {
			t.Fatalf("temp file left behind: %v", err)
		}
	}
}

func TestOpenContextZipIsSentAsIs(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "ctx.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(".env") // the server filters it; the client does not touch a ready zip
	_, _ = w.Write([]byte("S=1"))
	_ = zw.Close()
	if err := os.WriteFile(zp, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := OpenContext(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	got, _ := io.ReadAll(src.Reader)
	if !bytes.Equal(got, buf.Bytes()) || src.Name != "ctx.zip" {
		t.Fatalf("zip changed or renamed: %q", src.Name)
	}
}

func TestOpenContextOversizedZipIsRefused(t *testing.T) {
	old := MaxContextBytes
	MaxContextBytes = 10
	defer func() { MaxContextBytes = old }()
	zp := filepath.Join(t.TempDir(), "ctx.zip")
	if err := os.WriteFile(zp, bytes.Repeat([]byte("a"), 50), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenContext(zp); !errors.Is(err, ErrContextTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenContextRejectsOtherFiles(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.tar")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if _, err := OpenContext(f); err == nil {
		t.Fatal("expected error for non-zip file")
	}
	if _, err := OpenContext(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing path")
	}
}
