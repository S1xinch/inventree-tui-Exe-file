package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AndrewYatzkan/inventree-tui/buzz"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mode int

const (
	modeLoading mode = iota
	modeSearch
	modeDetail
	modeForm
	modePicker
	modePrompt
	modeConfirm
	modeHermes
	modeHelp
)

// Styles kept deliberately light: the target is a Linux framebuffer console
// on a Pi 1, so we stick to the 16 ANSI colors and cheap attributes.
var (
	styTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stySel      = lipgloss.NewStyle().Reverse(true)
	styErr      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	styOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styBin      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styHeader   = lipgloss.NewStyle().Bold(true).Underline(true)
	styLowStock = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

type App struct {
	client       *Client
	hermes       *HermesClient // nil when not configured
	journal      *Journal
	fonts        *fontCtl
	buzzer       *buzz.Buzzer // nil when pigpiod not available
	width        int
	height       int
	mode         mode
	helpReturn   mode // where to go back to when help closes
	helpFontPrev int  // font idx to restore on help close (-1 = none)

	// data store (loaded fully at startup; refreshed after mutations)
	parts       []Part
	stockByPart map[int][]StockItem
	locations   []Location
	categories  []Category
	docs        []searchDoc // precomputed search text, aligned with parts
	docsDirty   bool

	search     SearchView
	detail     DetailView
	form       *Form
	picker     *Picker
	prompt     *Prompt
	confirm    *Confirm
	hermesView HermesView

	flow *flow // multi-step stock action in progress

	status    string
	statusErr bool
	statusAt  time.Time
	loading   bool

	// busy serializes mutations: while one is in flight (including undo and
	// redo), starting another is rejected. This makes a rapid double ctrl+z
	// (which would otherwise race the journal append and undo the same entry
	// twice) impossible.
	busy bool

	lowAlertPending bool // raise the low-stock alert when the next load lands
	quietReload     bool // background reload: don't announce "loaded N parts"
}

// flow tracks a multi-step stock operation (qty prompt -> location pick -> exec).
type flow struct {
	kind    string // add, remove, count, move, newstock, delstock, delpart, undo
	partPK  int
	stockPK int
	qty     float64
	loc     int
	entry   *JEntry // journal entry being undone (kind == "undo")
}

func NewApp(client *Client, buzzerPin uint) *App {
	a := &App{
		client:      client,
		mode:        modeLoading,
		stockByPart: map[int][]StockItem{},
	}
	a.search = NewSearchView()
	a.hermesView.input = textinput.New()
	a.journal = openJournal()
	a.fonts = newFontCtl()
	a.buzzer = buzz.NewBuzzer(buzzerPin)
	a.lowAlertPending = true // check right after the first load
	return a
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.loadAllCmd(), a.search.input.Cursor.BlinkCmd(), statusTick(), lowStockTick())
}

// ---- Messages ----

type dataLoadedMsg struct {
	parts      []Part
	stock      []StockItem
	locations  []Location
	categories []Category
	note       string // overrides the default "loaded N parts" status
	err        error
}

type partRefreshedMsg struct {
	part     Part
	stock    []StockItem
	note     string
	goDetail bool // open the detail view for this part (after create)
	err      error
}

type partDeletedMsg struct {
	pk  int
	err error
}

type statusMsg struct {
	text  string
	isErr bool
}

// ---- Commands ----

func (a *App) loadAllCmd() tea.Cmd {
	c := a.client
	return func() tea.Msg {
		parts, err := c.FetchParts()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		stock, err := c.FetchStock()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		locs, err := c.FetchLocations()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		cats, err := c.FetchCategories()
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		return dataLoadedMsg{parts: parts, stock: stock, locations: locs, categories: cats}
	}
}

// refreshPartCmd re-fetches one part and its stock after a mutation.
func (a *App) refreshPartCmd(pk int, note string) tea.Cmd {
	c := a.client
	return func() tea.Msg {
		part, err := c.FetchPart(pk)
		if err != nil {
			return partRefreshedMsg{err: err}
		}
		stock, err := c.FetchStockForPart(pk)
		if err != nil {
			return partRefreshedMsg{err: err}
		}
		return partRefreshedMsg{part: part, stock: stock, note: note}
	}
}

// mutateCmd is the funnel for journaled mutations: snapshot the pre-image
// (abort if that fails -- no snapshot, no change), run the operation, journal
// it, then refresh the affected part.
func (a *App) mutateCmd(e *JEntry, note string, op func() error) tea.Cmd {
	c, j := a.client, a.journal
	refresh := a.refreshPartCmd(e.PartPK, note)
	refreshWarn := a.refreshPartCmd(e.PartPK, note+" (warning: journal write failed)")
	return func() tea.Msg {
		if err := snapshot(c, e); err != nil {
			return statusMsg{text: err.Error(), isErr: true}
		}
		if err := op(); err != nil {
			return statusMsg{text: err.Error(), isErr: true}
		}
		if err := j.Append(e); err != nil {
			return refreshWarn()
		}
		return refresh()
	}
}

// partLabel names a part for journal summaries.
func (a *App) partLabel(pk int) string {
	if p := a.partByPK(pk); p != nil {
		return p.Name
	}
	return fmt.Sprintf("part #%d", pk)
}

// runMutation gates mutation dispatch: only one in flight at a time.
func (a *App) runMutation(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cmd == nil {
		return a, nil
	}
	if a.busy {
		a.setStatus("busy: previous change still in progress", true)
		return a, nil
	}
	a.busy = true
	return a, cmd
}

// undoLast / redoLast open a confirm dialog for the next reversible entry.
func (a *App) undoLast() {
	a.openReversal(a.journal.LastUndoable, "Undo", "nothing to undo")
}

func (a *App) redoLast() {
	a.openReversal(a.journal.LastRedoable, "Redo", "nothing to redo")
}

func (a *App) openReversal(pick func() (*JEntry, error), verb, emptyMsg string) {
	if a.busy {
		a.setStatus("busy: previous change still in progress", true)
		return
	}
	e, err := pick()
	if err != nil {
		a.setStatus("journal: "+err.Error(), true)
		return
	}
	if e == nil {
		a.setStatus(emptyMsg, false)
		return
	}
	a.openConfirm(&flow{kind: "undo", entry: e},
		fmt.Sprintf("%s #%d (%s)?", verb, e.ID, e.Summary))
}

// ---- Update ----

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		if a.width <= 0 || a.height <= 0 {
			a.width, a.height = 80, 24
		}
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "f1":
			return a, a.openHelp()
		case "ctrl+l":
			if a.mode == modeSearch || a.mode == modeDetail {
				a.showLowStock()
				return a, nil
			}
		case "ctrl+b":
			if muted := a.buzzer.ToggleMute(); muted {
				a.setStatus("sound off", false)
			} else {
				a.setStatus("sound on", false)
				a.buzzer.Play("ok")
			}
		case "f2", "f3", "alt++", "alt+=", "alt+-":
			// ctrl+plus/minus would be the familiar chord, but terminals
			// don't transmit those as distinct bytes; alt chords arrive
			// ESC-prefixed and do work.
			delta := 1
			if msg.String() == "f2" || msg.String() == "alt+-" {
				delta = -1
			}
			if err := a.fonts.change(delta); err != nil {
				a.setStatus(err.Error(), true)
			} else if a.statusErr {
				a.setStatus("", false) // a size change clears the limit warning
			}
			// console font change resizes the terminal; re-measure
			return a, tea.WindowSize()
		}

	case dataLoadedMsg:
		a.loading = false
		a.busy = false
		if msg.err != nil {
			if a.mode == modeLoading {
				a.mode = modeSearch
			}
			a.setStatus("load failed: "+msg.err.Error(), true)
			return a, nil
		}
		a.applyData(msg)
		if a.mode == modeLoading {
			a.mode = modeSearch
		}
		a.search.refilter(a)
		quiet := a.quietReload
		a.quietReload = false
		if msg.note != "" {
			a.setStatus(msg.note, false)
		} else if !quiet {
			a.setStatus(fmt.Sprintf("loaded %d parts, %d stock items", len(a.parts), len(msg.stock)), false)
		}
		if a.lowAlertPending {
			a.raiseLowAlert()
		}
		return a, nil

	case lowStockTickMsg:
		return a, a.onLowStockTick()

	case partRefreshedMsg:
		a.busy = false
		if msg.err != nil {
			a.buzzer.Play("error")
			a.setStatus(msg.err.Error(), true)
			return a, nil
		}
		a.upsertPart(msg.part, msg.stock)
		a.search.refilter(a)
		a.detail.clampCursor(a)
		if msg.goDetail {
			a.openDetail(msg.part.PK)
		}
		if msg.note != "" {
			verb := msg.note
			if strings.HasPrefix(verb, "undid") {
				a.buzzer.Play("undo")
			} else if strings.HasPrefix(verb, "redid") {
				a.buzzer.Play("redo")
			} else {
				a.buzzer.Play("ok")
			}
			a.setStatus(msg.note, false)
		}
		return a, nil

	case partDeletedMsg:
		a.busy = false
		if msg.err != nil {
			a.buzzer.Play("error")
			a.setStatus(msg.err.Error(), true)
			return a, nil
		}
		a.removePart(msg.pk)
		a.search.refilter(a)
		a.mode = modeSearch
		a.buzzer.Play("ok")
		a.setStatus("part deleted", false)
		return a, nil

	case statusMsg:
		a.busy = false
		if msg.isErr {
			a.buzzer.Play("error")
		}
		a.setStatus(msg.text, msg.isErr)
		return a, nil

	case statusTickMsg:
		if a.status != "" {
			ttl := statusTTL
			if a.statusErr {
				ttl = statusErrTTL
			}
			if time.Since(a.statusAt) > ttl {
				a.status = ""
			}
		}
		return a, statusTick()

	case hermesReplyMsg:
		a.handleHermesReply(msg)
		if msg.err == nil {
			a.buzzer.Play("hermes")
		} else {
			a.buzzer.Play("error")
		}
		return a, nil

	case hermesTickMsg:
		return a.updateHermes(msg)
	}

	switch a.mode {
	case modeLoading:
		return a, nil
	case modeSearch:
		return a.updateSearch(msg)
	case modeDetail:
		return a.updateDetail(msg)
	case modeForm:
		return a.updateForm(msg)
	case modePicker:
		return a.updatePicker(msg)
	case modePrompt:
		return a.updatePrompt(msg)
	case modeConfirm:
		return a.updateConfirm(msg)
	case modeHermes:
		return a.updateHermes(msg)
	case modeHelp:
		return a.updateHelp(msg)
	}
	return a, nil
}

func (a *App) applyData(msg dataLoadedMsg) {
	a.parts = msg.parts
	sort.Slice(a.parts, func(i, j int) bool {
		return strings.ToLower(a.parts[i].Name) < strings.ToLower(a.parts[j].Name)
	})
	a.locations = msg.locations
	sort.Slice(a.locations, func(i, j int) bool {
		return strings.ToLower(a.locations[i].PathString) < strings.ToLower(a.locations[j].PathString)
	})
	a.categories = msg.categories
	a.stockByPart = map[int][]StockItem{}
	for _, s := range msg.stock {
		a.stockByPart[s.Part] = append(a.stockByPart[s.Part], s)
	}
	a.docsDirty = true
}

func (a *App) upsertPart(p Part, stock []StockItem) {
	found := false
	for i := range a.parts {
		if a.parts[i].PK == p.PK {
			a.parts[i] = p
			found = true
			break
		}
	}
	if !found {
		a.parts = append(a.parts, p)
		sort.Slice(a.parts, func(i, j int) bool {
			return strings.ToLower(a.parts[i].Name) < strings.ToLower(a.parts[j].Name)
		})
	}
	a.stockByPart[p.PK] = stock
	a.docsDirty = true
}

func (a *App) removePart(pk int) {
	for i := range a.parts {
		if a.parts[i].PK == pk {
			a.parts = append(a.parts[:i], a.parts[i+1:]...)
			break
		}
	}
	delete(a.stockByPart, pk)
	a.docsDirty = true
}

func (a *App) partByPK(pk int) *Part {
	for i := range a.parts {
		if a.parts[i].PK == pk {
			return &a.parts[i]
		}
	}
	return nil
}

// Status messages expire on their own (errors get longer to be read).
const (
	statusTTL    = 3 * time.Second
	statusErrTTL = 6 * time.Second
)

func (a *App) setStatus(text string, isErr bool) {
	a.status = text
	a.statusErr = isErr
	a.statusAt = time.Now()
}

type statusTickMsg struct{}

func statusTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

// binSummary returns a compact "where is it" string for a part.
func (a *App) binSummary(pk int) string {
	items := a.stockByPart[pk]
	if len(items) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var bins []string
	for _, s := range items {
		p := s.LocationPath()
		if !seen[p] {
			seen[p] = true
			bins = append(bins, p)
		}
	}
	return strings.Join(bins, ", ")
}

// ---- View ----

func (a *App) View() string {
	if a.width == 0 {
		return ""
	}
	switch a.mode {
	case modeLoading:
		return "\n  Loading inventory from " + a.client.BaseURL + " ..."
	case modeSearch:
		return a.viewSearch()
	case modeDetail:
		return a.viewDetail()
	case modeForm:
		return a.form.view(a)
	case modePicker:
		return a.picker.view(a)
	case modePrompt:
		return a.prompt.view(a)
	case modeConfirm:
		return a.confirm.view(a)
	case modeHermes:
		return a.viewHermes()
	case modeHelp:
		return a.viewHelp()
	}
	return ""
}

func (a *App) statusBar() string {
	s := a.status
	if s == "" {
		return ""
	}
	if a.statusErr {
		return styErr.Render(truncate(s, a.width))
	}
	return styOK.Render(truncate(s, a.width))
}

// bottomBar is the single bottom line of a view: key hints, temporarily
// replaced by the status message while one is showing.
func (a *App) bottomBar(hints string) string {
	if a.status != "" {
		return a.statusBar()
	}
	return styDim.Render(truncate(hints, a.width))
}

// ---- small helpers ----

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

func padRight(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return truncate(s, w)
	}
	return s + strings.Repeat(" ", w-len(r))
}

// scrollStep: a mouse wheel notch scrolls 3 lines; a touch drag (marked with
// Shift by the gesture translator) scrolls 1 line per cell, tracking the
// finger 1:1.
func scrollStep(msg tea.MouseMsg) int {
	if msg.Shift {
		return 1
	}
	return 3
}

func fmtQty(q float64) string {
	if q == float64(int64(q)) {
		return fmt.Sprintf("%d", int64(q))
	}
	return fmt.Sprintf("%.2f", q)
}
