package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Help overlay (F1; also "?" from the detail view). Lays out in two columns
// when the terminal is wide enough, one otherwise — and if the console font
// is zoomed in past the default, it temporarily drops back to the default
// size so the whole page fits, restoring the user's size on close.

func (a *App) openHelp() tea.Cmd {
	if a.mode == modeHelp {
		return nil
	}
	a.helpReturn = a.mode
	a.mode = modeHelp
	a.helpFontPrev = -1
	if prev := a.fonts.idx; prev > defaultFontIdx {
		if a.fonts.setIdx(defaultFontIdx, false) == nil {
			a.helpFontPrev = prev
		}
	}
	return tea.WindowSize()
}

func (a *App) closeHelp() tea.Cmd {
	a.mode = a.helpReturn
	if a.helpFontPrev >= 0 {
		a.fonts.setIdx(a.helpFontPrev, false)
		a.helpFontPrev = -1
	}
	return tea.WindowSize()
}

func (a *App) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a, a.closeHelp()
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			return a, a.closeHelp()
		}
	}
	return a, nil
}

func (a *App) helpColumns() (left, right []string) {
	left = []string{
		"Search",
		"  type           filter parts",
		"  bin:57 cat:tools  filter by bin/category",
		"  enter          open selection",
		"  ↑/↓ pgup pgdn  move selection",
		"  esc            clear search",
		"  ctrl+n         new part",
	}
	if a.hermes != nil {
		left = append(left, "  ctrl+h         ask hermes")
	}
	left = append(left,
		"",
		"Anywhere",
		"  ctrl+z / y     undo / redo",
		"  ctrl+r / F5    refresh data",
		"  F2 / F3        text size",
		"  ctrl+b         mute / unmute",
		"  F1             this help",
		"  ctrl+q         quit",
	)
	right = []string{
		"Part detail",
		"  a / r / c      add/remove/count",
		"  m              move to a bin",
		"  n              new stock in a bin",
		"  d / D          delete stock/part",
		"  e              edit part",
		"  ? / esc        help / back",
		"",
		"Touchscreen",
		"  tap            select / open",
		"  drag           scroll",
		"  tap title      go back",
	}
	return left, right
}

func (a *App) viewHelp() string {
	left, right := a.helpColumns()
	var lines []string
	lines = append(lines, styTitle.Render("Shortcuts"), "")
	if a.width >= 76 {
		colW := 38
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, padRight(l, colW)+r)
		}
	} else {
		lines = append(lines, left...)
		lines = append(lines, "")
		lines = append(lines, right...)
	}
	lines = append(lines, "", styDim.Render("press any key or tap to close"))

	var b strings.Builder
	for i, line := range lines {
		if a.height > 0 && i >= a.height {
			break
		}
		b.WriteString(truncate(line, a.width) + "\n")
	}
	return b.String()
}
