package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type DetailView struct {
	partPK    int
	cursor    int // index into the part's stock items
	headerEnd int // last header row (computed during render, for mouse hits)
}

func (a *App) openDetail(pk int) {
	a.detail = DetailView{partPK: pk}
	a.mode = modeDetail
	a.setStatus("", false)
	a.buzzer.Play("tap")
}

func (dv *DetailView) clampCursor(a *App) {
	n := len(a.stockByPart[dv.partPK])
	if dv.cursor >= n {
		dv.cursor = n - 1
	}
	if dv.cursor < 0 {
		dv.cursor = 0
	}
}

func (dv *DetailView) selectedStock(a *App) *StockItem {
	items := a.stockByPart[dv.partPK]
	if dv.cursor < 0 || dv.cursor >= len(items) {
		return nil
	}
	return &items[dv.cursor]
}

func (a *App) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	dv := &a.detail
	part := a.partByPK(dv.partPK)
	if part == nil {
		a.mode = modeSearch
		return a, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "backspace", "left":
			a.mode = modeSearch
			a.setStatus("", false)
		case "up", "k":
			dv.cursor--
			dv.clampCursor(a)
		case "down", "j":
			dv.cursor++
			dv.clampCursor(a)
		case "a": // add quantity to selected stock item (or create one)
			if s := dv.selectedStock(a); s != nil {
				a.startFlow(&flow{kind: "add", partPK: part.PK, stockPK: s.PK},
					fmt.Sprintf("Add how many to %s?", s.LocationPath()))
			} else {
				a.startFlow(&flow{kind: "newstock", partPK: part.PK}, "Quantity for new stock?")
			}
		case "r": // remove quantity
			if s := dv.selectedStock(a); s != nil {
				a.startFlow(&flow{kind: "remove", partPK: part.PK, stockPK: s.PK},
					fmt.Sprintf("Remove how many from %s?", s.LocationPath()))
			}
		case "c": // count / stocktake
			if s := dv.selectedStock(a); s != nil {
				a.startFlow(&flow{kind: "count", partPK: part.PK, stockPK: s.PK},
					fmt.Sprintf("Counted quantity at %s?", s.LocationPath()))
			}
		case "m": // move to another bin
			if s := dv.selectedStock(a); s != nil {
				f := &flow{kind: "move", partPK: part.PK, stockPK: s.PK, qty: s.Quantity}
				a.openLocationPicker(f, "Move to which bin?")
			}
		case "n": // new stock item in a new bin
			a.startFlow(&flow{kind: "newstock", partPK: part.PK}, "Quantity for new stock?")
		case "d": // delete selected stock item
			if s := dv.selectedStock(a); s != nil {
				a.openConfirm(&flow{kind: "delstock", partPK: part.PK, stockPK: s.PK},
					fmt.Sprintf("Delete stock (%s × %s)?", s.LocationPath(), fmtQty(s.Quantity)))
			}
		case "e": // edit part
			a.openPartForm(part.PK)
		case "D": // delete part
			a.openConfirm(&flow{kind: "delpart", partPK: part.PK},
				fmt.Sprintf("Delete part %q entirely?", part.Name))
		case "ctrl+z":
			a.undoLast()
		case "ctrl+y":
			a.redoLast()
		case "?":
			return a, a.openHelp()
		}
		return a, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == 0 { // tap title row = back
				a.mode = modeSearch
				return a, nil
			}
			row := msg.Y - dv.headerEnd
			if row >= 0 && row < len(a.stockByPart[dv.partPK]) {
				dv.cursor = row
			}
		}
		return a, nil
	}
	return a, nil
}

func (a *App) viewDetail() string {
	dv := &a.detail
	part := a.partByPK(dv.partPK)
	if part == nil {
		return ""
	}
	var b strings.Builder

	title := part.Name
	if part.IPN != "" {
		title = part.IPN + " | " + title
	}
	b.WriteString(styTitle.Render(truncate("← "+title, a.width)) + "\n")

	rows := 1
	write := func(label, val string) {
		if val == "" {
			return
		}
		b.WriteString(styDim.Render(padRight(label, 10)) + truncate(val, a.width-11) + "\n")
		rows++
	}
	write("Category", part.CategoryName)
	for _, line := range wrap(part.Description, a.width-11) {
		write("", line)
	}
	write("Keywords", part.Keywords)
	if part.Link != "" {
		write("Link", part.Link)
	}
	total := fmtQty(part.TotalInStock)
	if part.Units != "" {
		total += " " + part.Units
	}
	if part.MinimumStock > 0 {
		total += fmt.Sprintf("  (min %s)", fmtQty(part.MinimumStock))
	}
	if !part.Active {
		total += "  INACTIVE"
	}
	write("In stock", total)

	b.WriteString("\n" + styHeader.Render(padRight("Bin", a.width-20)+"  "+padRight("Qty", 8)+"  Status") + "\n")
	rows += 2
	dv.headerEnd = rows

	items := a.stockByPart[dv.partPK]
	if len(items) == 0 {
		b.WriteString(styDim.Render("  (no stock items -- press 'a' to add stock)") + "\n")
		rows++
	}
	for i, s := range items {
		line := padRight(s.LocationPath(), a.width-20) + "  " + padRight(fmtQty(s.Quantity), 8) + "  " + s.StatusText
		if i == dv.cursor {
			b.WriteString(stySel.Render(truncate(line, a.width)) + "\n")
		} else {
			b.WriteString(truncate(line, a.width) + "\n")
		}
		rows++
	}

	// pad to bottom, then the bottom bar
	for rows < a.height-1 {
		b.WriteString("\n")
		rows++
	}
	hints := "a:add  r:remove  c:count  m:move  n:new-bin  d/D:delete  e:edit  F1:help  esc:back"
	b.WriteString(a.bottomBar(hints))
	return b.String()
}

func wrap(s string, w int) []string {
	if s == "" || w < 10 {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, word := range words {
		if cur == "" {
			cur = word
		} else if len(cur)+1+len(word) <= w {
			cur += " " + word
		} else {
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
