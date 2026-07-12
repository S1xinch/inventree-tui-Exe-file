package main

import (
	"strings"
	"testing"
)

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b  string
		bound int
		want  int
	}{
		{"socket", "sockt", 2, 1},   // deletion
		{"resistr", "resist", 2, 1}, // insertion
		{"jakc", "jack", 2, 1},      // transposition
		{"m3", "m4", 2, 1},          // substitution
		{"abc", "xyz", 1, 2},        // over bound -> bound+1
		{"same", "same", 2, 0},
	}
	for _, c := range cases {
		got := editDistance([]rune(c.a), []rune(c.b), c.bound)
		if got != c.want {
			t.Errorf("editDistance(%q,%q,%d) = %d, want %d", c.a, c.b, c.bound, got, c.want)
		}
	}
}

func searchFor(t *testing.T, a *App, query string) string {
	t.Helper()
	a.search.input.SetValue(query)
	a.search.refilter(a)
	var names []string
	for _, idx := range a.search.results {
		names = append(names, a.parts[idx].Name)
	}
	return strings.Join(names, "|")
}

func TestFuzzySearch(t *testing.T) {
	a := testApp()

	// typo in a name word
	if got := searchFor(t, a, "sockt"); !strings.Contains(got, "Mono Jack Socket") {
		t.Errorf("'sockt' should fuzzy-match Socket, got %q", got)
	}
	// transposition
	if got := searchFor(t, a, "jakc"); !strings.Contains(got, "Mono Jack Socket") {
		t.Errorf("'jakc' should fuzzy-match Jack, got %q", got)
	}
	// typo in a description word
	if got := searchFor(t, a, "guitr"); !strings.Contains(got, "Mono Jack Socket") {
		t.Errorf("'guitr' should fuzzy-match description 'guitar', got %q", got)
	}
	// short terms stay exact: m4 must NOT match the M3 screw
	if got := searchFor(t, a, "m4"); got != "" {
		t.Errorf("'m4' should match nothing (short terms exact), got %q", got)
	}
	// exact matches rank above fuzzy ones: "screw" exact on M3 Screw;
	// a part whose only match is fuzzy should come after
	if got := searchFor(t, a, "scre"); !strings.HasPrefix(got, "M3 Screw") {
		t.Errorf("exact prefix should rank first, got %q", got)
	}
}

func TestSearchFilters(t *testing.T) {
	a := testApp() // jack is in Workshop/Bin 57 (Electronics); screw has no stock

	if got := searchFor(t, a, "bin:57"); got != "Mono Jack Socket" {
		t.Errorf("bin:57 should match only the jack, got %q", got)
	}
	if got := searchFor(t, a, "bin:5"); got != "" {
		t.Errorf("bin numbers match exactly; bin:5 should be empty, got %q", got)
	}
	if got := searchFor(t, a, "bin:workshop"); got != "Mono Jack Socket" {
		t.Errorf("non-numeric bin: matches path substring (raw path still has Workshop/), got %q", got)
	}
	if got := searchFor(t, a, "jack bin:57"); got != "Mono Jack Socket" {
		t.Errorf("filter combined with text term failed, got %q", got)
	}
	if got := searchFor(t, a, "bin:57 m3"); got != "" {
		t.Errorf("text term must still match within the bin, got %q", got)
	}
	if got := searchFor(t, a, "cat:electronics"); got != "Mono Jack Socket" {
		t.Errorf("cat: filter failed, got %q", got)
	}
	if got := searchFor(t, a, "cat:fasteners"); got != "M3 Screw" {
		t.Errorf("cat: filter failed, got %q", got)
	}
}

func TestDocsRebuildAfterDataChange(t *testing.T) {
	a := testApp()
	searchFor(t, a, "") // force initial doc build
	a.upsertPart(Part{PK: 3, Name: "Banana Connector", Active: true}, nil)
	if got := searchFor(t, a, "banana"); !strings.Contains(got, "Banana Connector") {
		t.Errorf("new part should be searchable after upsert, got %q", got)
	}
	a.removePart(3)
	if got := searchFor(t, a, "banana"); got != "" {
		t.Errorf("removed part should disappear from search, got %q", got)
	}
}
