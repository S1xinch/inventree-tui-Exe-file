package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- Form: create / edit part ----------

type formField struct {
	label   string
	input   textinput.Model
	picker  string // "" = text field; otherwise "category" or "location"
	id      int    // selected pk for picker fields
	display string
	numeric bool
}

type Form struct {
	title  string
	fields []formField
	cursor int
	editPK int // 0 = creating a new part
}

func textField(label, value string, numeric bool) formField {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 250
	ti.SetValue(value)
	return formField{label: label, input: ti, numeric: numeric}
}

func (a *App) openPartForm(editPK int) {
	f := &Form{editPK: editPK}
	var p Part
	if editPK != 0 {
		if existing := a.partByPK(editPK); existing != nil {
			p = *existing
		}
		f.title = "Edit part: " + p.Name
	} else {
		f.title = "New part"
	}
	f.fields = []formField{
		textField("Name", p.Name, false),
		textField("IPN", p.IPN, false),
		textField("Description", p.Description, false),
		textField("Keywords", p.Keywords, false),
		textField("Units", p.Units, false),
		textField("Min stock", fmtQty(p.MinimumStock), true),
		{label: "Category", picker: "category", id: p.Category, display: p.CategoryName},
	}
	if editPK == 0 {
		if len(a.categories) > 0 {
			f.fields[6].id = a.categories[0].PK
			f.fields[6].display = a.categories[0].PathString
		}
		f.fields = append(f.fields,
			textField("Initial qty", "", true),
			formField{label: "Bin", picker: "location", id: 0, display: "(no location)"},
		)
	}
	f.fields[0].input.Focus()
	a.form = f
	a.mode = modeForm
	a.setStatus("", false)
}

func (a *App) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	f := a.form
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			a.mode = modeSearch
			if f.editPK != 0 {
				a.mode = modeDetail
			}
			return a, nil
		case "up", "shift+tab":
			f.focusField(f.cursor - 1)
			return a, nil
		case "down", "tab":
			f.focusField(f.cursor + 1)
			return a, nil
		case "ctrl+s":
			return a.submitForm()
		case "enter":
			fld := &f.fields[f.cursor]
			if fld.picker != "" {
				p := newPicker("Choose "+strings.ToLower(fld.label), a.pickerOptionsFor(fld.picker))
				p.formField = f.cursor
				p.returnMode = modeForm
				a.picker = p
				a.mode = modePicker
				return a, nil
			}
			if f.cursor == len(f.fields)-1 {
				return a.submitForm()
			}
			f.focusField(f.cursor + 1)
			return a, nil
		}
		fld := &f.fields[f.cursor]
		if fld.picker == "" {
			var cmd tea.Cmd
			fld.input, cmd = fld.input.Update(msg)
			return a, cmd
		}
		return a, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			row := msg.Y - 2
			if row >= 0 && row < len(f.fields) {
				f.focusField(row)
			}
		}
		return a, nil
	}
	return a, nil
}

func (f *Form) focusField(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(f.fields) {
		idx = len(f.fields) - 1
	}
	for i := range f.fields {
		f.fields[i].input.Blur()
	}
	f.cursor = idx
	if f.fields[idx].picker == "" {
		f.fields[idx].input.Focus()
	}
}

func (f *Form) value(label string) string {
	for i := range f.fields {
		if f.fields[i].label == label {
			return strings.TrimSpace(f.fields[i].input.Value())
		}
	}
	return ""
}

func (f *Form) pickerID(label string) int {
	for i := range f.fields {
		if f.fields[i].label == label {
			return f.fields[i].id
		}
	}
	return 0
}

func (a *App) submitForm() (tea.Model, tea.Cmd) {
	f := a.form
	name := f.value("Name")
	if name == "" {
		a.setStatus("name is required", true)
		return a, nil
	}
	minStock := 0.0
	if v := f.value("Min stock"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			minStock = x
		}
	}
	fields := map[string]any{
		"name":          name,
		"IPN":           f.value("IPN"),
		"description":   f.value("Description"),
		"keywords":      f.value("Keywords"),
		"units":         f.value("Units"),
		"minimum_stock": minStock,
		"category":      f.pickerID("Category"),
	}
	c := a.client

	if f.editPK != 0 {
		pk := f.editPK
		a.mode = modeDetail
		e := &JEntry{Action: "part.update", PartPK: pk,
			Summary: fmt.Sprintf("edit part %q", a.partLabel(pk))}
		return a.runMutation(a.mutateCmd(e, "part saved", func() error {
			_, err := c.UpdatePart(pk, fields)
			return err
		}))
	}

	qty := 0.0
	if v := f.value("Initial qty"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			qty = x
		}
	}
	loc := f.pickerID("Bin")
	a.mode = modeSearch
	a.setStatus("creating part...", false)
	j := a.journal
	return a.runMutation(func() tea.Msg {
		p, err := c.CreatePart(fields)
		if err != nil {
			return statusMsg{text: err.Error(), isErr: true}
		}
		e := &JEntry{Action: "part.create", NewPartPK: p.PK, Qty: qty, ToLoc: loc,
			Summary: fmt.Sprintf("create part %q", name)}
		if qty > 0 {
			if s, serr := c.CreateStock(p.PK, qty, loc); serr != nil {
				j.Append(e)
				return statusMsg{text: "part created but stock failed: " + serr.Error(), isErr: true}
			} else {
				e.NewStockPK = s.PK
			}
		}
		j.Append(e)
		msg := a.refreshPartCmd(p.PK, "part created")().(partRefreshedMsg)
		msg.goDetail = true
		return msg
	})
}

func (f *Form) view(a *App) string {
	var b strings.Builder
	b.WriteString(styTitle.Render(truncate(f.title, a.width)) + "\n\n")
	labelW := 13
	for i := range f.fields {
		fld := &f.fields[i]
		label := padRight(fld.label, labelW)
		var val string
		if fld.picker != "" {
			val = "▸ " + fld.display
		} else {
			fld.input.Width = max(10, a.width-labelW-4)
			val = fld.input.View()
		}
		if i == f.cursor {
			b.WriteString(stySel.Render(padRight(truncate(label, labelW), labelW)) + truncate(val, a.width-labelW-1) + "\n")
		} else {
			b.WriteString(styDim.Render(label) + truncate(val, a.width-labelW-1) + "\n")
		}
	}
	b.WriteString("\n" + styDim.Render("tab/↑↓:fields  enter:next/pick  ctrl+s:save  esc:cancel") + "\n")
	b.WriteString(a.statusBar())
	return b.String()
}
