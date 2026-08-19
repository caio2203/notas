package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver registrado como "sqlite"
	"github.com/seuusuario/notas/internal/model"
)

// DB encapsula a conexão com o banco e todas as operações
type DB struct {
	conn *sql.DB
}

// Open abre (ou cria) o banco SQLite no caminho indicado
func Open(path string) (*DB, error) {
	// Garante que o diretório pai existe
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}

	db := &DB{conn: conn}

	if err := db.applyPragmas(); err != nil {
		return nil, err
	}
	if err := db.migrate(); err != nil {
		return nil, err
	}

	return db, nil
}

// applyPragmas configura o SQLite para máxima performance de leitura
func (db *DB) applyPragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",      // escrita sem bloquear leituras
		"PRAGMA synchronous=NORMAL",    // balanceia durabilidade e velocidade
		"PRAGMA cache_size=-32000",     // 32 MB de cache em memória
		"PRAGMA foreign_keys=ON",       // integridade referencial
		"PRAGMA temp_store=MEMORY",     // operações temporárias em RAM
	}
	for _, p := range pragmas {
		if _, err := db.conn.Exec(p); err != nil {
			return fmt.Errorf("pragma '%s': %w", p, err)
		}
	}
	return nil
}

// migrate cria as tabelas se ainda não existirem (idempotente)
func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS notes (
		id          TEXT PRIMARY KEY,
		slug        TEXT UNIQUE NOT NULL,
		title       TEXT NOT NULL,
		file_path   TEXT NOT NULL,
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS tags (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS note_tags (
		note_id TEXT    NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
		tag_id  INTEGER NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
		PRIMARY KEY (note_id, tag_id)
	);

	CREATE TABLE IF NOT EXISTS links (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		source_id   TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
		target_slug TEXT NOT NULL,
		alias       TEXT DEFAULT '',
		UNIQUE(source_id, target_slug)
	);

	CREATE INDEX IF NOT EXISTS idx_notes_updated ON notes(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_links_source  ON links(source_id);
	CREATE INDEX IF NOT EXISTS idx_links_target  ON links(target_slug);
	`
	_, err := db.conn.Exec(schema)
	return err
}

func (db *DB) Close() error { return db.conn.Close() }

// ─────────────────────────────────────────────────────────────────────────────
// ESCRITA
// ─────────────────────────────────────────────────────────────────────────────

// UpsertNote insere ou atualiza uma nota com suas tags e links (numa transação)
func (db *DB) UpsertNote(n *model.Note) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO notes (id, slug, title, file_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			slug       = excluded.slug,
			title      = excluded.title,
			file_path  = excluded.file_path,
			updated_at = excluded.updated_at`,
		n.ID, n.Slug, n.Title, n.Path,
		n.CreatedAt.Format(time.RFC3339),
		n.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert note '%s': %w", n.Title, err)
	}

	// Limpa relações antigas antes de reinserir
	tx.Exec(`DELETE FROM note_tags WHERE note_id=?`, n.ID)
	tx.Exec(`DELETE FROM links     WHERE source_id=?`, n.ID)

	// Insere tags
	for _, tag := range n.Tags {
		if tag = strings.TrimSpace(tag); tag == "" {
			continue
		}
		tx.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag)
		var tagID int64
		tx.QueryRow(`SELECT id FROM tags WHERE name=?`, tag).Scan(&tagID)
		tx.Exec(`INSERT OR IGNORE INTO note_tags (note_id, tag_id) VALUES (?,?)`, n.ID, tagID)
	}

	// Insere links (wikilinks extraídos do corpo)
	for _, target := range n.Links {
		if target = strings.TrimSpace(target); target == "" {
			continue
		}
		tx.Exec(`INSERT OR IGNORE INTO links (source_id, target_slug) VALUES (?,?)`,
			n.ID, target)
	}

	return tx.Commit()
}

// RebuildIndex apaga todo o índice e reinsere todas as notas fornecidas
func (db *DB) RebuildIndex(notes []*model.Note) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Limpa tudo (CASCADE cuida de note_tags e links)
	tx.Exec(`DELETE FROM notes`)
	tx.Exec(`DELETE FROM tags`)

	if err := tx.Commit(); err != nil {
		return err
	}

	// Reindexa em batch
	for _, n := range notes {
		if err := db.UpsertNote(n); err != nil {
			// Não aborta tudo por uma nota problemática — apenas loga
			fmt.Fprintf(os.Stderr, "warn: skip '%s': %v\n", n.Title, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// LEITURA
// ─────────────────────────────────────────────────────────────────────────────

// AllNotes retorna todas as notas ordenadas por data de atualização (mais recentes primeiro)
func (db *DB) AllNotes() ([]*model.Note, error) {
	return db.queryNotes(`
		SELECT n.id, n.slug, n.title, n.file_path, n.created_at, n.updated_at,
		       GROUP_CONCAT(t.name, ',') AS tags
		FROM   notes n
		LEFT JOIN note_tags nt ON nt.note_id = n.id
		LEFT JOIN tags t       ON t.id = nt.tag_id
		GROUP BY n.id
		ORDER BY n.updated_at DESC
	`)
}

// SearchNotes busca notas cujo título contenha o termo (case-insensitive)
func (db *DB) SearchNotes(query string) ([]*model.Note, error) {
	return db.queryNotes(`
		SELECT n.id, n.slug, n.title, n.file_path, n.created_at, n.updated_at,
		       GROUP_CONCAT(t.name, ',') AS tags
		FROM   notes n
		LEFT JOIN note_tags nt ON nt.note_id = n.id
		LEFT JOIN tags t       ON t.id = nt.tag_id
		WHERE  lower(n.title) LIKE lower('%'||?||'%')
		GROUP BY n.id
		ORDER BY n.updated_at DESC
		LIMIT 50
	`, query)
}

// Count retorna o total de notas indexadas
func (db *DB) Count() int {
	var n int
	db.conn.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n)
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// HELPERS INTERNOS
// ─────────────────────────────────────────────────────────────────────────────

func (db *DB) queryNotes(query string, args ...any) ([]*model.Note, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*model.Note
	for rows.Next() {
		var n model.Note
		var createdStr, updatedStr string
		var tagsStr sql.NullString

		if err := rows.Scan(&n.ID, &n.Slug, &n.Title, &n.Path,
			&createdStr, &updatedStr, &tagsStr); err != nil {
			continue
		}

		n.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		n.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

		if tagsStr.Valid && tagsStr.String != "" {
			n.Tags = strings.Split(tagsStr.String, ",")
		}

		notes = append(notes, &n)
	}
	return notes, rows.Err()
}
