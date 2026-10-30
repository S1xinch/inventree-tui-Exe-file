package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testApp() *App {
	a := NewApp(NewClient(filepath.Join(os.TempDir(), "inv-tui-test.json")), 0)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	loc := Location{PK: 64, Name: "Bin 57", PathString: "Workshop/Bin 57"}
	a.Update(dataLoadedMsg{
		parts: []Part{
			{PK: 1, Name: "M3 Screw", IPN: "FAST-M3", Description: "machine screw", CategoryName: "Fasteners", TotalInStock: 100, Active: true, Category: 2},
			{PK: 2, Name: "Mono Jack Socket", IPN: "ELEC-JACK", Description: "guitar jack", CategoryName: "Electronics", TotalInStock: 9, Active: true, Category: 1},
		},
		stock: []StockItem{
			{PK: 10, Part: 2, Quantity: 9, LocationDetail: &loc, StatusText: "OK"},
		},
		locations:  []Location{loc},
		categories: []Category{{PK: 1, Name: "Electronics", PathString: "Electronics"}},
	})
	return a
}

func key(a *App, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	a.Update(msg)
}

func TestSearchAndDetail(t *testing.T) {
	a := testApp()
	v := a.View()
	if !strings.Contains(v, "M3 Screw") || !strings.Contains(v, "Mono Jack Socket") {
		t.Fatalf("expected both parts listed:\n%s", v)
	}
	if !strings.Contains(v, "Bin 57") {
		t.Errorf("expected bin path inline in results:\n%s", v)
	}

	// type a query that should only match the jack (via description)
	key(a, "guitar")
	v = a.View()
	if strings.Contains(v, "M3 Screw") || !strings.Contains(v, "Mono Jack Socket") {
		t.Fatalf("search 'guitar' should match only the jack:\n%s", v)
	}

	// open detail
	key(a, "enter")
	if a.mode != modeDetail {
		t.Fatalf("expected detail mode, got %v", a.mode)
	}
	v = a.View()
	for _, want := range []string{"Mono Jack Socket", "Bin 57", "Electronics", "9"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail view missing %q:\n%s", want, v)
		}
	}

	// back to search, esc clears query
	key(a, "esc")
	if a.mode != modeSearch {
		t.Fatalf("expected search mode")
	}
	key(a, "esc")
	if got := a.search.input.Value(); got != "" {
		t.Errorf("esc should clear query, got %q", got)
	}
}

func TestMouseTapSelectsAndOpens(t *testing.T) {
	a := testApp()
	// direct mouse: first click selects row, second click on same row opens it
	tap := tea.MouseMsg{X: 5, Y: searchListTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	a.Update(tap)
	if a.search.cursor != 1 {
		t.Fatalf("expected cursor 1 after first click, got %d", a.search.cursor)
	}
	a.Update(tap)
	if a.mode != modeDetail {
		t.Fatalf("expected detail mode after second click on same row")
	}
	if a.detail.partPK != 2 {
		t.Fatalf("expected part 2 opened, got %d", a.detail.partPK)
	}
	// tap title row goes back
	a.View()
	a.Update(tea.MouseMsg{X: 1, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.mode != modeSearch {
		t.Fatalf("expected back to search after tapping title")
	}
}

func TestStockFlowPrompt(t *testing.T) {
	a := testApp()
	key(a, "guitar")
	key(a, "enter") // detail of jack
	key(a, "a")     // add stock -> prompt
	if a.mode != modePrompt {
		t.Fatalf("expected prompt mode, got %v", a.mode)
	}
	if !strings.Contains(a.View(), "Bin 57") {
		t.Errorf("prompt should name the bin:\n%s", a.View())
	}
	key(a, "esc")
	if a.mode != modeDetail {
		t.Fatalf("esc should return to detail")
	}
}

func TestNewPartForm(t *testing.T) {
	a := testApp()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if a.mode != modeForm {
		t.Fatalf("expected form mode")
	}
	v := a.View()
	for _, want := range []string{"New part", "Name", "Category", "Initial qty", "Bin"} {
		if !strings.Contains(v, want) {
			t.Errorf("form missing %q:\n%s", want, v)
		}
	}
	key(a, "Widget")
	if got := a.form.fields[0].input.Value(); got != "Widget" {
		t.Errorf("typing should fill name, got %q", got)
	}
	key(a, "esc")
	if a.mode != modeSearch {
		t.Fatalf("esc should cancel form")
	}
}

// testStore opens an empty inventory file in a temp dir.
func testStore(t *testing.T) *Client {
	t.Helper()
	c := NewClient(filepath.Join(t.TempDir(), "inventory.json"))
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"Workshop/Bin 1", "Workshop/Bin 2"} {
		if _, err := c.CreateLocation(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CreateCategory("Electronics"); err != nil {
		t.Fatal(err)
	}
	return c
}
