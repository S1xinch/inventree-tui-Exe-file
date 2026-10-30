package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- Picker: filterable list overlay ----------

type PickerOpt struct {
	ID    int
	Label string
}

type Picker struct {
	title      string
	filter     textinput.Model
	options    []PickerOpt
	filtered   []int
	cursor     int
	scroll     int
	formField  int   // >= 0: result goes to this form field
	f          *flow // != nil: result continues this flow
	returnMode mode
	kind       string // "location" or "category": enables "+ new ..." from the filter text
	createText string // filter text offered as a new entry ("" = none)
}

// createIdx is the pseudo option index for the "+ new ..." row.
func (p *Picker) createIdx() int { return len(p.options) }

func (p *Picker) labelAt(idx int) string {
	if idx == p.createIdx() {
		noun := "bin"
		if p.kind == "category" {
			noun = "category"
		}
		return "+ new " + noun + ": " + p.createText
	}
	return p.options[idx].Label
}

func newPicker(title string, options []PickerOpt) *Picker {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Focus()
	p := &Picker{title: title, filter: ti, options: options, formField: -1}
	p.refilter()
	return p
}

func (p *Picker) refilter() {
	raw := strings.TrimSpace(p.filter.Value())
	q := strings.ToLower(raw)
	p.filtered = p.filtered[:0]
	exact := false
	for i, o := range p.options {
		if q == "" || strings.Contains(strings.ToLower(o.Label), q) {
			p.filtered = append(p.filtered, i)
		}
		if strings.EqualFold(o.Label, raw) {
			exact = true
		}
	}
	p.createText = ""
	if p.kind != "" && raw != "" && !exact {
		p.createText = raw
		p.filtered = append(p.filtered, p.createIdx())
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (a *App) locationOptions() []PickerOpt {
	opts := []PickerOpt{{ID: 0, Label: "(no location)"}}
	for _, l := range a.locations {
		opts = append(opts, PickerOpt{ID: l.PK, Label: l.PathString})
	}
	return opts
}

func (a *App) categoryOptions() []PickerOpt {
	opts := []PickerOpt{{ID: 0, Label: "(no category)"}}
	for _, c := range a.categories {
		opts = append(opts, PickerOpt{ID: c.PK, Label: c.PathString})
	}
	return opts
}

func (a *App) pickerOptionsFor(kind string) []PickerOpt {
	if kind == "category" {
		return a.categoryOptions()
	}
	return a.locationOptions()
}

func (a *App) openLocationPicker(f *flow, title string) {
	p := newPicker(title, a.locationOptions())
	p.kind = "location"
	p.f = f
	p.returnMode = modeDetail
	a.picker = p
	a.mode = modePicker
}

func (a *App) pickerListHeight() int { return max(1, a.height-3) }

func (a *App) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := a.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			a.mode = p.returnMode
			return a, nil
		case "up":
			p.move(-1, a)
			return a, nil
		case "down":
			p.move(1, a)
			return a, nil
		case "pgup":
			p.move(-a.pickerListHeight(), a)
			return a, nil
		case "pgdown":
			p.move(a.pickerListHeight(), a)
			return a, nil
		case "enter":
			return a.pickerChoose(p.cursor)
		}
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		p.refilter()
		p.cursor, p.scroll = 0, 0
		return a, cmd

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				p.move(-scrollStep(msg), a)
			case tea.MouseButtonWheelDown:
				p.move(scrollStep(msg), a)
			case tea.MouseButtonLeft:
				row := msg.Y - 2
				if row >= 0 && row < a.pickerListHeight() {
					idx := p.scroll + row
					if idx < len(p.filtered) {
						if idx == p.cursor {
							return a.pickerChoose(idx)
						}
						p.cursor = idx
					}
				}
			}
		}
		return a, nil
	}
	return a, nil
}

func (a *App) pickerChoose(idx int) (tea.Model, tea.Cmd) {
	p := a.picker
	if idx < 0 || idx >= len(p.filtered) {
		return a, nil
	}
	var opt PickerOpt
	if p.filtered[idx] == p.createIdx() {
		var err error
		if opt, err = a.createPickerEntry(p.kind, p.createText); err != nil {
			a.setStatus(err.Error(), true)
			return a, nil
		}
	} else {
		opt = p.options[p.filtered[idx]]
	}
	a.mode = p.returnMode
	if p.f != nil {
		return a.runMutation(a.flowLocationPicked(p.f, opt.ID))
	}
	if p.formField >= 0 && a.form != nil {
		fld := &a.form.fields[p.formField]
		fld.id = opt.ID
		fld.display = opt.Label
	}
	return a, nil
}

func (p *Picker) move(delta int, a *App) {
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	h := a.pickerListHeight()
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+h {
		p.scroll = p.cursor - h + 1
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

func (p *Picker) view(a *App) string {
	var b strings.Builder
	b.WriteString(styTitle.Render(truncate(p.title, a.width)) + "\n")
	p.filter.Width = max(10, a.width-6)
	b.WriteString(truncate(p.filter.View(), a.width) + "\n")
	h := a.pickerListHeight()
	for row := 0; row < h; row++ {
		idx := p.scroll + row
		if idx >= len(p.filtered) {
			b.WriteString("\n")
			continue
		}
		line := truncate(p.labelAt(p.filtered[idx]), a.width-2)
		if idx == p.cursor {
			b.WriteString(stySel.Render(padRight(line, a.width-2)) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	hint := "enter:choose  esc:cancel"
	if p.kind != "" {
		hint = "type a new name to create it  " + hint
	}
	b.WriteString(a.bottomBar(hint))
	return b.String()
}

// createPickerEntry makes a new bin or category (paths like "Garage/Shelf 2"
// create parents too) and adds it to the app's in-memory lists.
func (a *App) createPickerEntry(kind, name string) (PickerOpt, error) {
	if kind == "category" {
		cat, err := a.client.CreateCategory(name)
		if err != nil {
			return PickerOpt{}, err
		}
		a.categories, _ = a.client.FetchCategories()
		a.setStatus("new category: "+cat.PathString, false)
		return PickerOpt{ID: cat.PK, Label: cat.PathString}, nil
	}
	loc, err := a.client.CreateLocation(name)
	if err != nil {
		return PickerOpt{}, err
	}
	a.locations, _ = a.client.FetchLocations()
	a.setStatus("new bin: "+loc.PathString, false)
	return PickerOpt{ID: loc.PK, Label: loc.PathString}, nil
}
