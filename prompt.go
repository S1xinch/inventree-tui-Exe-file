package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- Prompt: single-value question (quantities) ----------

type Prompt struct {
	title      string
	input      textinput.Model
	f          *flow
	returnMode mode
}

func (a *App) startFlow(f *flow, title string) {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 12
	if f.kind == "count" {
		// prefill nothing; count is a fresh number
	} else if f.kind == "move" && f.qty > 0 {
		ti.SetValue(fmtQty(f.qty))
	}
	a.prompt = &Prompt{title: title, input: ti, f: f, returnMode: a.mode}
	a.mode = modePrompt
}

func (a *App) updatePrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := a.prompt
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			a.mode = p.returnMode
			return a, nil
		case "enter":
			qty, err := strconv.ParseFloat(strings.TrimSpace(p.input.Value()), 64)
			if err != nil || qty < 0 {
				a.setStatus("enter a valid quantity", true)
				return a, nil
			}
			a.mode = p.returnMode
			return a.runMutation(a.flowQtyEntered(p.f, qty))
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return a, cmd
}

func (p *Prompt) view(a *App) string {
	var b strings.Builder
	b.WriteString("\n  " + styTitle.Render(p.title) + "\n\n")
	b.WriteString("  " + p.input.View() + "\n\n")
	b.WriteString("  " + styDim.Render("enter:ok  esc:cancel") + "\n")
	return b.String()
}

// flowQtyEntered continues a stock flow once a quantity is known.
func (a *App) flowQtyEntered(f *flow, qty float64) tea.Cmd {
	c := a.client
	label := a.partLabel(f.partPK)
	switch f.kind {
	case "add":
		e := &JEntry{Action: "stock.add", PartPK: f.partPK, StockPK: f.stockPK, Qty: qty,
			Summary: fmt.Sprintf("add %s to %q", fmtQty(qty), label)}
		return a.mutateCmd(e, "stock added", func() error { return c.StockAdd(f.stockPK, qty) })
	case "remove":
		e := &JEntry{Action: "stock.remove", PartPK: f.partPK, StockPK: f.stockPK, Qty: qty,
			Summary: fmt.Sprintf("remove %s from %q", fmtQty(qty), label)}
		return a.mutateCmd(e, "stock removed", func() error { return c.StockRemove(f.stockPK, qty) })
	case "count":
		e := &JEntry{Action: "stock.count", PartPK: f.partPK, StockPK: f.stockPK, Qty: qty,
			Summary: fmt.Sprintf("count %q as %s", label, fmtQty(qty))}
		return a.mutateCmd(e, "stock counted", func() error { return c.StockCount(f.stockPK, qty) })
	case "move":
		f.qty = qty
		e := &JEntry{Action: "stock.transfer", PartPK: f.partPK, StockPK: f.stockPK, Qty: qty, ToLoc: f.loc,
			Summary: fmt.Sprintf("move %s of %q to location %d", fmtQty(qty), label, f.loc)}
		return a.mutateCmd(e, "stock moved", func() error {
			return c.StockTransfer(f.stockPK, f.qty, f.loc)
		})
	case "newstock":
		f.qty = qty
		a.openLocationPicker(f, "Which bin?")
		return nil
	}
	return nil
}

// flowLocationPicked continues a flow once a location is chosen.
func (a *App) flowLocationPicked(f *flow, loc int) tea.Cmd {
	c := a.client
	switch f.kind {
	case "move":
		f.loc = loc
		a.startFlow(f, "How many to move?")
		return nil
	case "newstock":
		f.loc = loc
		e := &JEntry{Action: "stock.create", PartPK: f.partPK, Qty: f.qty, ToLoc: loc,
			Summary: fmt.Sprintf("new stock of %q (%s @ location %d)", a.partLabel(f.partPK), fmtQty(f.qty), loc)}
		return a.mutateCmd(e, "stock created", func() error {
			s, err := c.CreateStock(f.partPK, f.qty, f.loc)
			if err == nil {
				e.NewStockPK = s.PK
			}
			return err
		})
	}
	return nil
}
