package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Check struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}
type Summary struct {
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}
type DoctorResult struct {
	Checks  []Check `json:"checks"`
	Summary Summary `json:"summary"`
}

var subdomain = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

func Doctor(dir string) (DoctorResult, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return DoctorResult{}, fmt.Errorf("invalid directory %q", dir)
	}
	cfg, err := ReadProject(dir)
	if err != nil {
		return DoctorResult{}, err
	}
	result := DoctorResult{Checks: []Check{}}
	add := func(id, level, message string) { result.Checks = append(result.Checks, Check{id, level, message}) }
	if cfg == nil {
		add("config", "warn", "vertracloud.config missing")
	} else {
		var problems []string
		if cfg["MEMORY"] == "" {
			problems = append(problems, "MEMORY missing")
		} else {
			memory, err := strconv.Atoi(cfg["MEMORY"])
			if err != nil || memory < 100 {
				problems = append(problems, "MEMORY invalid")
			}
		}
		if main := cfg["MAIN"]; main != "" {
			if _, err := os.Stat(filepath.Join(dir, main)); err != nil {
				problems = append(problems, "MAIN missing")
			}
		}
		if domain := cfg["SUBDOMAIN"]; domain != "" && !subdomain.MatchString(domain) {
			problems = append(problems, "SUBDOMAIN invalid")
		}
		if len(problems) > 0 {
			add("config", "error", strings.Join(problems, "; "))
		} else {
			add("config", "pass", "configuration valid")
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Type            string            `json:"type"`
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pkg) != nil {
			add("node", "error", "package.json invalid")
		} else {
			var warnings []string
			start := pkg.Scripts["start"]
			if start == "" {
				start = cfg["START"]
			}
			if start == "" {
				warnings = append(warnings, "start script missing")
			}
			if pkg.Type == "module" && filepath.Ext(cfg["MAIN"]) == ".cjs" {
				warnings = append(warnings, "ESM package uses CJS main")
			}
			for _, dep := range []string{"tsx", "ts-node", "typescript"} {
				if strings.Contains(start, dep) && pkg.DevDependencies[dep] != "" && pkg.Dependencies[dep] == "" {
					warnings = append(warnings, dep+" is only in devDependencies")
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "node_modules")); err == nil {
				warnings = append(warnings, "node_modules present")
			}
			if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
				warnings = append(warnings, "package-lock.json missing")
			}
			if len(warnings) > 0 {
				add("node", "warn", strings.Join(warnings, "; "))
			} else {
				add("node", "pass", "Node project ready")
			}
		}
	}
	if hasPython(dir, 0) {
		var warnings []string
		if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err != nil {
			warnings = append(warnings, "requirements.txt missing")
		}
		for _, name := range []string{"venv", ".venv", "__pycache__"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				warnings = append(warnings, name+" present")
			}
		}
		if len(warnings) > 0 {
			add("python", "warn", strings.Join(warnings, "; "))
		} else {
			add("python", "pass", "Python project ready")
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
		add("env", "warn", ".env is not deployed automatically")
		var forbidden []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			key, _, ok := strings.Cut(line, "=")
			if ok {
				switch strings.ToUpper(strings.TrimSpace(key)) {
				case "PORT", "HOST", "PATH", "HOME", "USER", "SHELL":
					forbidden = append(forbidden, strings.TrimSpace(key))
				}
			}
		}
		if len(forbidden) > 0 {
			add("forbidden_env", "warn", "reserved env keys: "+strings.Join(forbidden, ", "))
		} else {
			add("forbidden_env", "pass", "no reserved env keys")
		}
	} else {
		add("env", "pass", "no .env file")
	}
	_, size, err := Estimate(dir)
	if err != nil {
		return DoctorResult{}, err
	}
	mb := float64(size) / 1048576
	if mb > 100 {
		add("zip_size", "error", fmt.Sprintf("project too large: %.1f MB", mb))
	} else if mb > 50 {
		add("zip_size", "warn", fmt.Sprintf("project large: %.1f MB", mb))
	} else {
		add("zip_size", "pass", fmt.Sprintf("project size: %.1f MB", mb))
	}
	for _, c := range result.Checks {
		switch c.Level {
		case "pass":
			result.Summary.Passed++
		case "warn":
			result.Summary.Warnings++
		case "error":
			result.Summary.Errors++
		}
	}
	return result, nil
}

func hasPython(dir string, depth int) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if ignored(name, defaultIgnore) {
			continue
		}
		if entry.IsDir() && depth < 2 && hasPython(filepath.Join(dir, name), depth+1) {
			return true
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".py") {
			return true
		}
	}
	return false
}
