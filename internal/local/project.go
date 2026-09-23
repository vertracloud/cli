package local

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const ProjectFile = "vertracloud.config"

func projectPath(dir string) string { return filepath.Join(dir, ProjectFile) }

func ReadProject(dir string) (map[string]string, error) {
	file, err := os.Open(projectPath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) != "" {
			values[strings.ToUpper(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	return values, scanner.Err()
}

func SetProjectID(dir, id string) error {
	path := projectPath(dir)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	found := false
	updated := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "ID") {
			if id != "" && !found {
				updated = append(updated, "ID="+id)
			}
			found = true
		} else {
			updated = append(updated, line)
		}
	}
	if id != "" && !found {
		updated = append(updated, "ID="+id)
	}
	if len(updated) == 0 && os.IsNotExist(err) && id == "" {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(updated, "\n")+"\n"), 0600)
}
