package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	VaultPath  string
	DBPath     string
	Theme      string
	DateFormat string
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		VaultPath:  filepath.Join(home, "notas", "vault"),
		DBPath:     filepath.Join(home, ".config", "notas", "index.db"),
		Theme:      "dark",
		DateFormat: "2006-01-02",
	}
}
