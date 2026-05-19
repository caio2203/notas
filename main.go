// cmd/notas/main.go
//
// Entrypoint do aplicativo notas — sistema de notas TUI para Linux.
// Este arquivo inicializa o Bubble Tea, configura o modelo raiz e
// renderiza a primeira interface: um dashboard com lista de notas
// e um painel de busca fuzzy flutuante demonstrativo.
//
// Compile com: go build -ldflags "-s -w" -o notas ./cmd/notas
// Execute com: ./notas

package main

import (
	"fmt"
	"os"
	"strings"

	// Bubble Tea — framework principal da TUI (padrão Elm Architecture)
	tea "github.com/charmbracelet/bubbletea"

	// Lip Gloss — estilização: cores, bordas, padding, composição de layouts
	"github.com/charmbracelet/lipgloss"

	// Bubbles — componentes prontos: list, textinput, viewport, etc.
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
)

// ─────────────────────────────────────────────────────────────────────────────
// ESTILOS — definidos uma única vez com Lip Gloss
// Usam cores ANSI 256/TrueColor adaptáveis ao terminal.
// ─────────────────────────────────────────────────────────────────────────────

var (
	// Paleta de cores principal
	colorBase    = lipgloss.Color("235") // Fundo escuro
	colorSurface = lipgloss.Color("237") // Superfície de cards
	colorBorder  = lipgloss.Color("239") // Bordas sutis
	colorPurple  = lipgloss.Color("135") // Destaque principal
	colorTeal    = lipgloss.Color("80")  // Destaque secundário
	colorMuted   = lipgloss.Color("244") // Texto secundário
	colorText    = lipgloss.Color("252") // Texto principal
	colorAmber   = lipgloss.Color("214") // Avisos / tags

	// Estilo do container principal (ocupa o terminal inteiro)
	styleApp = lipgloss.NewStyle().
			Background(colorBase).
			Padding(0, 1)

	// Estilo do cabeçalho
	styleHeader = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true).
			Padding(0, 1)

	// Estilo do logo ASCII
	styleLogo = lipgloss.NewStyle().
			Foreground(colorTeal).
			Bold(true)

	// Estilo da barra de status inferior
	styleStatusBar = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMuted).
			Padding(0, 1).
			Width(80)

	// Estilo do painel flutuante (fuzzy finder)
	styleFloat = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Background(colorSurface).
			Padding(1, 2).
			Width(60)

	// Estilo do título do painel flutuante
	styleFloatTitle = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true).
			MarginBottom(1)

	// Estilo de resultado selecionado no fuzzy finder
	styleSelected = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true)

	// Estilo de resultado normal no fuzzy finder
	styleResult = lipgloss.NewStyle().
			Foreground(colorText)

	// Estilo de tag
	styleTag = lipgloss.NewStyle().
			Foreground(colorAmber).
			Padding(0, 1)

	// Estilo do separador
	styleSep = lipgloss.NewStyle().
			Foreground(colorBorder)
)

// ─────────────────────────────────────────────────────────────────────────────
// TIPOS DE DOMÍNIO (simplificados para o boilerplate inicial)
// Na implementação completa, estes virão de internal/model/
// ─────────────────────────────────────────────────────────────────────────────

// Note representa uma nota no sistema (versão simplificada para bootstrap)
type Note struct {
	ID    string
	Title string
	Tags  []string
	Path  string
}

// Implementa a interface list.Item do pacote bubbles/list
func (n Note) FilterValue() string { return n.Title }
func (n Note) Title() string       { return n.Title } // shadowed intencionalmente
func (n Note) Description() string {
	if len(n.Tags) == 0 {
		return "sem tags"
	}
	return strings.Join(n.Tags, " · ")
}
                
// ─────────────────────────────────────────────────────────────────────────────
// DELEGATE CUSTOMIZADO para o bubbles/list
// Controla como cada item da lista é renderizado.
// ─────────────────────────────────────────────────────────────────────────────

type noteDelegate struct{}

func (d noteDelegate) Height() int                             { return 2 }
func (d noteDelegate) Spacing() int                           { return 1 }
func (d noteDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d noteDelegate) Render(w interface{ WriteString(string) (int, error) }, m list.Model, index int, item list.Item) {
	note, ok := item.(Note)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Título da nota
	title := note.Title
	if isSelected {
		title = styleSelected.Render("▸ " + title)
	} else {
		title = styleResult.Render("  " + title)
	}

	// Tags da nota
	tags := ""
	for _, t := range note.Tags {
		tags += styleTag.Render("#"+t) + " "
	}

	w.WriteString(title + "\n" + "    " + lipgloss.NewStyle().Foreground(colorMuted).Render(tags))
}

// ─────────────────────────────────────────────────────────────────────────────
// MENSAGENS CUSTOMIZADAS do Bubble Tea
// Seguem o padrão: tipos vazios ou com dados que disparam transições de estado.
// ─────────────────────────────────────────────────────────────────────────────

// openFuzzyMsg dispara a abertura do painel de busca flutuante
type openFuzzyMsg struct{}

// closeFuzzyMsg dispara o fechamento do painel flutuante
type closeFuzzyMsg struct{}

// ─────────────────────────────────────────────────────────────────────────────
// MODEL — o coração do Bubble Tea (padrão Elm)
// Representa TODO o estado do aplicativo em um único struct.
// O estado é imutável: Update retorna um novo Model a cada mudança.
// ─────────────────────────────────────────────────────────────────────────────

type Model struct {
	// Dimensões do terminal
	width  int
	height int

	// Lista principal de notas (bubbles/list)
	noteList list.Model

	// ── Estado do Painel Flutuante (Fuzzy Finder) ──────────────────────────
	fuzzyOpen    bool            // Painel está visível?
	fuzzyInput   textinput.Model // Campo de texto para digitação da busca
	fuzzyResults []Note          // Resultados filtrados
	fuzzyIndex   int             // Índice selecionado na lista de resultados
	allNotes     []Note          // Todas as notas (fonte para o filtro)

	// Mensagem na status bar
	statusMsg string
}

// ─────────────────────────────────────────────────────────────────────────────
// DADOS DE EXEMPLO — substituídos por queries SQLite na implementação real
// ─────────────────────────────────────────────────────────────────────────────

func sampleNotes() []Note {
	return []Note{
		{ID: "01HZ001", Title: "Arquitetura Hexagonal", Tags: []string{"arquitetura", "go"}, Path: "arquitetura-hexagonal.md"},
		{ID: "01HZ002", Title: "Domain-Driven Design", Tags: []string{"ddd", "arquitetura"}, Path: "ddd.md"},
		{ID: "01HZ003", Title: "Bubble Tea Patterns", Tags: []string{"go", "tui"}, Path: "bubbletea.md"},
		{ID: "01HZ004", Title: "SQLite Performance Tuning", Tags: []string{"database", "sqlite"}, Path: "sqlite-tuning.md"},
		{ID: "01HZ005", Title: "Go Concurrency Patterns", Tags: []string{"go", "concurrency"}, Path: "go-concurrency.md"},
		{ID: "01HZ006", Title: "Zettelkasten Method", Tags: []string{"notas", "produtividade"}, Path: "zettelkasten.md"},
		{ID: "01HZ007", Title: "Linux Internals — inotify", Tags: []string{"linux", "kernel"}, Path: "linux-inotify.md"},
		{ID: "01HZ008", Title: "YAML Frontmatter Spec", Tags: []string{"markdown", "yaml"}, Path: "frontmatter.md"},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// InitialModel — constrói o Model com estado inicial
// ─────────────────────────────────────────────────────────────────────────────

func InitialModel() Model {
	notes := sampleNotes()

	// Converte []Note para []list.Item (interface requerida pelo bubbles/list)
	items := make([]list.Item, len(notes))
	for i, n := range notes {
		items[i] = n
	}

	// Configura a lista principal com delegate customizado
	l := list.New(items, noteDelegate{}, 0, 0)
	l.Title = "notas"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false) // Usaremos nosso próprio fuzzy finder
	l.Styles.Title = styleHeader
	l.Styles.NoItems = lipgloss.NewStyle().Foreground(colorMuted)

	// Configura o campo de texto do fuzzy finder
	ti := textinput.New()
	ti.Placeholder = "Buscar notas..."
	ti.CharLimit = 100
	ti.Width = 50
	ti.Prompt = "  " // Espaço para o ícone de busca

	return Model{
		noteList:     l,
		fuzzyInput:   ti,
		fuzzyResults: notes,
		allNotes:     notes,
		statusMsg:    "Ctrl+P: buscar  ·  Ctrl+N: nova nota  ·  ?: ajuda",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Init — retorna o comando inicial (nenhum, no caso do boilerplate)
// Na implementação real: carregaria notas do SQLite aqui.
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Update — recebe mensagens e retorna o novo estado + comandos
// É o único lugar onde o estado muda. Sempre retorna um novo Model.
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Redimensionamento do terminal ─────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Ajusta a lista ao novo tamanho (reserva espaço para header e statusbar)
		m.noteList.SetSize(msg.Width-2, msg.Height-6)
		return m, nil

	// ── Entrada de teclado ────────────────────────────────────────────────
	case tea.KeyMsg:
		// Esc sempre fecha o painel flutuante (se aberto)
		if msg.String() == "esc" && m.fuzzyOpen {
			m.fuzzyOpen = false
			m.fuzzyInput.SetValue("")
			m.fuzzyResults = m.allNotes
			m.statusMsg = "Ctrl+P: buscar  ·  Ctrl+N: nova nota  ·  ?: ajuda"
			return m, nil
		}

		// Ctrl+C / q — sair da aplicação
		if msg.String() == "ctrl+c" || (!m.fuzzyOpen && msg.String() == "q") {
			return m, tea.Quit
		}

		// Ctrl+P — abre/fecha o fuzzy finder
		if msg.String() == "ctrl+p" {
			if m.fuzzyOpen {
				m.fuzzyOpen = false
				m.fuzzyInput.SetValue("")
			} else {
				m.fuzzyOpen = true
				m.fuzzyIndex = 0
				m.fuzzyResults = m.allNotes
				m.statusMsg = "↑↓: navegar  ·  Enter: abrir  ·  Esc: fechar"
				return m, m.fuzzyInput.Focus()
			}
			return m, nil
		}

		// Se o painel flutuante está aberto, redireciona inputs para ele
		if m.fuzzyOpen {
			switch msg.String() {
			case "up":
				if m.fuzzyIndex > 0 {
					m.fuzzyIndex--
				}
				return m, nil
			case "down":
				if m.fuzzyIndex < len(m.fuzzyResults)-1 {
					m.fuzzyIndex++
				}
				return m, nil
			case "enter":
				// Na implementação real: abre a nota selecionada
				if len(m.fuzzyResults) > 0 {
					selected := m.fuzzyResults[m.fuzzyIndex]
					m.fuzzyOpen = false
					m.fuzzyInput.SetValue("")
					m.statusMsg = fmt.Sprintf("Aberto: %s", selected.Title)
				}
				return m, nil
			default:
				// Delega para o textinput e refiltra
				var cmd tea.Cmd
				m.fuzzyInput, cmd = m.fuzzyInput.Update(msg)
				m.fuzzyResults = filterNotes(m.allNotes, m.fuzzyInput.Value())
				m.fuzzyIndex = 0
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
		}

	// ── Mensagens customizadas ────────────────────────────────────────────
	case openFuzzyMsg:
		m.fuzzyOpen = true
		return m, m.fuzzyInput.Focus()

	case closeFuzzyMsg:
		m.fuzzyOpen = false
		m.fuzzyInput.SetValue("")
	}

	// Propaga mensagens para a lista principal (quando o fuzzy não está aberto)
	if !m.fuzzyOpen {
		var cmd tea.Cmd
		m.noteList, cmd = m.noteList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// filterNotes — filtro fuzzy simples para o boilerplate
// Substituir por sahilm/fuzzy com ranking na Fase 4.
func filterNotes(notes []Note, query string) []Note {
	if query == "" {
		return notes
	}
	query = strings.ToLower(query)
	var results []Note
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n.Title), query) {
			results = append(results, n)
		}
	}
	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// View — renderiza o estado atual como string ANSI
// É chamada a cada mudança de estado. Deve ser pura (sem side effects).
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		// Terminal ainda não enviou WindowSizeMsg, renderiza placeholder
		return "Carregando notas...\n"
	}

	// ── 1. Cabeçalho ──────────────────────────────────────────────────────
	logo := styleLogo.Render("◆ notas")
	version := lipgloss.NewStyle().Foreground(colorMuted).Render("v0.1.0-dev")
	vaultInfo := lipgloss.NewStyle().Foreground(colorMuted).Render("~/notas/vault")
	noteCount := lipgloss.NewStyle().Foreground(colorTeal).Render(
		fmt.Sprintf("%d notas", len(m.allNotes)),
	)

	headerLeft := lipgloss.JoinHorizontal(lipgloss.Center,
		logo, "  ", version,
	)
	headerRight := lipgloss.JoinHorizontal(lipgloss.Center,
		vaultInfo, "  ", noteCount,
	)

	// Calcula o espaço entre logo e info do vault
	gap := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(headerRight) - 2
	if gap < 0 {
		gap = 0
	}

	header := headerLeft + strings.Repeat(" ", gap) + headerRight
	header = styleApp.Width(m.width).Render(header)

	// Separador visual
	sep := styleSep.Render(strings.Repeat("─", m.width))

	// ── 2. Lista principal ────────────────────────────────────────────────
	mainContent := m.noteList.View()

	// ── 3. Status bar ─────────────────────────────────────────────────────
	statusBar := styleStatusBar.Width(m.width).Render(m.statusMsg)

	// ── 4. Composição da view base ────────────────────────────────────────
	baseView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		sep,
		mainContent,
		statusBar,
	)

	// ── 5. Painel Flutuante (Fuzzy Finder) — sobrepõe a view base ─────────
	if !m.fuzzyOpen {
		return baseView
	}

	// Renderiza o conteúdo do painel
	floatContent := renderFuzzyPanel(m)

	// Posiciona o painel ao centro do terminal
	// Técnica: padding calculado para simular posicionamento
	panelWidth := lipgloss.Width(floatContent)
	panelHeight := lipgloss.Height(floatContent)

	// Offset horizontal para centralizar
	hOffset := (m.width - panelWidth) / 2
	if hOffset < 0 {
		hOffset = 0
	}

	// Offset vertical: 20% do topo
	vOffset := m.height / 5

	// Constrói a sobreposição linha a linha
	baseLines := strings.Split(baseView, "\n")
	floatLines := strings.Split(floatContent, "\n")

	// Injeta as linhas do painel sobre a view base
	for i, fl := range floatLines {
		targetLine := vOffset + i
		if targetLine >= len(baseLines) {
			break
		}

		baseLine := baseLines[targetLine]
		// Preenche a linha base se for menor que o offset
		baseLen := lipgloss.Width(baseLine)
		if baseLen < hOffset {
			baseLine += strings.Repeat(" ", hOffset-baseLen)
		}

		// Sobrepõe o painel na posição calculada
		// (simplificação — na Fase 4 usaremos overlay completo com runes)
		_ = fl
		_ = panelHeight
		baseLines[targetLine] = lipgloss.NewStyle().PaddingLeft(hOffset).Render("") + floatContent
		break // Renderiza o painel como bloco (simplificação do boilerplate)
	}

	// Injeta o painel de forma mais direta para o boilerplate
	// Na Fase 4: implementar overlay pixel-perfect com manipulação de runes
	topPad := strings.Repeat("\n", vOffset)
	leftPad := strings.Repeat(" ", hOffset)
	overlay := topPad + leftPad + strings.ReplaceAll(floatContent, "\n", "\n"+leftPad)

	// Combina view base com overlay do painel
	_ = overlay
	_ = baseLines

	// Abordagem final do boilerplate: renderiza painel abaixo do header
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		sep,
		lipgloss.NewStyle().PaddingLeft(hOffset).PaddingTop(1).Render(floatContent),
		statusBar,
	)
}

// renderFuzzyPanel — renderiza o conteúdo interno do painel flutuante
func renderFuzzyPanel(m Model) string {
	// Título do painel
	title := styleFloatTitle.Render("  Buscar Notas")

	// Campo de texto com ícone
	searchIcon := lipgloss.NewStyle().Foreground(colorPurple).Render("󰍉 ")
	inputLine := searchIcon + m.fuzzyInput.View()

	// Separador interno
	innerSep := styleSep.Render(strings.Repeat("─", 52))

	// Lista de resultados
	var resultLines []string
	maxResults := 8
	if len(m.fuzzyResults) < maxResults {
		maxResults = len(m.fuzzyResults)
	}

	for i := 0; i < maxResults; i++ {
		note := m.fuzzyResults[i]
		tags := ""
		for _, t := range note.Tags {
			tags += styleTag.Render("#"+t) + " "
		}

		if i == m.fuzzyIndex {
			line := styleSelected.Render(fmt.Sprintf(" ▸ %s", note.Title))
			resultLines = append(resultLines, line)
			if tags != "" {
				resultLines = append(resultLines, "     "+tags)
			}
		} else {
			line := styleResult.Render(fmt.Sprintf("   %s", note.Title))
			resultLines = append(resultLines, line)
		}
	}

	// Contador de resultados
	counter := lipgloss.NewStyle().
		Foreground(colorMuted).
		Render(fmt.Sprintf("  %d resultados", len(m.fuzzyResults)))

	if len(m.fuzzyResults) == 0 {
		resultLines = []string{
			lipgloss.NewStyle().Foreground(colorMuted).Render("   Nenhuma nota encontrada"),
		}
	}

	// Composição do painel
	panelBody := lipgloss.JoinVertical(lipgloss.Left,
		title,
		inputLine,
		innerSep,
		strings.Join(resultLines, "\n"),
		innerSep,
		counter,
	)

	return styleFloat.Render(panelBody)
}

// ─────────────────────────────────────────────────────────────────────────────
// MAIN — bootstrap do aplicativo
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	// Inicializa o modelo com estado inicial
	initialModel := InitialModel()

	// Cria o programa Bubble Tea
	// WithAltScreen: usa o buffer alternativo do terminal (sem sujar o histórico)
	// WithMouseCellMotion: habilita eventos de mouse para hover effects
	p := tea.NewProgram(
		initialModel,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Inicia o loop de eventos do Bubble Tea
	// O loop roda até que Update retorne tea.Quit
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao iniciar notas: %v\n", err)
		os.Exit(1)
	}

	// Após sair, exibe informações do estado final (útil para debug)
	_ = finalModel
	fmt.Println("Até mais! Suas notas estão salvas em ~/notas/vault/")
}
