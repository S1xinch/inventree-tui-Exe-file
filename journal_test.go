package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tempJournal(t *testing.T) *Journal {
	t.Helper()
	return &Journal{path: filepath.Join(t.TempDir(), "journal.jsonl")}
}

func TestJournalAppendReadUndoable(t *testing.T) {
	j := tempJournal(t)
	if e, _ := j.LastUndoable(); e != nil {
		t.Fatalf("empty journal should have nothing to undo")
	}
	a1 := &JEntry{Action: "stock.add", PartPK: 1, StockPK: 2, Qty: 3, Summary: "add 3"}
	if err := j.Append(a1); err != nil {
		t.Fatal(err)
	}
	a2 := &JEntry{Action: "part.update", PartPK: 1, Summary: "edit"}
	if err := j.Append(a2); err != nil {
		t.Fatal(err)
	}
	if a1.ID != 1 || a2.ID != 2 {
		t.Fatalf("expected sequential ids, got %d %d", a1.ID, a2.ID)
	}

	e, err := j.LastUndoable()
	if err != nil || e == nil || e.ID != 2 {
		t.Fatalf("expected last undoable #2, got %+v err=%v", e, err)
	}
	// mark #2 undone -> #1 becomes the next undoable
	if err := j.Append(&JEntry{Action: "undo", UndoOf: 2, Summary: "undo 2"}); err != nil {
		t.Fatal(err)
	}
	e, _ = j.LastUndoable()
	if e == nil || e.ID != 1 {
		t.Fatalf("expected #1 next, got %+v", e)
	}
}

func TestJournalRotation(t *testing.T) {
	j := tempJournal(t)
	for i := 0; i < maxJournalEntries+25; i++ {
		if err := j.Append(&JEntry{Action: "stock.add", Summary: fmt.Sprintf("e%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := j.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxJournalEntries {
		t.Fatalf("expected %d entries after rotation, got %d", maxJournalEntries, len(entries))
	}
	// ids must stay monotonic across rotation
	if got := entries[len(entries)-1].ID; got != maxJournalEntries+25 {
		t.Errorf("expected last id %d, got %d", maxJournalEntries+25, got)
	}
	if entries[0].ID != 26 {
		t.Errorf("expected oldest kept id 26, got %d", entries[0].ID)
	}
}

func TestUndoRedoChain(t *testing.T) {
	j := tempJournal(t)
	for i := 0; i < 3; i++ {
		j.Append(&JEntry{Action: "stock.add", StockPK: 10 + i, Qty: 1, Summary: fmt.Sprintf("m%d", i+1)})
	}
	// undo m3, then m2
	u1 := &JEntry{Action: "stock.remove", StockPK: 12, Qty: 1, UndoOf: 3, Summary: "undo m3"}
	j.Append(u1)
	u2 := &JEntry{Action: "stock.remove", StockPK: 11, Qty: 1, UndoOf: 2, Summary: "undo m2"}
	j.Append(u2)

	if e, _ := j.LastUndoable(); e == nil || e.ID != 1 {
		t.Fatalf("expected m1 next undoable, got %+v", e)
	}
	// redo should target the most recent un-redone undo: u2
	if e, _ := j.LastRedoable(); e == nil || e.ID != u2.ID {
		t.Fatalf("expected u2 redoable, got %+v", e)
	}
	r1 := &JEntry{Action: "stock.add", StockPK: 11, Qty: 1, RedoOf: u2.ID, Summary: "redo m2"}
	j.Append(r1)
	// next redo walks deeper: u1
	if e, _ := j.LastRedoable(); e == nil || e.ID != u1.ID {
		t.Fatalf("expected u1 redoable after r1, got %+v", e)
	}
	// ctrl+z now targets the redo entry itself (the re-applied m2)
	if e, _ := j.LastUndoable(); e == nil || e.ID != r1.ID {
		t.Fatalf("expected r1 undoable, got %+v", e)
	}
	// a fresh normal mutation clears the redo chain
	j.Append(&JEntry{Action: "stock.add", StockPK: 99, Qty: 1, Summary: "m4"})
	if e, _ := j.LastRedoable(); e != nil {
		t.Fatalf("new mutation should clear redo, got %+v", e)
	}
}

func TestBuildInverse(t *testing.T) {
	pre := []json.RawMessage{json.RawMessage(`{"pk": 5, "quantity": 9.0, "location": 3}`)}
	cases := []struct {
		in         JEntry
		wantAction string
		wantQty    float64
		wantLoc    int
	}{
		{JEntry{Action: "stock.add", StockPK: 5, Qty: 2}, "stock.remove", 2, 0},
		{JEntry{Action: "stock.remove", StockPK: 5, Qty: 2}, "stock.add", 2, 0},
		{JEntry{Action: "stock.count", StockPK: 5, Qty: 4, PreStock: pre}, "stock.count", 9, 0},
		{JEntry{Action: "stock.transfer", StockPK: 5, Qty: 9, ToLoc: 7, PreStock: pre}, "stock.transfer", 9, 3},
		{JEntry{Action: "stock.create", PartPK: 1, NewStockPK: 5, Qty: 2}, "stock.delete", 0, 0},
		{JEntry{Action: "stock.delete", PartPK: 1, StockPK: 5, PreStock: pre}, "stock.create", 9, 3},
		{JEntry{Action: "part.create", NewPartPK: 8}, "part.delete", 0, 0},
	}
	for _, c := range cases {
		inv, err := buildInverse(&c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in.Action, err)
			continue
		}
		if inv.Action != c.wantAction {
			t.Errorf("%s: inverse action %s, want %s", c.in.Action, inv.Action, c.wantAction)
		}
		if c.wantQty != 0 && inv.Qty != c.wantQty {
			t.Errorf("%s: inverse qty %v, want %v", c.in.Action, inv.Qty, c.wantQty)
		}
		if c.wantLoc != 0 && inv.ToLoc != c.wantLoc {
			t.Errorf("%s: inverse loc %d, want %d", c.in.Action, inv.ToLoc, c.wantLoc)
		}
	}
}

func TestPreStockFields(t *testing.T) {
	e := &JEntry{PreStock: []json.RawMessage{
		json.RawMessage(`{"pk": 7, "quantity": 12.0, "location": 64}`),
	}}
	qty, loc, ok := e.preStockFields(7)
	if !ok || qty != 12 || loc != 64 {
		t.Fatalf("got qty=%v loc=%d ok=%v", qty, loc, ok)
	}
	if _, _, ok := e.preStockFields(99); ok {
		t.Fatal("unknown pk should not match")
	}
}

func TestBusyGateSerializesMutations(t *testing.T) {
	a := testApp()
	a.journal = tempJournal(t)

	// while a mutation is in flight, undo/redo and new mutations are rejected
	a.busy = true
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if a.mode != modeSearch {
		t.Fatalf("ctrl+z while busy must not open a confirm dialog")
	}
	if a.status == "" || !a.statusErr {
		t.Fatalf("expected busy status, got %q", a.status)
	}
	if _, cmd := a.runMutation(func() tea.Msg { return nil }); cmd != nil {
		t.Fatal("runMutation while busy must not dispatch")
	}

	// completion messages clear the gate
	a.Update(partRefreshedMsg{part: Part{PK: 1, Name: "M3 Screw", Active: true}})
	if a.busy {
		t.Fatal("partRefreshedMsg should clear busy")
	}
	// dispatch sets it again
	if _, cmd := a.runMutation(func() tea.Msg { return nil }); cmd == nil || !a.busy {
		t.Fatal("runMutation when idle should dispatch and set busy")
	}
}

// Live undo round-trip: the scary case -- delete a part, then resurrect it
// from the journal pre-image. Skipped unless INVENTREE_* env vars are set.
func TestLiveUndoDeletePart(t *testing.T) {
	url, user, pass := os.Getenv("INVENTREE_URL"), os.Getenv("INVENTREE_USERNAME"), os.Getenv("INVENTREE_PASSWORD")
	if url == "" || user == "" || pass == "" {
		t.Skip("INVENTREE_* env vars not set")
	}
	c := NewClient(url, user, pass)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	j := tempJournal(t)

	cats, err := c.FetchCategories()
	if err != nil || len(cats) == 0 {
		t.Fatal(err)
	}
	locs, err := c.FetchLocations()
	if err != nil || len(locs) == 0 {
		t.Fatal(err)
	}

	part, err := c.CreatePart(map[string]any{
		"name":        "ZZZ inv-tui undo-test part",
		"description": "temporary part for journal undo test; safe to delete",
		"keywords":    "selftest undo",
		"category":    cats[0].PK,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanupPK := part.PK
	defer func() {
		if cleanupPK != 0 {
			for _, s := range mustStock(t, c, cleanupPK) {
				c.DeleteStock(s.PK)
			}
			if err := c.DeletePart(cleanupPK); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	}()
	if _, err := c.CreateStock(part.PK, 7, locs[0].PK); err != nil {
		t.Fatal(err)
	}

	// journal + delete, exactly as the TUI does
	e := &JEntry{Action: "part.delete", PartPK: part.PK, Summary: "delete test part"}
	if err := snapshot(c, e); err != nil {
		t.Fatal(err)
	}
	if err := c.DeletePart(part.PK); err != nil {
		t.Fatal(err)
	}
	cleanupPK = 0
	if err := j.Append(e); err != nil {
		t.Fatal(err)
	}

	// undo: part should come back with same fields and stock
	summary, err := j.Reverse(c, e)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	t.Log(summary)

	parts, err := c.FetchParts()
	if err != nil {
		t.Fatal(err)
	}
	for i := range parts {
		if parts[i].Name == "ZZZ inv-tui undo-test part" {
			cleanupPK = parts[i].PK
			if parts[i].Keywords != "selftest undo" {
				t.Errorf("keywords not restored: %q", parts[i].Keywords)
			}
			stock := mustStock(t, c, cleanupPK)
			if len(stock) != 1 || stock[0].Quantity != 7 {
				t.Fatalf("stock not restored: %+v", stock)
			}
			if stock[0].Location == nil || *stock[0].Location != locs[0].PK {
				t.Errorf("location not restored: %+v", stock[0].Location)
			}
			return
		}
	}
	t.Fatal("recreated part not found")
}

// Live undo -> redo round-trip on a quantity change.
func TestLiveUndoRedoStockAdd(t *testing.T) {
	url, user, pass := os.Getenv("INVENTREE_URL"), os.Getenv("INVENTREE_USERNAME"), os.Getenv("INVENTREE_PASSWORD")
	if url == "" || user == "" || pass == "" {
		t.Skip("INVENTREE_* env vars not set")
	}
	c := NewClient(url, user, pass)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	j := tempJournal(t)

	cats, _ := c.FetchCategories()
	locs, _ := c.FetchLocations()
	part, err := c.CreatePart(map[string]any{
		"name": "ZZZ inv-tui redo-test part", "category": cats[0].PK,
		"description": "temporary part for redo test; safe to delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, s := range mustStock(t, c, part.PK) {
			c.DeleteStock(s.PK)
		}
		c.DeletePart(part.PK)
	}()
	stock, err := c.CreateStock(part.PK, 5, locs[0].PK)
	if err != nil {
		t.Fatal(err)
	}

	qtyNow := func() float64 {
		items := mustStock(t, c, part.PK)
		if len(items) != 1 {
			t.Fatalf("expected 1 stock item, got %d", len(items))
		}
		return items[0].Quantity
	}

	// journaled add, as the TUI does it: snapshot -> op -> append
	e := &JEntry{Action: "stock.add", PartPK: part.PK, StockPK: stock.PK, Qty: 3, Summary: "add 3"}
	if err := snapshot(c, e); err != nil {
		t.Fatal(err)
	}
	if err := c.StockAdd(stock.PK, 3); err != nil {
		t.Fatal(err)
	}
	j.Append(e)
	if q := qtyNow(); q != 8 {
		t.Fatalf("after add: qty %v, want 8", q)
	}

	// undo
	target, _ := j.LastUndoable()
	if target == nil || target.ID != e.ID {
		t.Fatalf("expected entry %d undoable, got %+v", e.ID, target)
	}
	if _, err := j.Reverse(c, target); err != nil {
		t.Fatal(err)
	}
	if q := qtyNow(); q != 5 {
		t.Fatalf("after undo: qty %v, want 5", q)
	}

	// redo
	redo, _ := j.LastRedoable()
	if redo == nil || redo.UndoOf != e.ID {
		t.Fatalf("expected the undo of #%d redoable, got %+v", e.ID, redo)
	}
	if _, err := j.Reverse(c, redo); err != nil {
		t.Fatal(err)
	}
	if q := qtyNow(); q != 8 {
		t.Fatalf("after redo: qty %v, want 8", q)
	}

	// and the redo itself is undoable again
	again, _ := j.LastUndoable()
	if again == nil || again.RedoOf == 0 {
		t.Fatalf("expected the redo entry undoable next, got %+v", again)
	}
}

func mustStock(t *testing.T, c *Client, pk int) []StockItem {
	t.Helper()
	stock, err := c.FetchStockForPart(pk)
	if err != nil {
		t.Fatal(err)
	}
	return stock
}
