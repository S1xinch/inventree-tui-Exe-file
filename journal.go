package main

// Every mutation this binary makes is journaled to a local JSONL file with a
// pre-image snapshot (raw API JSON) of the affected part and its stock, taken
// immediately before the change. InvenTree itself tracks stock history but
// has no undo, and deletes are permanent -- the journal makes everything
// reversible: ctrl+z in the TUI undoes the latest entry, and `inv-tui -log` /
// `inv-tui -undo <id>` handle older ones.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JEntry struct {
	ID      int     `json:"id"`
	Time    string  `json:"time"`
	Action  string  `json:"action"` // part.create/update/delete, stock.add/remove/count/transfer/create/delete
	Summary string  `json:"summary"`
	PartPK  int     `json:"part_pk,omitempty"`
	StockPK int     `json:"stock_pk,omitempty"`
	Qty     float64 `json:"qty,omitempty"`
	ToLoc   int     `json:"to_loc,omitempty"`

	// lineage: undo entries reference what they reversed; redo entries
	// reference the undo they reversed. Both are themselves faithful action
	// records, so the same reversal machinery applies all the way down.
	UndoOf int `json:"undo_of,omitempty"`
	RedoOf int `json:"redo_of,omitempty"`

	NewPartPK  int               `json:"new_part_pk,omitempty"`
	NewStockPK int               `json:"new_stock_pk,omitempty"`
	PrePart    json.RawMessage   `json:"pre_part,omitempty"`
	PreStock   []json.RawMessage `json:"pre_stock,omitempty"`

	// for part.create entries produced by reversal: the snapshot to recreate
	// the part (and its stock) from
	RestorePart  json.RawMessage   `json:"restore_part,omitempty"`
	RestoreStock []json.RawMessage `json:"restore_stock,omitempty"`
}

type Journal struct {
	path string
	mu   sync.Mutex
}

func journalPath() string {
	if p := os.Getenv("INV_TUI_JOURNAL"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state", "inv-tui", "journal.jsonl")
}

func openJournal() *Journal {
	p := journalPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	return &Journal{path: p}
}

// maxJournalEntries bounds the journal: the Pi has little free storage, so
// keep a generous-but-finite history (~500 entries x a few KB of pre-image
// each = single-digit MB worst case). Oldest entries are dropped on append;
// IDs keep increasing monotonically across rotation.
const maxJournalEntries = 500

func (j *Journal) Append(e *JEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	prev, err := j.readAllLocked()
	if err != nil {
		return err
	}
	e.ID = 1
	if len(prev) > 0 {
		e.ID = prev[len(prev)-1].ID + 1
	}
	e.Time = time.Now().Format(time.RFC3339)
	if len(prev) >= maxJournalEntries {
		// rotate: rewrite the file with the newest entries only
		keep := append(prev[len(prev)-maxJournalEntries+1:], *e)
		tmp := j.path + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		for i := range keep {
			data, err := json.Marshal(&keep[i])
			if err != nil {
				f.Close()
				return err
			}
			w.Write(data)
			w.WriteByte('\n')
		}
		if err := w.Flush(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, j.path)
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func (j *Journal) ReadAll() ([]JEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.readAllLocked()
}

func (j *Journal) readAllLocked() ([]JEntry, error) {
	f, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var entries []JEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var e JEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, sc.Err()
}

// LastUndoable returns the most recent normal-or-redo entry that has not
// already been undone.
func (j *Journal) LastUndoable() (*JEntry, error) {
	entries, err := j.ReadAll()
	if err != nil {
		return nil, err
	}
	undone := map[int]bool{}
	for _, e := range entries {
		if e.UndoOf != 0 {
			undone[e.UndoOf] = true
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.UndoOf == 0 && !undone[e.ID] {
			return &e, nil
		}
	}
	return nil, nil
}

// LastRedoable returns the most recent undo entry that has not been redone,
// unless a newer normal mutation has invalidated the redo chain (the
// standard editor rule: a fresh edit clears redo).
func (j *Journal) LastRedoable() (*JEntry, error) {
	entries, err := j.ReadAll()
	if err != nil {
		return nil, err
	}
	redone := map[int]bool{}
	for _, e := range entries {
		if e.RedoOf != 0 {
			redone[e.RedoOf] = true
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.UndoOf == 0 && e.RedoOf == 0 {
			return nil, nil // normal mutation after the undos: redo cleared
		}
		if e.UndoOf != 0 && !redone[e.ID] {
			return &e, nil
		}
	}
	return nil, nil
}

// ---- pre-image snapshots ----

// snapshot fills the entry's pre-images with the raw API JSON of the part and
// all of its stock items. Mutations are aborted if this fails: no snapshot,
// no change.
func snapshot(c *Client, e *JEntry) error {
	if e.PartPK == 0 {
		return nil
	}
	var part json.RawMessage
	if err := c.do("GET", fmt.Sprintf("/api/part/%d/", e.PartPK), nil, nil, &part); err != nil {
		return fmt.Errorf("pre-image snapshot failed (aborting): %w", err)
	}
	e.PrePart = part
	q := url.Values{}
	q.Set("part", fmt.Sprint(e.PartPK))
	q.Set("location_detail", "true")
	q.Set("in_stock", "true")
	q.Set("limit", "500")
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := c.do("GET", "/api/stock/", q, nil, &page); err != nil {
		return fmt.Errorf("pre-image snapshot failed (aborting): %w", err)
	}
	e.PreStock = page.Results
	return nil
}

// preStockFields decodes the snapshotted stock item with the given pk.
func (e *JEntry) preStockFields(pk int) (qty float64, loc int, ok bool) {
	for _, raw := range e.PreStock {
		var s struct {
			PK       int      `json:"pk"`
			Quantity float64  `json:"quantity"`
			Location *float64 `json:"location"`
		}
		if json.Unmarshal(raw, &s) == nil && (pk == 0 || s.PK == pk) {
			l := 0
			if s.Location != nil {
				l = int(*s.Location)
			}
			return s.Quantity, l, true
		}
	}
	return 0, 0, false
}

// writable part fields we restore on undo (the rest are read-only or computed)
var partWritableFields = []string{
	"name", "description", "IPN", "keywords", "units", "minimum_stock",
	"category", "link", "active", "assembly", "component", "purchaseable",
	"salable", "trackable", "virtual", "is_template",
}

func writablePartFields(raw json.RawMessage) (map[string]any, error) {
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, k := range partWritableFields {
		if v, ok := all[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out, nil
}

// ---- undo / redo ----
//
// Reversal works in two steps: buildInverse computes the faithful inverse
// action of an entry (using its pre-image), applyEntry executes any action
// entry forward. Because the inverse is journaled as a real action with its
// own pre-image, redo is simply "reverse the latest undo entry" through the
// exact same machinery, and chains of undo/redo compose.

// buildInverse computes the action that reverses e.
func buildInverse(e *JEntry) (*JEntry, error) {
	switch e.Action {
	case "stock.add":
		return &JEntry{Action: "stock.remove", PartPK: e.PartPK, StockPK: e.StockPK, Qty: e.Qty,
			Summary: fmt.Sprintf("remove %s back from stock #%d", fmtQty(e.Qty), e.StockPK)}, nil
	case "stock.remove":
		return &JEntry{Action: "stock.add", PartPK: e.PartPK, StockPK: e.StockPK, Qty: e.Qty,
			Summary: fmt.Sprintf("add %s back to stock #%d", fmtQty(e.Qty), e.StockPK)}, nil
	case "stock.count":
		qty, _, ok := e.preStockFields(e.StockPK)
		if !ok {
			return nil, fmt.Errorf("no pre-image for stock #%d", e.StockPK)
		}
		return &JEntry{Action: "stock.count", PartPK: e.PartPK, StockPK: e.StockPK, Qty: qty,
			Summary: fmt.Sprintf("restore count of stock #%d to %s", e.StockPK, fmtQty(qty))}, nil
	case "stock.transfer":
		_, loc, ok := e.preStockFields(e.StockPK)
		if !ok {
			return nil, fmt.Errorf("no pre-image for stock #%d", e.StockPK)
		}
		return &JEntry{Action: "stock.transfer", PartPK: e.PartPK, StockPK: e.StockPK, Qty: e.Qty, ToLoc: loc,
			Summary: fmt.Sprintf("move stock #%d back to location %d", e.StockPK, loc)}, nil
	case "stock.create":
		return &JEntry{Action: "stock.delete", PartPK: e.PartPK, StockPK: e.NewStockPK,
			Summary: fmt.Sprintf("delete created stock #%d", e.NewStockPK)}, nil
	case "stock.delete":
		qty, loc, ok := e.preStockFields(e.StockPK)
		if !ok {
			return nil, fmt.Errorf("no pre-image for stock #%d", e.StockPK)
		}
		return &JEntry{Action: "stock.create", PartPK: e.PartPK, Qty: qty, ToLoc: loc,
			Summary: fmt.Sprintf("recreate stock of part #%d (%s @ location %d)", e.PartPK, fmtQty(qty), loc)}, nil
	case "part.update":
		if len(e.PrePart) == 0 {
			return nil, fmt.Errorf("no pre-image for part #%d", e.PartPK)
		}
		return &JEntry{Action: "part.update", PartPK: e.PartPK, RestorePart: e.PrePart,
			Summary: fmt.Sprintf("restore previous fields of part #%d", e.PartPK)}, nil
	case "part.create":
		return &JEntry{Action: "part.delete", PartPK: e.NewPartPK,
			Summary: fmt.Sprintf("delete created part #%d", e.NewPartPK)}, nil
	case "part.delete":
		if len(e.PrePart) == 0 {
			return nil, fmt.Errorf("no pre-image for part #%d", e.PartPK)
		}
		return &JEntry{Action: "part.create", RestorePart: e.PrePart, RestoreStock: e.PreStock,
			Summary: "recreate deleted part from snapshot"}, nil
	}
	return nil, fmt.Errorf("don't know how to reverse action %q", e.Action)
}

// applyEntry executes an action entry forward, filling in NewPartPK /
// NewStockPK for creations. Returns a human summary.
func applyEntry(c *Client, e *JEntry) (string, error) {
	switch e.Action {
	case "stock.add":
		return e.Summary, c.StockAdd(e.StockPK, e.Qty)
	case "stock.remove":
		return e.Summary, c.StockRemove(e.StockPK, e.Qty)
	case "stock.count":
		return e.Summary, c.StockCount(e.StockPK, e.Qty)
	case "stock.transfer":
		return e.Summary, c.StockTransfer(e.StockPK, e.Qty, e.ToLoc)
	case "stock.delete":
		return e.Summary, c.DeleteStock(e.StockPK)
	case "stock.create":
		s, err := c.CreateStock(e.PartPK, e.Qty, e.ToLoc)
		if err != nil {
			return "", err
		}
		e.NewStockPK = s.PK
		return fmt.Sprintf("recreated stock as #%d (%s @ location %d)", s.PK, fmtQty(e.Qty), e.ToLoc), nil
	case "part.update":
		fields, err := writablePartFields(e.RestorePart)
		if err != nil {
			return "", err
		}
		_, err = c.UpdatePart(e.PartPK, fields)
		return e.Summary, err
	case "part.delete":
		// remove remaining stock first so the delete cannot half-fail
		stock, err := c.FetchStockForPart(e.PartPK)
		if err != nil {
			return "", err
		}
		for _, s := range stock {
			if err := c.DeleteStock(s.PK); err != nil {
				return "", err
			}
		}
		return e.Summary, c.DeletePart(e.PartPK)
	case "part.create":
		fields, err := writablePartFields(e.RestorePart)
		if err != nil {
			return "", err
		}
		p, err := c.CreatePart(fields)
		if err != nil {
			return "", err
		}
		e.NewPartPK = p.PK
		restored := 0
		for _, raw := range e.RestoreStock {
			var s struct {
				Quantity float64  `json:"quantity"`
				Location *float64 `json:"location"`
			}
			if json.Unmarshal(raw, &s) != nil {
				continue
			}
			loc := 0
			if s.Location != nil {
				loc = int(*s.Location)
			}
			if _, err := c.CreateStock(p.PK, s.Quantity, loc); err != nil {
				return "", fmt.Errorf("part recreated as #%d but stock restore failed: %w", p.PK, err)
			}
			restored++
		}
		return fmt.Sprintf("recreated part as #%d with %d stock item(s)", p.PK, restored), nil
	}
	return "", fmt.Errorf("don't know how to apply action %q", e.Action)
}

// Reverse undoes (or redoes) the given entry: it builds the inverse action,
// snapshots its pre-image, executes it, and journals it with lineage so
// LastUndoable / LastRedoable track the chain.
func (j *Journal) Reverse(c *Client, e *JEntry) (string, error) {
	inv, err := buildInverse(e)
	if err != nil {
		return "", err
	}
	if e.UndoOf != 0 {
		inv.RedoOf = e.ID // reversing an undo = redo
	} else {
		inv.UndoOf = e.ID
	}
	if err := snapshot(c, inv); err != nil {
		return "", err
	}
	summary, err := applyEntry(c, inv)
	if err != nil {
		return "", err
	}
	if err := j.Append(inv); err != nil {
		return summary, fmt.Errorf("reversed, but journaling it failed: %w", err)
	}
	return summary, nil
}
