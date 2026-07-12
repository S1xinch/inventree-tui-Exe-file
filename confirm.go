package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------- Confirm: y/n dialog ----------

type Confirm struct {
	message    string
	f          *flow
	returnMode mode
}

func (a *App) openConfirm(f *flow, message string) {
	a.confirm = &Confirm{message: message, f: f, returnMode: a.mode}
	a.mode = modeConfirm
}

func (a *App) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	cf := a.confirm
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	switch key.String() {
	case "y", "Y":
		a.mode = cf.returnMode
		c, j := a.client, a.journal
		switch cf.f.kind {
		case "delstock":
			e := &JEntry{Action: "stock.delete", PartPK: cf.f.partPK, StockPK: cf.f.stockPK,
				Summary: fmt.Sprintf("delete stock #%d of %q", cf.f.stockPK, a.partLabel(cf.f.partPK))}
			return a.runMutation(a.mutateCmd(e, "stock deleted", func() error {
				return c.DeleteStock(cf.f.stockPK)
			}))
		case "delpart":
			pk := cf.f.partPK
			e := &JEntry{Action: "part.delete", PartPK: pk,
				Summary: fmt.Sprintf("delete part %q", a.partLabel(pk))}
			return a.runMutation(func() tea.Msg {
				if err := snapshot(c, e); err != nil {
					return statusMsg{text: err.Error(), isErr: true}
				}
				if err := c.DeletePart(pk); err != nil {
					return partDeletedMsg{pk: pk, err: err}
				}
				if err := j.Append(e); err != nil {
					return partDeletedMsg{pk: pk} // deleted; journal warning lost is acceptable
				}
				return partDeletedMsg{pk: pk}
			})
		case "undo":
			entry := cf.f.entry
			loadAll := a.loadAllCmd()
			verb, failVerb := "undid", "undo"
			if entry.UndoOf != 0 {
				verb, failVerb = "redid", "redo"
			}
			return a.runMutation(func() tea.Msg {
				summary, err := j.Reverse(c, entry)
				if err != nil {
					return statusMsg{text: failVerb + " failed: " + err.Error(), isErr: true}
				}
				msg := loadAll().(dataLoadedMsg)
				msg.note = fmt.Sprintf("%s #%d: %s", verb, entry.ID, summary)
				return msg
			})
		}
	case "n", "N", "esc":
		a.mode = cf.returnMode
	}
	return a, nil
}

func (cf *Confirm) view(a *App) string {
	var b strings.Builder
	b.WriteString("\n  " + styErr.Render(cf.message) + "\n\n")
	b.WriteString("  " + styDim.Render("y:yes  n:no") + "\n")
	return b.String()
}
