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
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.filtered = p.filtered[:0]
	for i, o := range p.options {
		if q == "" || strings.Contains(strings.ToLower(o.Label), q) {
			p.filtered = append(p.filtered, i)
		}
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
	var opts []PickerOpt
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
	opt := p.options[p.filtered[idx]]
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
		o := p.options[p.filtered[idx]]
		line := truncate(o.Label, a.width-2)
		if idx == p.cursor {
			b.WriteString(stySel.Render(padRight(line, a.width-2)) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(a.bottomBar("enter:choose  esc:cancel"))
	return b.String()
}
