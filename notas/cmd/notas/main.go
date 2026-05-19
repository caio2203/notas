package main

import (
	"io"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

var (
	colorPurple = lipgloss.Color("135")
	colorTeal   = lipgloss.Color("80")
	colorMuted  = lipgloss.Color("244")
	colorText   = lipgloss.Color("252")
	colorAmber  = lipgloss.Color("214")
	colorBorder = lipgloss.Color("239")
	colorSurface = lipgloss.Color("237")

	styleHeader = lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Padding(0, 1)
	styleLogo   = lipgloss.NewStyle().Foreground(colorTeal).Bold(true)
	styleFloat  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Background(colorSurface).
			Padding(1, 2).
			Width(60)
	styleFloatTitle = lipgloss.NewStyle().Foreground(colorPurple).Bold(true).MarginBottom(1)
	styleSelected   = lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	styleResult     = lipgloss.NewStyle().Foreground(colorText)
	styleTag        = lipgloss.NewStyle().Foreground(colorAmber).Padding(0, 1)
	styleSep        = lipgloss.NewStyle().Foreground(colorBorder)
	styleStatusBar  = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
)

type Note struct {
	ID    string
	Title string
	Tags  []string
	Path  string
}

func (n Note) FilterValue() string { return n.Title }

type noteDelegate struct{}
func (d noteDelegate) Height() int  { return 2 }
func (d noteDelegate) Spacing() int { return 1 }
func (d noteDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d noteDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	note, ok := item.(Note)
	if !ok { return }
	isSelected := index == m.Index()
	title := note.Title
	if isSelected {
		title = styleSelected.Render("▸ " + title)
	} else {
		title = styleResult.Render("  " + title)
	}
	tags := ""
	for _, t := range note.Tags {
		tags += styleTag.Render("#"+t) + " "
	}
	fmt.Fprint(w, title + "\n    " + lipgloss.NewStyle().Foreground(colorMuted).Render(tags))
}

type Model struct {
	width, height int
	noteList      list.Model
	fuzzyOpen     bool
	fuzzyInput    textinput.Model
	fuzzyResults  []Note
	fuzzyIndex    int
	allNotes      []Note
	statusMsg     string
}

func sampleNotes() []Note {
	return []Note{
		{ID: "01HZ001", Title: "Arquitetura Hexagonal",    Tags: []string{"arquitetura", "go"}},
		{ID: "01HZ002", Title: "Domain-Driven Design",     Tags: []string{"ddd", "arquitetura"}},
		{ID: "01HZ003", Title: "Bubble Tea Patterns",      Tags: []string{"go", "tui"}},
		{ID: "01HZ004", Title: "SQLite Performance Tuning",Tags: []string{"database", "sqlite"}},
		{ID: "01HZ005", Title: "Go Concurrency Patterns",  Tags: []string{"go", "concurrency"}},
		{ID: "01HZ006", Title: "Zettelkasten Method",      Tags: []string{"notas", "produtividade"}},
		{ID: "01HZ007", Title: "Linux Internals inotify",  Tags: []string{"linux", "kernel"}},
		{ID: "01HZ008", Title: "YAML Frontmatter Spec",    Tags: []string{"markdown", "yaml"}},
	}
}

func InitialModel() Model {
	notes := sampleNotes()
	items := make([]list.Item, len(notes))
	for i, n := range notes { items[i] = n }

	l := list.New(items, noteDelegate{}, 0, 0)
	l.Title = "notas"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = styleHeader

	ti := textinput.New()
	ti.Placeholder = "Buscar notas..."
	ti.CharLimit = 100
	ti.Width = 50

	return Model{
		noteList:     l,
		fuzzyInput:   ti,
		fuzzyResults: notes,
		allNotes:     notes,
		statusMsg:    "Ctrl+P buscar  |  q sair  |  ? ajuda",
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.noteList.SetSize(msg.Width-2, msg.Height-5)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "esc" && m.fuzzyOpen {
			m.fuzzyOpen = false
			m.fuzzyInput.SetValue("")
			m.fuzzyResults = m.allNotes
			m.statusMsg = "Ctrl+P buscar  |  q sair  |  ? ajuda"
			return m, nil
		}
		if msg.String() == "ctrl+c" || (!m.fuzzyOpen && msg.String() == "q") {
			return m, tea.Quit
		}
		if msg.String() == "ctrl+p" {
			m.fuzzyOpen = !m.fuzzyOpen
			m.fuzzyIndex = 0
			m.fuzzyResults = m.allNotes
			m.statusMsg = "↑↓ navegar  |  Enter abrir  |  Esc fechar"
			if m.fuzzyOpen { return m, m.fuzzyInput.Focus() }
			return m, nil
		}
		if m.fuzzyOpen {
			switch msg.String() {
			case "up":
				if m.fuzzyIndex > 0 { m.fuzzyIndex-- }
			case "down":
				if m.fuzzyIndex < len(m.fuzzyResults)-1 { m.fuzzyIndex++ }
			case "enter":
				if len(m.fuzzyResults) > 0 {
					sel := m.fuzzyResults[m.fuzzyIndex]
					m.fuzzyOpen = false
					m.fuzzyInput.SetValue("")
					m.statusMsg = fmt.Sprintf("Aberto: %s", sel.Title)
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

func filterNotes(notes []Note, q string) []Note {
	if q == "" { return notes }
	q = strings.ToLower(q)
	var out []Note
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n.Title), q) { out = append(out, n) }
	}
	return out
}

func (m Model) View() string {
	if m.width == 0 { return "Carregando...\n" }

	logo    := styleLogo.Render("◆ notas")
	ver     := lipgloss.NewStyle().Foreground(colorMuted).Render(" v0.1.0")
	count   := lipgloss.NewStyle().Foreground(colorTeal).Render(fmt.Sprintf("%d notas", len(m.allNotes)))
	gap     := m.width - lipgloss.Width(logo+ver) - lipgloss.Width(count) - 2
	if gap < 0 { gap = 0 }
	header  := logo + ver + strings.Repeat(" ", gap) + count
	sep     := styleSep.Render(strings.Repeat("─", m.width))
	status  := styleStatusBar.Width(m.width).Render(m.statusMsg)

	if !m.fuzzyOpen {
		return strings.Join([]string{header, sep, m.noteList.View(), sep, status}, "\n")
	}

	// Painel flutuante
	floatTitle := styleFloatTitle.Render("  Buscar Notas")
	input      := "  >> " + m.fuzzyInput.View()
	innerSep   := styleSep.Render(strings.Repeat("─", 52))

	var lines []string
	max := 8
	if len(m.fuzzyResults) < max { max = len(m.fuzzyResults) }
	for i := 0; i < max; i++ {
		n := m.fuzzyResults[i]
		tags := ""
		for _, t := range n.Tags { tags += styleTag.Render("#"+t) }
		if i == m.fuzzyIndex {
			lines = append(lines, styleSelected.Render("  ▸ "+n.Title))
			if tags != "" { lines = append(lines, "     "+tags) }
		} else {
			lines = append(lines, styleResult.Render("    "+n.Title))
		}
	}
	if len(m.fuzzyResults) == 0 {
		lines = []string{lipgloss.NewStyle().Foreground(colorMuted).Render("    Nenhuma nota encontrada")}
	}
	counter := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  %d resultado(s)", len(m.fuzzyResults)))
	panel   := styleFloat.Render(strings.Join([]string{floatTitle, input, innerSep, strings.Join(lines, "\n"), innerSep, counter}, "\n"))

	hOffset := (m.width - lipgloss.Width(panel)) / 2
	if hOffset < 0 { hOffset = 0 }
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
