package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	APIKey string `json:"apiKey,omitempty"`
	Locale string `json:"locale,omitempty"`
}

// defaultFileName can be set at build time to isolate a local API profile.
var defaultFileName = "config.json"

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vertracloud", defaultFileName), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return err
	}
	// WriteFile only applies the mode on create; a key file left 0644 by an
	// older CLI must not stay readable by other users.
	return os.Chmod(path, 0600)
}

func APIKey(cfg Config) string {
	if key := os.Getenv("VERTRA_API_KEY"); key != "" {
		return key
	}
	return cfg.APIKey
}

func Locale(cfg Config, explicit string) string {
	value := explicit
	if value == "" {
		value = os.Getenv("VERTRA_LANG")
	}
	if value == "" {
		value = cfg.Locale
	}
	if value == "" {
		value = os.Getenv("LC_ALL")
	}
	if value == "" {
		value = os.Getenv("LANG")
	}
	value = strings.ToLower(value)
	if strings.HasPrefix(value, "pt") {
		return "pt"
	}
	if strings.HasPrefix(value, "es") {
		return "es"
	}
	return "en"
}
