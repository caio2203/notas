package main

import (
	"testing"

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

func TestBacklinksFor(t *testing.T) {
	alvo := &model.Note{ID: "1", Slug: "alvo", Title: "Alvo"}
	linka := &model.Note{ID: "2", Title: "Origem", Links: []string{"Alvo"}}
	naoLinka := &model.Note{ID: "3", Title: "Outra", Links: []string{"Sei la"}}

	got := backlinksFor(items(alvo, linka, naoLinka), alvo)
	if len(got) != 1 || got[0].n != linka {
		t.Errorf("backlinksFor devia achar só 'Origem', veio %v", got)
	}
}
