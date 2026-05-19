package model

import "time"

type Note struct {
	ID         string
	Slug       string
	Title      string
	Body       string
	Tags       []string
	Links      []string
	Path       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Properties map[string]string
}

type Link struct {
	SourceID string
	TargetID string
	Alias    string
}
