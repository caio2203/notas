package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/seuusuario/notas/internal/config"
	"github.com/seuusuario/notas/internal/model"
	mdstore "github.com/seuusuario/notas/internal/storage/markdown"
	sqlstore "github.com/seuusuario/notas/internal/storage/sqlite"
)

var (
	colorPurple  = lipgloss.Color("135")
	colorTeal    = lipgloss.Color("80")
	colorMuted   = lipgloss.Color("244")
	colorText    = lipgloss.Color("252")
	colorAmber   = lipgloss.Color("214")
	colorBorder  = lipgloss.Color("239")
	colorSurface = lipgloss.Color("237")

	styleHeader     = lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Padding(0, 1)
	styleLogo       = lipgloss.NewStyle().Foreground(colorTeal).Bold(true)
	styleFloat      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPurple).Background(colorSurface).Padding(1, 2).Width(60)
	styleFloatTitle = lipgloss.NewStyle().Foreground(colorPurple).Bold(true).MarginBottom(1)
	styleSelected   = lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	styleResult     = lipgloss.NewStyle().Foreground(colorText)
	styleTag        = lipgloss.NewStyle().Foreground(colorAmber).Padding(0, 1)
	styleSep        = lipgloss.NewStyle().Foreground(colorBorder)
	styleStatusBar  = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	styleError      = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

// noteItem adapta *model.Note para satisfazer list.Item
type noteItem struct{ n *model.Note }

func (i noteItem) FilterValue() string { return i.n.Title }

type noteDelegate struct{}

func (d noteDelegate) Height() int                             { return 2 }
func (d noteDelegate) Spacing() int                            { return 1 }
func (d noteDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d noteDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ni, ok := item.(noteItem)
	if !ok {
		return
	}
	isSelected := index == m.Index()
	title := ni.n.Title
	if isSelected {
		title = styleSelected.Render("▸ " + title)
	} else {
		title = styleResult.Render("  " + title)
	}
	tags := ""
	for _, t := range ni.n.Tags {
		tags += styleTag.Render("#"+t) + " "
	}
	fmt.Fprint(w, title+"\n    "+lipgloss.NewStyle().Foreground(colorMuted).Render(tags))
}

// indexDoneMsg é enviado quando o scan do vault + indexação concluem
type indexDoneMsg struct {
	notes []*model.Note
	err   error
}

// editorDoneMsg é enviado quando o editor externo fecha
type editorDoneMsg struct{ err error }

// newNoteReadyMsg é enviado após o arquivo da nova nota ser criado no vault
type newNoteReadyMsg struct{ path string }

// createNote gera um arquivo .md com frontmatter no vault e avisa quando pronto
func createNote(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		slug := "nota-" + now.Format("20060102-150405")
		path := filepath.Join(cfg.VaultPath, slug+".md")
		content := fmt.Sprintf("---\nid: %s\ntitle: Nova Nota\ntags: []\ncreated_at: %s\nupdated_at: %s\n---\n\n",
			slug, now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err := os.MkdirAll(cfg.VaultPath, 0755); err != nil {
			return editorDoneMsg{err: fmt.Errorf("criar vault: %w", err)}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return editorDoneMsg{err: fmt.Errorf("criar nota: %w", err)}
		}
		return newNoteReadyMsg{path: path}
	}
}

// deleteDoneMsg é enviado após a exclusão do arquivo no vault
type deleteDoneMsg struct{ err error }

// deleteNote remove o arquivo .md do vault
func deleteNote(path string) tea.Cmd {
	return func() tea.Msg {
		return deleteDoneMsg{err: os.Remove(path)}
	}
}

// openInEditor suspende a TUI, abre o arquivo no $EDITOR e retorna
func openInEditor(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{err: err}
	})
}

type Model struct {
	width, height  int
	noteList       list.Model
	fuzzyOpen      bool
	fuzzyInput     textinput.Model
	fuzzyResults   []noteItem
	fuzzyBase      []noteItem // conjunto sobre o qual a busca filtra (todas ou backlinks)
	fuzzyMode      string     // "search" | "backlinks"
	fuzzyIndex     int
	allNotes       []noteItem
	statusMsg      string
	cfg            *config.Config
	loading        bool
	pendingDelete  *model.Note
	previewOpen    bool
	previewNote    *model.Note
	previewLinks   []*model.Note // wikilinks de saída resolvidos (para seguir com 1-9)
	previewHistory []*model.Note // pilha para "voltar" (Backspace)
	viewport       viewport.Model
}

// resolveLink acha a nota-alvo de um wikilink (por slug ou título).
func resolveLink(all []noteItem, target string) *model.Note {
	ts := mdstore.Slugify(target)
	for _, ni := range all {
		if ni.n.Slug == ts || strings.EqualFold(ni.n.Title, target) {
			return ni.n
		}
	}
	return nil
}

// outgoingLinks resolve os [[wikilinks]] da nota para notas existentes (dedup).
// ponytail: numeração 1-9 no preview; >9 links seguem via lista se um dia precisar.
func outgoingLinks(all []noteItem, n *model.Note) []*model.Note {
	var out []*model.Note
	seen := map[string]bool{}
	for _, l := range n.Links {
		if t := resolveLink(all, l); t != nil && !seen[t.ID] {
			out = append(out, t)
			seen[t.ID] = true
		}
	}
	return out
}

// previewStatus monta a barra do preview, listando os links seguíveis (até 9).
func previewStatus(links []*model.Note) string {
	if len(links) == 0 {
		return "E editar  |  ↑↓ / PgUp / PgDn rolar  |  Esc voltar"
	}
	s := "Ir p/: "
	for i, l := range links {
		if i == 9 {
			break
		}
		s += fmt.Sprintf("[%d] %s  ", i+1, l.Title)
	}
	return s + "|  E editar  |  Bksp voltar  |  Esc sair"
}

// previewBody devolve o corpo pronto para render (placeholder se vazio)
func previewBody(n *model.Note) string {
	if strings.TrimSpace(n.Body) == "" {
		return "*(nota vazia)*"
	}
	return n.Body
}

// renderMarkdown converte markdown em texto estilizado para o terminal
func renderMarkdown(body string, width int) string {
	if width < 20 {
		width = 20 // piso p/ evitar wrap inválido antes do tamanho da janela chegar
	}
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		return body
	}
	out, err := r.Render(body)
	if err != nil {
		return body
	}
	return out
}

// setPreview renderiza a nota no painel de leitura e recalcula os links seguíveis.
// Não mexe no histórico — quem chama decide (abrir do zero, seguir link ou voltar).
func (m *Model) setPreview(n *model.Note) {
	m.previewOpen = true
	m.previewNote = n
	m.viewport = viewport.New(m.width-2, m.height-5)
	m.viewport.SetContent(renderMarkdown(previewBody(n), m.width-4))
	m.previewLinks = outgoingLinks(m.allNotes, n)
	m.statusMsg = previewStatus(m.previewLinks)
}

func InitialModel() Model {
	cfg := config.Default()

	l := list.New(nil, noteDelegate{}, 0, 0)
	l.Title = "notas"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = styleHeader

	ti := textinput.New()
	ti.Placeholder = "Buscar notas..."
	ti.CharLimit = 100
	ti.Width = 50

	return Model{
		noteList:   l,
		fuzzyInput: ti,
		statusMsg:  fmt.Sprintf("Indexando vault: %s ...", cfg.VaultPath),
		cfg:        cfg,
		loading:    true,
	}
}

// loadVault escaneia o vault e indexa no SQLite em background (tea.Cmd)
func loadVault(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		notes, err := mdstore.ScanVault(cfg.VaultPath)
		if err != nil {
			return indexDoneMsg{err: fmt.Errorf("scan vault '%s': %w", cfg.VaultPath, err)}
		}

		db, err := sqlstore.Open(cfg.DBPath)
		if err != nil {
			// Retorna as notas mesmo sem DB — exibe sem persistência
			fmt.Fprintf(os.Stderr, "warn: não foi possível abrir SQLite: %v\n", err)
			return indexDoneMsg{notes: notes}
		}
		defer db.Close()

		if err := db.RebuildIndex(notes); err != nil {
			fmt.Fprintf(os.Stderr, "warn: erro ao indexar: %v\n", err)
		}

		return indexDoneMsg{notes: notes}
	}
}

func (m Model) Init() tea.Cmd {
	return loadVault(m.cfg)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.noteList.SetSize(msg.Width-2, msg.Height-5)
		if m.previewOpen {
			m.viewport.Width = msg.Width - 2
			m.viewport.Height = msg.Height - 5
			m.viewport.SetContent(renderMarkdown(previewBody(m.previewNote), msg.Width-4))
		}
		return m, nil

	case newNoteReadyMsg:
		return m, openInEditor(msg.path)

	case deleteDoneMsg:
		m.pendingDelete = nil
		if msg.err != nil {
			m.statusMsg = styleError.Render("Erro ao deletar: " + msg.err.Error())
			return m, nil
		}
		return m, loadVault(m.cfg)

	case editorDoneMsg:
		if msg.err != nil {
			m.statusMsg = styleError.Render("Erro no editor: " + msg.err.Error())
			return m, nil
		}
		// Re-escaneia para capturar edições salvas
		return m, loadVault(m.cfg)

	case indexDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = styleError.Render("Erro: " + msg.err.Error())
			return m, nil
		}
		items := make([]list.Item, len(msg.notes))
		all := make([]noteItem, len(msg.notes))
		for i, n := range msg.notes {
			ni := noteItem{n}
			items[i] = ni
			all[i] = ni
		}
		m.noteList.SetItems(items)
		m.allNotes = all
		m.fuzzyResults = all
		m.statusMsg = helpStatus(len(msg.notes))
		return m, nil

	case tea.KeyMsg:
		// Preview aberto — modal de leitura, intercepta as teclas
		if m.previewOpen {
			s := msg.String()
			switch {
			case s == "ctrl+c":
				return m, tea.Quit
			case s == "e" || s == "E" || s == "enter":
				m.previewOpen = false
				m.previewHistory = nil
				return m, openInEditor(m.previewNote.Path)
			case s == "esc" || s == "q":
				m.previewOpen = false
				m.previewHistory = nil
				m.statusMsg = helpStatus(len(m.allNotes))
				return m, nil
			case s == "backspace":
				if len(m.previewHistory) > 0 {
					prev := m.previewHistory[len(m.previewHistory)-1]
					m.previewHistory = m.previewHistory[:len(m.previewHistory)-1]
					m.setPreview(prev)
				}
				return m, nil
			case len(s) == 1 && s[0] >= '1' && s[0] <= '9':
				if idx := int(s[0] - '1'); idx < len(m.previewLinks) {
					m.previewHistory = append(m.previewHistory, m.previewNote)
					m.setPreview(m.previewLinks[idx])
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
		}
		// Confirmação de delete — intercepta todas as teclas
		if m.pendingDelete != nil {
			if msg.String() == "d" {
				return m, deleteNote(m.pendingDelete.Path)
			}
			m.pendingDelete = nil
			m.statusMsg = helpStatus(len(m.allNotes))
			return m, nil
		}
		if msg.String() == "esc" && m.fuzzyOpen {
			m.fuzzyOpen = false
			m.fuzzyInput.SetValue("")
			m.fuzzyResults = m.allNotes
			m.statusMsg = helpStatus(len(m.allNotes))
			return m, nil
		}
		if msg.String() == "ctrl+c" || (!m.fuzzyOpen && msg.String() == "q") {
			return m, tea.Quit
		}
		if !m.fuzzyOpen && msg.String() == "d" {
			if item := m.noteList.SelectedItem(); item != nil {
				ni := item.(noteItem)
				m.pendingDelete = ni.n
				m.statusMsg = styleError.Render(fmt.Sprintf("Deletar \"%s\"? [d] confirmar  |  qualquer tecla cancela", ni.n.Title))
				return m, nil
			}
		}
		if !m.fuzzyOpen && msg.String() == "ctrl+n" {
			return m, createNote(m.cfg)
		}
		if !m.fuzzyOpen && msg.String() == "enter" {
			if item := m.noteList.SelectedItem(); item != nil {
				ni := item.(noteItem)
				return m, openInEditor(ni.n.Path)
			}
			return m, nil
		}
		if !m.fuzzyOpen && msg.String() == " " {
			if item := m.noteList.SelectedItem(); item != nil {
				ni := item.(noteItem)
				m.previewHistory = nil
				m.setPreview(ni.n)
			}
			return m, nil
		}
		if !m.fuzzyOpen && msg.String() == "b" {
			if item := m.noteList.SelectedItem(); item != nil {
				ni := item.(noteItem)
				m.fuzzyOpen = true
				m.fuzzyMode = "backlinks"
				m.fuzzyBase = backlinksFor(m.allNotes, ni.n)
				m.fuzzyResults = m.fuzzyBase
				m.fuzzyIndex = 0
				m.fuzzyInput.SetValue("")
				m.statusMsg = "↑↓ navegar  |  Enter abrir  |  Esc fechar"
				return m, m.fuzzyInput.Focus()
			}
			return m, nil
		}
		if msg.String() == "ctrl+p" {
			m.fuzzyOpen = !m.fuzzyOpen
			m.fuzzyMode = "search"
			m.fuzzyBase = m.allNotes
			m.fuzzyIndex = 0
			m.fuzzyResults = m.allNotes
			m.fuzzyInput.SetValue("")
			m.statusMsg = "↑↓ navegar  |  Enter abrir  |  Esc fechar  |  #tag filtra por tag"
			if m.fuzzyOpen {
				return m, m.fuzzyInput.Focus()
			}
			return m, nil
		}
		if m.fuzzyOpen {
			switch msg.String() {
			case "up":
				if m.fuzzyIndex > 0 {
					m.fuzzyIndex--
				}
			case "down":
				if m.fuzzyIndex < len(m.fuzzyResults)-1 {
					m.fuzzyIndex++
				}
			case "enter":
				if len(m.fuzzyResults) > 0 {
					sel := m.fuzzyResults[m.fuzzyIndex]
					m.fuzzyOpen = false
					m.fuzzyInput.SetValue("")
					return m, openInEditor(sel.n.Path)
				}
			default:
				var cmd tea.Cmd
				m.fuzzyInput, cmd = m.fuzzyInput.Update(msg)
				m.fuzzyResults = filterNotes(m.fuzzyBase, m.fuzzyInput.Value())
				m.fuzzyIndex = 0
				return m, cmd
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.noteList, cmd = m.noteList.Update(msg)
	return m, cmd
}

// filterNotes faz busca full-text (título + corpo + tags). Prefixo '#' filtra por tag.
// ponytail: busca linear em memória; migrar p/ FTS5 (tabela virtual) só se o vault não couber em RAM.
func filterNotes(notes []noteItem, q string) []noteItem {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return notes
	}
	// #tag → filtra por tag em vez de full-text
	if strings.HasPrefix(q, "#") {
		tag := strings.TrimPrefix(q, "#")
		var out []noteItem
		for _, ni := range notes {
			for _, t := range ni.n.Tags {
				if strings.Contains(strings.ToLower(t), tag) {
					out = append(out, ni)
					break
				}
			}
		}
		return out
	}
	var out []noteItem
	for _, ni := range notes {
		hay := strings.ToLower(ni.n.Title + "\n" + ni.n.Body + "\n" + strings.Join(ni.n.Tags, " "))
		if strings.Contains(hay, q) {
			out = append(out, ni)
		}
	}
	return out
}

// backlinksFor retorna as notas que apontam para target via [[wikilink]].
func backlinksFor(all []noteItem, target *model.Note) []noteItem {
	var out []noteItem
	for _, ni := range all {
		if ni.n.ID == target.ID {
			continue
		}
		for _, l := range ni.n.Links {
			if mdstore.Slugify(l) == target.Slug || strings.EqualFold(l, target.Title) {
				out = append(out, ni)
				break
			}
		}
	}
	return out
}

// helpStatus é a barra de ajuda padrão do rodapé.
func helpStatus(n int) string {
	return fmt.Sprintf("Ctrl+N nova  |  Espaço ler  |  Enter editar  |  Ctrl+P buscar  |  b backlinks  |  d deletar  |  q sair  (%d notas)", n)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Carregando...\n"
	}

	logo := styleLogo.Render("◆ notas")
	ver := lipgloss.NewStyle().Foreground(colorMuted).Render(" v0.1.0")
	count := lipgloss.NewStyle().Foreground(colorTeal).Render(fmt.Sprintf("%d notas", len(m.allNotes)))
	gap := m.width - lipgloss.Width(logo+ver) - lipgloss.Width(count) - 2
	if gap < 0 {
		gap = 0
	}
	header := logo + ver + strings.Repeat(" ", gap) + count
	sep := styleSep.Render(strings.Repeat("─", m.width))
	status := styleStatusBar.Width(m.width).Render(m.statusMsg)

	if m.previewOpen {
		return strings.Join([]string{header, sep, m.viewport.View(), sep, status}, "\n")
	}

	if !m.fuzzyOpen {
		return strings.Join([]string{header, sep, m.noteList.View(), sep, status}, "\n")
	}

	panelTitle := "  Buscar Notas"
	if m.fuzzyMode == "backlinks" {
		panelTitle = "  Backlinks"
	}
	floatTitle := styleFloatTitle.Render(panelTitle)
	input := "  >> " + m.fuzzyInput.View()
	innerSep := styleSep.Render(strings.Repeat("─", 52))

	var lines []string
	max := 8
	if len(m.fuzzyResults) < max {
		max = len(m.fuzzyResults)
	}
	for i := 0; i < max; i++ {
		ni := m.fuzzyResults[i]
		tags := ""
		for _, t := range ni.n.Tags {
			tags += styleTag.Render("#" + t)
		}
		if i == m.fuzzyIndex {
			lines = append(lines, styleSelected.Render("  ▸ "+ni.n.Title))
			if tags != "" {
				lines = append(lines, "     "+tags)
			}
		} else {
			lines = append(lines, styleResult.Render("    "+ni.n.Title))
		}
	}
	if len(m.fuzzyResults) == 0 {
		empty := "    Nenhuma nota encontrada"
		if m.fuzzyMode == "backlinks" {
			empty = "    Nenhuma nota aponta para esta"
		}
		lines = []string{lipgloss.NewStyle().Foreground(colorMuted).Render(empty)}
	}
	counter := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  %d resultado(s)", len(m.fuzzyResults)))
	panel := styleFloat.Render(strings.Join([]string{floatTitle, input, innerSep, strings.Join(lines, "\n"), innerSep, counter}, "\n"))

	// centraliza a caixa inteira (todas as linhas), não só a primeira
	centered := lipgloss.PlaceHorizontal(m.width, lipgloss.Center, panel)
	return strings.Join([]string{header, sep, centered, sep, status}, "\n")
}

func main() {
	vault := flag.String("vault", "", "caminho do vault (sobrepõe $NOTAS_VAULT e o padrão)")
	flag.Parse()
	if *vault != "" {
		os.Setenv("NOTAS_VAULT", *vault) // config.Default() lê essa env
	}

	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
