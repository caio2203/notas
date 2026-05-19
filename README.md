# notas — Um sistema de notas TUI para Linux

> **Notas** une a flexibilidade de propriedades do Notion com o sistema de conhecimento em
> rede do Obsidian, entregando tudo dentro do terminal — em um único binário Go, rápido e sem
> dependências externas.

---

## Visão Geral

`notas` é um aplicativo de anotações com interface de terminal (TUI) escrito em Go puro.
Ele foi projetado para desenvolvedores e pesquisadores que vivem no terminal e querem um
sistema de notas que seja:

- **Rápido**: busca fuzzy em tempo real sobre milhares de notas.
- **Portável**: um único binário estático, sem CGO, sem dependências de sistema.
- **Interoperável**: armazena tudo em arquivos Markdown padrão — funciona com qualquer editor.
- **Conectado**: links bidirecionais `[[wikilinks]]` criam um grafo de conhecimento navegável.
- **Extensível**: propriedades YAML frontmatter tipadas (Notion-like) indexadas para busca instantânea.

---

## Filosofia de Design

| Princípio          | Decisão                                                      |
|--------------------|--------------------------------------------------------------|
| Terminal-first     | TUI nativa via Bubble Tea — sem servidor web, sem Electron   |
| Telescope UX       | Floating panels, fuzzy search, listas filtráveis em tempo real|
| Dados seus         | Markdown puro no disco — migre para qualquer ferramenta      |
| Velocidade         | SQLite como índice — nunca como fonte de verdade             |
| Binário único      | `go build` gera um executável estático ~10 MB                |

---

## Arquitetura de Dados

### Estratégia Híbrida: Markdown + SQLite

O sistema usa dois tipos de armazenamento com papéis complementares e bem definidos:

```
┌─────────────────────────────────────────────────┐
│  Arquivos Markdown (.md)  — Fonte de Verdade     │
│  ~/.notas/vault/                                 │
│  • Conteúdo completo da nota                     │
│  • Propriedades (YAML frontmatter)               │
│  • Links [[bidirecionais]]                       │
│  • Legíveis/editáveis por qualquer ferramenta    │
└──────────────┬──────────────────────────────────┘
               │  Parser analisa e indexa
               ▼
┌─────────────────────────────────────────────────┐
│  SQLite (index.db)  — Cache de Índices           │
│  ~/.config/notas/index.db                        │
│  • Títulos, slugs, datas para busca rápida       │
│  • Grafo de links bidirecionais                  │
│  • Tags e propriedades tipadas                   │
│  • FTS5 para busca full-text                     │
│  Pode ser DELETADO e reconstruído a qualquer     │
│  momento com `notas index --rebuild`             │
└─────────────────────────────────────────────────┘
```

**Regra de ouro**: O SQLite é descartável. Se corrompido, `notas index --rebuild` varre
todos os `.md` e reconstrói o índice completo.

### Formato de uma Nota

```markdown
---
id: 01HZ8K9MXPQ3V7RNWT4GBYE5F
title: "Arquitetura Hexagonal"
created_at: 2025-01-15T10:30:00Z
updated_at: 2025-06-01T18:45:00Z
tags: [arquitetura, go, design-patterns]
status: published
priority: high
type: concept
---

# Arquitetura Hexagonal

Também conhecida como **Ports and Adapters**, esta arquitetura...

## Relações

- Ver também: [[Domain-Driven Design]]
- Implementação em Go: [[Projeto Exemplo Hexagonal]]
- Contrasta com: [[Arquitetura em Camadas]]
```

O campo `id` usa ULID (Universally Unique Lexicographically Sortable Identifier), garantindo
ordenação cronológica sem colisões.

### Schema SQLite

```sql
-- Tabela principal de notas (metadados para busca rápida)
CREATE TABLE notes (
    id          TEXT PRIMARY KEY,  -- ULID
    slug        TEXT UNIQUE NOT NULL,
    title       TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    checksum    TEXT NOT NULL      -- SHA256 do arquivo para detectar mudanças
);

-- Índice full-text (FTS5) para busca no conteúdo
CREATE VIRTUAL TABLE notes_fts USING fts5(
    id UNINDEXED,
    title,
    body,
    content='notes'
);

-- Grafo de links bidirecionais
CREATE TABLE links (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id   TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    target_id   TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    alias       TEXT,                -- [[Target|Alias visível]]
    created_at  DATETIME NOT NULL,
    UNIQUE(source_id, target_id)
);

-- Tags (normalizado para busca eficiente)
CREATE TABLE tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    name    TEXT UNIQUE NOT NULL
);

CREATE TABLE note_tags (
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

-- Propriedades arbitrárias tipadas (como Notion)
CREATE TABLE properties (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id   TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    key       TEXT NOT NULL,
    value     TEXT NOT NULL,
    type      TEXT NOT NULL  -- "string" | "number" | "boolean" | "date" | "list"
);

-- Índices para performance
CREATE INDEX idx_links_source   ON links(source_id);
CREATE INDEX idx_links_target   ON links(target_id);
CREATE INDEX idx_props_key      ON properties(key, value);
CREATE INDEX idx_notes_updated  ON notes(updated_at DESC);
```

---

## Estrutura de Diretórios

```
notas/
├── cmd/
│   └── notas/
│       └── main.go              # Entrypoint — parse de flags e bootstrap
│
├── internal/
│   ├── app/
│   │   └── app.go               # Instancia e conecta todos os serviços
│   │
│   ├── tui/                     # Camada de interface (Bubble Tea)
│   │   ├── tui.go               # Model raiz, roteamento de mensagens
│   │   ├── keys/
│   │   │   └── keymap.go        # Mapeamento global de teclas
│   │   ├── views/
│   │   │   ├── dashboard.go     # Tela inicial com lista de notas recentes
│   │   │   ├── editor.go        # Editor com textarea + preview Markdown
│   │   │   └── graph.go         # Visualização do grafo de links (ASCII)
│   │   └── components/
│   │       ├── fuzzy/
│   │       │   └── fuzzy.go     # Componente de busca fuzzy (Telescope-style)
│   │       ├── float/
│   │       │   └── panel.go     # Sistema de painéis flutuantes
│   │       ├── props/
│   │       │   └── editor.go    # Editor de propriedades (Notion-like)
│   │       └── statusbar/
│   │           └── statusbar.go # Barra de status inferior
│   │
│   ├── service/                 # Lógica de negócio (sem dependências de UI)
│   │   ├── note.go              # NoteService: CRUD de notas
│   │   ├── link.go              # LinkService: grafo de backlinks
│   │   ├── index.go             # IndexService: rebuild, sync, watch
│   │   └── search.go            # SearchService: fuzzy + FTS5
│   │
│   ├── storage/                 # Adaptadores de persistência
│   │   ├── sqlite/
│   │   │   ├── db.go            # Conexão, migrations, pragma tuning
│   │   │   ├── note_repo.go     # Repositório de notas
│   │   │   ├── link_repo.go     # Repositório de links
│   │   │   └── migrations/
│   │   │       └── 001_init.sql # Migration inicial
│   │   └── markdown/
│   │       ├── parser.go        # Parse de frontmatter YAML + wikilinks
│   │       └── writer.go        # Serializa Note -> arquivo .md
│   │
│   ├── model/                   # Tipos de domínio (structs puras, sem lógica)
│   │   ├── note.go
│   │   ├── link.go
│   │   └── property.go
│   │
│   └── config/
│       └── config.go            # Leitura de ~/.config/notas/config.toml
│
├── assets/
│   └── themes/
│       ├── dark.toml            # Tema escuro padrão
│       └── light.toml           # Tema claro
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## Atalhos de Teclado (Keymap padrão)

| Tecla             | Ação                                      |
|-------------------|-------------------------------------------|
| `Ctrl+P`          | Abrir fuzzy finder (todas as notas)       |
| `Ctrl+N`          | Nova nota                                 |
| `Ctrl+F`          | Busca full-text no conteúdo               |
| `Ctrl+T`          | Filtrar por tag                           |
| `Ctrl+L`          | Ver backlinks da nota atual               |
| `Ctrl+E`          | Abrir nota no editor externo (`$EDITOR`)  |
| `Ctrl+P`          | Painel de propriedades                    |
| `[[ `             | Autocompletar wikilink (dentro do editor) |
| `Esc`             | Fechar painel flutuante / voltar          |
| `?`               | Ajuda inline                              |

---

## Configuração (`~/.config/notas/config.toml`)

```toml
[vault]
path = "~/notas/vault"       # Diretório das notas Markdown

[editor]
external = "$EDITOR"         # Comando para abrir editor externo
preview = true               # Pré-visualização Markdown inline

[database]
path = "~/.config/notas/index.db"

[ui]
theme = "dark"               # "dark" | "light" | caminho para arquivo .toml
fuzzy_limit = 50             # Máx. de resultados no fuzzy finder
date_format = "2006-01-02"   # Formato Go de data

[sync]
watch = true                 # Monitorar vault com inotify para reindexar
```

---

## Dependências

| Pacote                            | Versão  | Uso                              |
|-----------------------------------|---------|----------------------------------|
| `github.com/charmbracelet/bubbletea` | v1.x  | Framework TUI principal          |
| `github.com/charmbracelet/lipgloss`  | v1.x  | Estilização terminal             |
| `github.com/charmbracelet/bubbles`   | v0.x  | Componentes (list, input, etc.)  |
| `modernc.org/sqlite`                 | latest | SQLite puro Go (sem CGO)         |
| `github.com/adrg/frontmatter`        | latest | Parse YAML frontmatter           |
| `github.com/oklog/ulid/v2`           | latest | Geração de IDs ULID              |
| `github.com/BurntSushi/toml`         | latest | Parse de config.toml             |
| `github.com/sahilm/fuzzy`            | latest | Algoritmo de busca fuzzy         |
| `github.com/fsnotify/fsnotify`       | latest | Watch de arquivos (inotify)      |

---

## Compilação

```bash
# Build padrão (Linux amd64, binário único)
make build

# Build otimizado para release (com strip de debug info)
make release

# Cross-compile para múltiplas plataformas
make build-all
```

`Makefile`:
```makefile
BINARY  := notas
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/notas

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -ldflags "$(LDFLAGS)" -trimpath -o bin/$(BINARY) ./cmd/notas

build-all:
	GOOS=linux  GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64  ./cmd/notas
	GOOS=linux  GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64  ./cmd/notas
	GOOS=darwin GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 ./cmd/notas

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...
```

---

## Licença

MIT © 2025 — Contribuições são bem-vindas.
