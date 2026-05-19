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

	// Prioridade 1: variável de ambiente NOTAS_VAULT
	vaultPath := os.Getenv("NOTAS_VAULT")

	// Prioridade 2: pasta vault/ no diretório atual (dentro do projeto)
	if vaultPath == "" {
		cwd, err := os.Getwd()
		if err == nil {
			candidate := filepath.Join(cwd, "vault")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				vaultPath = candidate
			}
		}
	}

	// Prioridade 3: ~/notas/vault (instalação global)
	if vaultPath == "" {
		vaultPath = filepath.Join(home, "notas", "vault")
	}

	return &Config{
		VaultPath:  vaultPath,
		DBPath:     filepath.Join(home, ".config", "notas", "index.db"),
		Theme:      "dark",
		DateFormat: "2006-01-02",
	}
}
