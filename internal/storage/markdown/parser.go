package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"github.com/seuusuario/notas/internal/model"
)

// matter mapeia o YAML frontmatter de cada arquivo .md
type matter struct {
	ID        string    `yaml:"id"`
	Title     string    `yaml:"title"`
	Tags      []string  `yaml:"tags"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
	Status    string    `yaml:"status"`
	Priority  string    `yaml:"priority"`
}

// ParseFile lê um arquivo .md e retorna uma Note populada
func ParseFile(path string) (*model.Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fm matter
	rest, err := frontmatter.Parse(strings.NewReader(string(data)), &fm)
	if err != nil {
		// Sem frontmatter válido: usa o arquivo inteiro como corpo
		rest = data
	}

	// Fallback de título: nome do arquivo sem extensão
	title := fm.Title
	if title == "" {
		base := filepath.Base(path)
		title = strings.TrimSuffix(base, filepath.Ext(base))
		title = strings.ReplaceAll(title, "-", " ")
		title = strings.ReplaceAll(title, "_", " ")
	}

	now := time.Now()
	createdAt := fm.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := fm.UpdatedAt
	if updatedAt.IsZero() {
		// Tenta usar o mtime real do arquivo
		if info, err := os.Stat(path); err == nil {
			updatedAt = info.ModTime()
		} else {
			updatedAt = now
		}
	}

	// ID: usa o do frontmatter ou deriva do slug
	slug := Slugify(title)
	id := fm.ID
	if id == "" {
		id = slug
	}

	note := &model.Note{
		ID:        id,
		Slug:      slug,
		Title:     title,
		Body:      string(rest),
		Tags:      fm.Tags,
		Path:      path,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Properties: map[string]string{
			"status":   fm.Status,
			"priority": fm.Priority,
		},
	}

	note.Links = ExtractWikilinks(note.Body)
	return note, nil
}

// ScanVault percorre um diretório e faz parse de todos os .md encontrados
func ScanVault(vaultPath string) ([]*model.Note, error) {
	var notes []*model.Note

	err := filepath.WalkDir(vaultPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // ignora diretórios sem permissão
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir // ignora pastas ocultas (.git, .obsidian, etc.)
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			note, err := ParseFile(path)
			if err != nil {
				return nil // ignora arquivos não parseáveis
			}
			notes = append(notes, note)
		}
		return nil
	})

	return notes, err
}

// ExtractWikilinks encontra todos os [[Link]] e [[Link|Alias]] no corpo
func ExtractWikilinks(body string) []string {
	var links []string
	seen := map[string]bool{}
	i := 0
	for i < len(body)-3 {
		if body[i] == '[' && body[i+1] == '[' {
			end := strings.Index(body[i+2:], "]]")
			if end != -1 {
				raw := body[i+2 : i+2+end]
				// [[Target|Alias]] → pega só Target
				target := strings.TrimSpace(strings.SplitN(raw, "|", 2)[0])
				if target != "" && !seen[target] {
					links = append(links, target)
					seen[target] = true
				}
				i += end + 4
				continue
			}
		}
		i++
	}
	return links
}

// Slugify converte um título em slug URL-safe
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}
