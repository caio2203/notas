#!/usr/bin/env bash
# setup.sh — Inicializa o projeto notas do zero
# Uso: bash setup.sh
set -euo pipefail

echo "==> Criando estrutura de diretórios..."
mkdir -p notas/{cmd/notas,assets/themes}
mkdir -p notas/internal/{app,config,model}
mkdir -p notas/internal/tui/{keys,views,components/{fuzzy,float,props,statusbar}}
mkdir -p notas/internal/{service,storage/{sqlite/migrations,markdown}}

cd notas

echo "==> Inicializando módulo Go..."
go mod init github.com/seuusuario/notas

echo "==> Instalando dependências..."
# Framework TUI principal
go get github.com/charmbracelet/bubbletea@latest
# Estilização de terminal (cores, bordas, layouts)
go get github.com/charmbracelet/lipgloss@latest
# Componentes prontos (list, textarea, viewport, spinner)
go get github.com/charmbracelet/bubbles@latest
# SQLite puro em Go — zero CGO, binário único garantido
go get modernc.org/sqlite@latest
# Parse de YAML frontmatter nos arquivos .md
go get github.com/adrg/frontmatter@latest
# IDs ULID (sortable unique IDs)
go get github.com/oklog/ulid/v2@latest
# Parse de config.toml
go get github.com/BurntSushi/toml@latest
# Algoritmo de busca fuzzy em Go puro
go get github.com/sahilm/fuzzy@latest
# Watch de arquivos do sistema via inotify
go get github.com/fsnotify/fsnotify@latest

echo "==> Organizando go.mod..."
go mod tidy

echo ""
echo "✓ Setup concluído! Execute: cd notas && go run ./cmd/notas"
