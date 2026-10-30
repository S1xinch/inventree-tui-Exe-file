package main

import (
	"testing"
)

// Live round-trip test against a real InvenTree instance. Skipped unless
// INVENTREE_URL / INVENTREE_USERNAME / INVENTREE_PASSWORD are set.
// Creates a throwaway part, runs every mutation the TUI uses, then deletes it.
func TestLiveCRUD(t *testing.T) {
	c := testStore(t)

	cats, err := c.FetchCategories()
	if err != nil || len(cats) == 0 {
		t.Fatalf("categories: %v", err)
	}
	locs, err := c.FetchLocations()
	if err != nil || len(locs) < 2 {
		t.Fatalf("locations: %v", err)
	}

	part, err := c.CreatePart(map[string]any{
		"name":        "ZZZ inv-tui self-test part",
		"description": "temporary part created by inv-tui live test; safe to delete",
		"category":    cats[0].PK,
	})
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	t.Logf("created part %d", part.PK)
	defer func() {
		if err := c.DeletePart(part.PK); err != nil {
			t.Errorf("cleanup delete part %d: %v", part.PK, err)
		} else {
			t.Logf("deleted part %d", part.PK)
		}
	}()

	if _, err := c.UpdatePart(part.PK, map[string]any{"keywords": "selftest"}); err != nil {
		t.Fatalf("update part: %v", err)
	}

	stock, err := c.CreateStock(part.PK, 5, locs[0].PK)
	if err != nil {
		t.Fatalf("create stock: %v", err)
	}
	if err := c.StockAdd(stock.PK, 3); err != nil {
		t.Fatalf("stock add: %v", err)
	}
	if err := c.StockRemove(stock.PK, 2); err != nil {
		t.Fatalf("stock remove: %v", err)
	}
	if err := c.StockCount(stock.PK, 10); err != nil {
		t.Fatalf("stock count: %v", err)
	}
	if err := c.StockTransfer(stock.PK, 10, locs[1].PK); err != nil {
		t.Fatalf("stock transfer: %v", err)
	}

	items, err := c.FetchStockForPart(part.PK)
	if err != nil {
		t.Fatalf("fetch stock: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 stock item, got %d", len(items))
	}
	if items[0].Quantity != 10 {
		t.Errorf("expected qty 10, got %v", items[0].Quantity)
	}
	if items[0].LocationDetail == nil || items[0].LocationDetail.PK != locs[1].PK {
		t.Errorf("expected location %d (%s), got %+v", locs[1].PK, locs[1].PathString, items[0].LocationDetail)
	}
	t.Logf("stock at %q qty %v", items[0].LocationPath(), items[0].Quantity)

	if err := c.DeleteStock(items[0].PK); err != nil {
		t.Fatalf("delete stock: %v", err)
	}
}
