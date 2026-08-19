package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
func (d noteDelegate) Spacing() int                           { return 1 }
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
	width, height int
	noteList      list.Model
	fuzzyOpen     bool
	fuzzyInput    textinput.Model
	fuzzyResults  []noteItem
	fuzzyIndex    int
	allNotes      []noteItem
	statusMsg     string
	cfg           *config.Config
	loading       bool
	pendingDelete *model.Note
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
		noteList:  l,
		fuzzyInput: ti,
		statusMsg: fmt.Sprintf("Indexando vault: %s ...", cfg.VaultPath),
		cfg:       cfg,
		loading:   true,
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
		m.statusMsg = fmt.Sprintf("Ctrl+N nova  |  Ctrl+P buscar  |  Enter abrir  |  d deletar  |  q sair  (%d notas)", len(msg.notes))
		return m, nil

	case tea.KeyMsg:
		// Confirmação de delete — intercepta todas as teclas
		if m.pendingDelete != nil {
			if msg.String() == "d" {
				return m, deleteNote(m.pendingDelete.Path)
			}
			m.pendingDelete = nil
			m.statusMsg = fmt.Sprintf("Ctrl+N nova  |  Ctrl+P buscar  |  Enter abrir  |  d deletar  |  q sair  (%d notas)", len(m.allNotes))
			return m, nil
		}
		if msg.String() == "esc" && m.fuzzyOpen {
			m.fuzzyOpen = false
			m.fuzzyInput.SetValue("")
			m.fuzzyResults = m.allNotes
			m.statusMsg = fmt.Sprintf("Ctrl+N nova  |  Ctrl+P buscar  |  Enter abrir  |  d deletar  |  q sair  (%d notas)", len(m.allNotes))
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
		if msg.String() == "ctrl+p" {
			m.fuzzyOpen = !m.fuzzyOpen
			m.fuzzyIndex = 0
			m.fuzzyResults = m.allNotes
			m.statusMsg = "↑↓ navegar  |  Enter abrir  |  Esc fechar"
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
				m.fuzzyResults = filterNotes(m.allNotes, m.fuzzyInput.Value())
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

func filterNotes(notes []noteItem, q string) []noteItem {
	if q == "" {
		return notes
	}
	q = strings.ToLower(q)
	var out []noteItem
	for _, ni := range notes {
		if strings.Contains(strings.ToLower(ni.n.Title), q) {
			out = append(out, ni)
		}
	}
	return out
}

func (m Model) View() string {
	if m.width == 0 {
		return "Carregando...\n"
	}

	logo  := styleLogo.Render("◆ notas")
	ver   := lipgloss.NewStyle().Foreground(colorMuted).Render(" v0.1.0")
	count := lipgloss.NewStyle().Foreground(colorTeal).Render(fmt.Sprintf("%d notas", len(m.allNotes)))
	gap   := m.width - lipgloss.Width(logo+ver) - lipgloss.Width(count) - 2
	if gap < 0 {
		gap = 0
	}
	header := logo + ver + strings.Repeat(" ", gap) + count
	sep    := styleSep.Render(strings.Repeat("─", m.width))
	status := styleStatusBar.Width(m.width).Render(m.statusMsg)

	if !m.fuzzyOpen {
		return strings.Join([]string{header, sep, m.noteList.View(), sep, status}, "\n")
	}

	floatTitle := styleFloatTitle.Render("  Buscar Notas")
	input      := "  >> " + m.fuzzyInput.View()
	innerSep   := styleSep.Render(strings.Repeat("─", 52))

	var lines []string
	max := 8
	if len(m.fuzzyResults) < max {
		max = len(m.fuzzyResults)
	}
	for i := 0; i < max; i++ {
		ni := m.fuzzyResults[i]
		tags := ""
		for _, t := range ni.n.Tags {
			tags += styleTag.Render("#"+t)
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
		lines = []string{lipgloss.NewStyle().Foreground(colorMuted).Render("    Nenhuma nota encontrada")}
	}
	counter := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  %d resultado(s)", len(m.fuzzyResults)))
	panel   := styleFloat.Render(strings.Join([]string{floatTitle, input, innerSep, strings.Join(lines, "\n"), innerSep, counter}, "\n"))

	hOffset := (m.width - lipgloss.Width(panel)) / 2
	if hOffset < 0 {
		hOffset = 0
	}
	pad := strings.Repeat(" ", hOffset)
	return strings.Join([]string{header, sep, pad + panel, sep, status}, "\n")
}

func main() {
	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
