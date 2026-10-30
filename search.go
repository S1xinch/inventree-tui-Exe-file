package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type SearchView struct {
	input   textinput.Model
	results []int // indices into app.parts
	cursor  int   // index into results
	scroll  int
}

func NewSearchView() SearchView {
	ti := textinput.New()
	ti.Placeholder = "type to search parts..."
	ti.Prompt = "/ "
	ti.Focus()
	ti.CharLimit = 100
	return SearchView{input: ti}
}

// searchDoc is a precomputed, lowercased view of one part, rebuilt only when
// data changes so per-keystroke filtering stays cheap on the Pi 1.
type searchDoc struct {
	name, ipn, rest string
	cat             string   // lowercased category path
	locNames        []string // lowercased stock location names ("bin 57")
	locPaths        []string // lowercased stock location paths
	low             bool     // below its minimum stock (is:low)
	nameWords       [][]rune // for fuzzy matching
	allWords        [][]rune
}

func splitWords(s string) [][]rune {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r > 127)
	})
	words := make([][]rune, 0, len(fields))
	for _, f := range fields {
		words = append(words, []rune(f))
	}
	return words
}

func (a *App) rebuildDocs() {
	a.docs = make([]searchDoc, len(a.parts))
	for i, p := range a.parts {
		d := &a.docs[i]
		d.name = strings.ToLower(p.Name)
		d.ipn = strings.ToLower(p.IPN)
		d.cat = strings.ToLower(p.CategoryName)
		d.low = isLow(p)
		d.locNames, d.locPaths = nil, nil
		for _, s := range a.stockByPart[p.PK] {
			if s.LocationDetail != nil {
				d.locNames = append(d.locNames, strings.ToLower(s.LocationDetail.Name))
				d.locPaths = append(d.locPaths, strings.ToLower(s.LocationDetail.PathString))
			}
		}
		d.rest = strings.ToLower(p.Description + " " + p.Keywords + " " +
			p.CategoryName + " " + a.binSummary(p.PK))
		d.nameWords = splitWords(d.name)
		d.allWords = append(splitWords(d.ipn+" "+d.rest), d.nameWords...)
	}
	a.docsDirty = false
}

// matchesFilter evaluates a "key:value" search filter against a part.
// bin:57 matches the exact bin number; bin:closet matches any bin whose path
// contains "closet"; cat:tools matches the category.
func (d *searchDoc) matchesFilter(key, val string) bool {
	switch key {
	case "bin", "loc":
		return matchBinFilter(d.locNames, d.locPaths, val)
	case "cat":
		return strings.Contains(d.cat, val)
	case "is":
		return val == "low" && d.low
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isFilterKey(k string) bool { return k == "bin" || k == "loc" || k == "cat" || k == "is" }

// refilter recomputes results from the query. All whitespace-separated terms
// must match somewhere in the part (name, IPN, description, keywords,
// category, or bin path). Results are ranked: name prefix, name, IPN, rest,
// then typo-tolerant (edit distance) matches last.
func (sv *SearchView) refilter(a *App) {
	if a.docsDirty || len(a.docs) != len(a.parts) {
		a.rebuildDocs()
	}
	query := strings.ToLower(strings.TrimSpace(sv.input.Value()))

	// split into key:value filters (bin:57, cat:tools) and plain text terms
	var terms []string
	type kv struct{ key, val string }
	var filters []kv
	for _, t := range strings.Fields(query) {
		if k, v, ok := strings.Cut(t, ":"); ok && v != "" && isFilterKey(k) {
			filters = append(filters, kv{k, v})
		} else {
			terms = append(terms, t)
		}
	}

	type scored struct{ idx, score int }
	var hits []scored
	for i := range a.parts {
		doc := &a.docs[i]
		filtered := false
		for _, f := range filters {
			if !doc.matchesFilter(f.key, f.val) {
				filtered = true
				break
			}
		}
		if filtered {
			continue
		}
		if len(terms) == 0 {
			hits = append(hits, scored{i, 1})
			continue
		}
		score := 0
		ok := true
		for _, t := range terms {
			switch {
			case strings.HasPrefix(doc.name, t):
				// score 0: best
			case strings.Contains(doc.name, t):
				score = max(score, 1)
			case strings.Contains(doc.ipn, t):
				score = max(score, 2)
			case strings.Contains(doc.rest, t):
				score = max(score, 3)
			case fuzzyMatch(t, doc.nameWords):
				score = max(score, 4)
			case fuzzyMatch(t, doc.allWords):
				score = max(score, 5)
			default:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok {
			hits = append(hits, scored{i, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	sv.results = sv.results[:0]
	for _, h := range hits {
		sv.results = append(sv.results, h.idx)
	}
	if sv.cursor >= len(sv.results) {
		sv.cursor = len(sv.results) - 1
	}
	if sv.cursor < 0 {
		sv.cursor = 0
	}
}

func (sv *SearchView) selectedPart(a *App) *Part {
	if sv.cursor < 0 || sv.cursor >= len(sv.results) {
		return nil
	}
	return &a.parts[sv.results[sv.cursor]]
}

func (a *App) searchListHeight() int {
	return max(1, a.height-3) // input, column header, bottom bar
}

const searchListTop = 2 // rows above the first result row

func (a *App) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	sv := &a.search
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			sv.move(-1, a)
			a.buzzer.Play("key")
		case "down":
			sv.move(1, a)
			a.buzzer.Play("key")
		case "pgup":
			sv.move(-a.searchListHeight(), a)
			a.buzzer.Play("key")
		case "pgdown":
			sv.move(a.searchListHeight(), a)
			a.buzzer.Play("key")
		case "home":
			sv.cursor = 0
			sv.clampScroll(a)
			a.buzzer.Play("key")
		case "enter", "right":
			if p := sv.selectedPart(a); p != nil {
				a.openDetail(p.PK)
			}
		case "esc":
			if sv.input.Value() != "" {
				sv.input.SetValue("")
				sv.refilter(a)
			}
		case "ctrl+n":
			a.openPartForm(0)
		case "ctrl+h":
			if a.hermes != nil {
				a.openHermes(strings.TrimSpace(sv.input.Value()))
			}
		case "ctrl+r", "f5":
			a.loading = true
			a.setStatus("refreshing...", false)
			return a, a.loadAllCmd()
		case "ctrl+z":
			a.undoLast()
		case "ctrl+y":
			a.redoLast()
		case "ctrl+q":
			return a, tea.Quit
		default:
			var cmd tea.Cmd
			sv.input, cmd = sv.input.Update(msg)
			sv.refilter(a)
			sv.cursor = 0
			sv.scroll = 0
			return a, cmd
		}
		return a, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				sv.move(-scrollStep(msg), a)
				a.buzzer.Play("key")
			case tea.MouseButtonWheelDown:
				sv.move(scrollStep(msg), a)
				a.buzzer.Play("key")
			case tea.MouseButtonLeft:
				row := msg.Y - searchListTop
				if row >= 0 && row < a.searchListHeight() {
					idx := sv.scroll + row
					if idx < len(sv.results) {
						if idx == sv.cursor {
							if p := sv.selectedPart(a); p != nil {
								a.openDetail(p.PK)
							}
						} else {
							sv.cursor = idx // silent — "tap" only plays on open
						}
					}
				}
			}
		}
		return a, nil
	}

	var cmd tea.Cmd
	sv.input, cmd = sv.input.Update(msg)
	return a, cmd
}

func (sv *SearchView) move(delta int, a *App) {
	sv.cursor += delta
	if sv.cursor < 0 {
		sv.cursor = 0
	}
	if sv.cursor >= len(sv.results) {
		sv.cursor = len(sv.results) - 1
	}
	sv.clampScroll(a)
}

func (sv *SearchView) clampScroll(a *App) {
	h := a.searchListHeight()
	if sv.cursor < sv.scroll {
		sv.scroll = sv.cursor
	}
	if sv.cursor >= sv.scroll+h {
		sv.scroll = sv.cursor - h + 1
	}
	if sv.scroll < 0 {
		sv.scroll = 0
	}
}

func (a *App) viewSearch() string {
	sv := &a.search
	var b strings.Builder

	// row 0: search input + count
	count := fmt.Sprintf("%d/%d", len(sv.results), len(a.parts))
	if a.loading {
		count += " ⟳"
	}
	lowTag := ""
	if n := len(a.lowParts()); n > 0 {
		lowTag = fmt.Sprintf("%d low", n)
		count = lowTag + "  " + count
	}
	inputW := a.width - len(count) - 2
	sv.input.Width = max(10, inputW-4)
	line := sv.input.View()
	b.WriteString(padRight(truncate(line, inputW), inputW))
	if lowTag != "" {
		b.WriteString("  " + styErr.Render(lowTag) + styDim.Render(strings.TrimPrefix(count, lowTag)) + "\n")
	} else {
		b.WriteString("  " + styDim.Render(count) + "\n")
	}

	// column widths: qty fixed, bin ~35%, name the rest
	qtyW := 6
	binW := max(14, a.width*35/100)
	nameW := a.width - qtyW - binW - 4
	b.WriteString(styHeader.Render(
		padRight("Part", nameW)+"  "+padRight("Qty", qtyW)+"  "+padRight("Bin", binW)) + "\n")

	h := a.searchListHeight()
	for row := 0; row < h; row++ {
		idx := sv.scroll + row
		if idx >= len(sv.results) {
			b.WriteString("\n")
			continue
		}
		p := a.parts[sv.results[idx]]
		name := p.Name
		if p.IPN != "" {
			name += "  " + p.IPN
		}
		if !p.Active {
			name += " (inactive)"
		}
		qty := fmtQty(p.TotalInStock)
		bins := a.binSummary(p.PK)

		if idx == sv.cursor {
			full := padRight(name, nameW) + "  " + padRight(qty, qtyW) + "  " + padRight(bins, binW)
			b.WriteString(stySel.Render(full) + "\n")
		} else {
			qs := padRight(qty, qtyW)
			// same rules as the windowed app: only items with a minimum are judged
			if p.MinimumStock > 0 && p.TotalInStock <= 0 {
				qs = styOutStock.Render(qs)
			} else if isLow(p) {
				qs = styLowStock.Render(qs)
			}
			b.WriteString(padRight(name, nameW) + "  " + qs + "  " + styBin.Render(padRight(bins, binW)) + "\n")
		}
	}

	hints := "enter:open  ctrl+n:new  ctrl+z/y:undo/redo  F1:help  ctrl+q:quit"
	if a.hermes != nil {
		hints = "enter:open  ctrl+h:hermes  ctrl+n:new  ctrl+z/y:undo/redo  F1:help  ctrl+q:quit"
	}
	b.WriteString(a.bottomBar(hints))
	return b.String()
}
