package local

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var defaultIgnore = []string{"node_modules", ".git", ".gitignore", ".vscode", ".github", ".vertraignore", ".vertracloudignore", "__pycache__", "venv", ".venv", "vendor", "target", ".next"}

func patterns(dir string) []string {
	out := append([]string(nil), defaultIgnore...)
	for _, name := range []string{".vertraignore", ".vertracloudignore"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				out = append(out, line)
			}
		}
		break
	}
	return out
}

func ignored(name string, patterns []string) bool {
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	for _, pattern := range patterns {
		if name == pattern || strings.HasSuffix(name, strings.TrimPrefix(pattern, "*")) {
			return true
		}
	}
	return false
}

func collect(dir, dest string, patterns []string) ([]string, int64, error) {
	var files []string
	var size int64
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		if dest != "" && path == dest {
			return nil
		}
		if ignored(entry.Name(), patterns) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		files = append(files, path)
		return nil
	})
	return files, size, err
}

func Estimate(dir string) (int, int64, error) {
	files, size, err := collect(dir, "", patterns(dir))
	return len(files), size, err
}

func Zip(dir, dest string) error {
	files, _, err := collect(dir, dest, patterns(dir))
	if err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for _, path := range files {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			break
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			err = openErr
			break
		}
		w, createErr := zw.Create(filepath.ToSlash(rel))
		if createErr == nil {
			_, createErr = io.Copy(w, f)
		}
		_ = f.Close()
		if createErr != nil {
			err = createErr
			break
		}
	}
	closeErr := zw.Close()
	fileErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileErr
}
