package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// How often the low-stock check runs. Each check re-reads the inventory file
// (so edits from another copy of the program show up) and flags every part
// whose total stock is below the minimum set on it.
const lowStockInterval = 2 * time.Minute

type lowStockTickMsg struct{}

func lowStockTick() tea.Cmd {
	return tea.Tick(lowStockInterval, func(time.Time) tea.Msg { return lowStockTickMsg{} })
}

// isLow: a part is short when it has a minimum set and is below it.
func isLow(p Part) bool {
	return p.MinimumStock > 0 && p.TotalInStock < p.MinimumStock
}

// lowParts lists every short part, emptiest first.
func (a *App) lowParts() []Part {
	var out []Part
	for _, p := range a.parts {
		if isLow(p) {
			out = append(out, p)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].TotalInStock/out[j].MinimumStock < out[j-1].TotalInStock/out[j-1].MinimumStock; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// lowStockSummary is the one-line alert, e.g.
// "3 items low: AA batteries (1/4), M3 screws (10/50), +1 more  [ctrl+l to list]"
func (a *App) lowStockSummary() string {
	low := a.lowParts()
	if len(low) == 0 {
		return ""
	}
	noun := "items"
	if len(low) == 1 {
		noun = "item"
	}
	var names []string
	for i, p := range low {
		if i == 3 {
			names = append(names, fmt.Sprintf("+%d more", len(low)-3))
			break
		}
		names = append(names, fmt.Sprintf("%s (%s/%s)", p.Name, fmtQty(p.TotalInStock), fmtQty(p.MinimumStock)))
	}
	return fmt.Sprintf("%d %s low: %s  [ctrl+l to list]", len(low), noun, strings.Join(names, ", "))
}

// onLowStockTick reloads (when nothing is mid-edit) and raises the alert.
func (a *App) onLowStockTick() tea.Cmd {
	cmds := []tea.Cmd{lowStockTick()}
	if !a.busy && !a.loading && (a.mode == modeSearch || a.mode == modeDetail) {
		// the alert is raised when the reload lands (see dataLoadedMsg)
		a.loading = true
		a.lowAlertPending = true
		a.quietReload = true
		cmds = append(cmds, a.loadAllCmd())
	} else {
		a.raiseLowAlert()
	}
	return tea.Batch(cmds...)
}

// raiseLowAlert shows the summary in the status line and rings the terminal
// bell (and the Pi buzzer, if there is one).
func (a *App) raiseLowAlert() {
	a.lowAlertPending = false
	msg := a.lowStockSummary()
	if msg == "" {
		return
	}
	a.setStatus(msg, true)
	a.buzzer.Play("error")
	fmt.Fprint(os.Stderr, "\a")
}

// showLowStock switches the search to the short list.
func (a *App) showLowStock() {
	a.mode = modeSearch
	a.search.input.SetValue("is:low")
	a.search.input.CursorEnd()
	a.search.refilter(a)
	if len(a.search.results) == 0 {
		a.setStatus("nothing is below its minimum", false)
	}
}
