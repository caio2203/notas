package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/seuusuario/notas/internal/model"
)

func items(ns ...*model.Note) []noteItem {
	out := make([]noteItem, len(ns))
	for i, n := range ns {
		out[i] = noteItem{n}
	}
	return out
}

func TestFilterNotes(t *testing.T) {
	a := &model.Note{Title: "Go concorrência", Body: "goroutines e channels", Tags: []string{"go"}}
	b := &model.Note{Title: "Receita bolo", Body: "farinha e ovos", Tags: []string{"cozinha"}}
	all := items(a, b)

	if got := filterNotes(all, "channels"); len(got) != 1 || got[0].n != a {
		t.Errorf("full-text no corpo falhou: %v", got)
	}
	if got := filterNotes(all, "#cozinha"); len(got) != 1 || got[0].n != b {
		t.Errorf("filtro por tag falhou: %v", got)
	}
	if got := filterNotes(all, ""); len(got) != 2 {
		t.Errorf("query vazia devia retornar tudo, veio %d", len(got))
	}
}

func TestPreviewAndFollowLink(t *testing.T) {
	m := InitialModel()
	step := func(msg tea.Msg) { mm, _ := m.Update(msg); m = mm.(Model) }

	step(tea.WindowSizeMsg{Width: 80, Height: 24})
	a := &model.Note{ID: "a", Slug: "nota-a", Title: "Nota A", Body: "veja [[Nota B]]", Links: []string{"Nota B"}, Path: "/tmp/a.md"}
	b := &model.Note{ID: "b", Slug: "nota-b", Title: "Nota B", Body: "conteudo B", Path: "/tmp/b.md"}
	step(indexDoneMsg{notes: []*model.Note{a, b}})

	// Espaço abre o preview da nota A (selecionada)
	step(tea.KeyMsg{Type: tea.KeySpace})
	if !m.previewOpen || m.previewNote != a {
		t.Fatalf("Espaço deveria abrir o preview de A; open=%v", m.previewOpen)
	}
	if len(m.previewLinks) != 1 || m.previewLinks[0] != b {
		t.Fatalf("A deveria ter 1 wikilink resolvido (B); got %v", m.previewLinks)
	}
	if m.View() == "" {
		t.Fatal("View não deveria ser vazio no preview")
	}

	// "1" segue o wikilink para B, empilhando A no histórico
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.previewNote != b {
		t.Fatalf("seguir link deveria ir para B; previewNote=%q", m.previewNote.Title)
	}
	if len(m.previewHistory) != 1 || m.previewHistory[0] != a {
		t.Fatal("histórico deveria conter A após seguir o link")
	}

	// Backspace volta para A
	step(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.previewNote != a || len(m.previewHistory) != 0 {
		t.Fatalf("Backspace deveria voltar para A; previewNote=%q", m.previewNote.Title)
	}

	// Enter edita (fecha preview + retorna comando)
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.previewOpen || cmd == nil {
		t.Error("Enter no preview deveria editar: fechar painel e retornar comando")
	}
}

func TestBacklinksFor(t *testing.T) {
	alvo := &model.Note{ID: "1", Slug: "alvo", Title: "Alvo"}
	linka := &model.Note{ID: "2", Title: "Origem", Links: []string{"Alvo"}}
	naoLinka := &model.Note{ID: "3", Title: "Outra", Links: []string{"Sei la"}}

	got := backlinksFor(items(alvo, linka, naoLinka), alvo)
	if len(got) != 1 || got[0].n != linka {
		t.Errorf("backlinksFor devia achar só 'Origem', veio %v", got)
	}
}
