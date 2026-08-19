package markdown

import "testing"

func TestDeriveTitle(t *testing.T) {
	cases := map[string]string{
		"# Minha ideia\n\ncorpo": "Minha ideia",
		"\n\n  primeira linha":   "primeira linha",
		"":                       "",
		"   \n  ":                "",
	}
	for body, want := range cases {
		if got := deriveTitle(body); got != want {
			t.Errorf("deriveTitle(%q) = %q, quer %q", body, got, want)
		}
	}
}
