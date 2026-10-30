package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLowStockAndLocalCreate(t *testing.T) {
	c := testStore(t)

	// nested bin path creates its parents
	loc, err := c.CreateLocation("Garage/Shelf 2/Box A")
	if err != nil || loc.PathString != "Garage/Shelf 2/Box A" {
		t.Fatalf("CreateLocation: %+v %v", loc, err)
	}
	locs, _ := c.FetchLocations()
	if len(locs) != 6 { // 2 seeded workshop bins + Workshop + Garage, Shelf 2, Box A
		t.Fatalf("want 6 locations, got %d", len(locs))
	}

	batt, _ := c.CreatePart(map[string]any{"name": "AA batteries", "minimum_stock": 4.0})
	screw, _ := c.CreatePart(map[string]any{"name": "M3 screws", "minimum_stock": 10.0})
	if _, err := c.CreateStock(batt.PK, 1, loc.PK); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStock(screw.PK, 50, loc.PK); err != nil {
		t.Fatal(err)
	}

	// data survives a reopen
	c2 := NewClient(c.path)
	if err := c2.Login(); err != nil {
		t.Fatal(err)
	}
	a := NewApp(c2, 0)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(a.loadAllCmd()())

	low := a.lowParts()
	if len(low) != 1 || low[0].Name != "AA batteries" {
		t.Fatalf("want only AA batteries low, got %+v", low)
	}
	if !strings.Contains(a.status, "1 item low: AA batteries (1/4)") {
		t.Fatalf("startup alert missing, status = %q", a.status)
	}

	a.showLowStock()
	if len(a.search.results) != 1 {
		t.Fatalf("is:low should show 1 result, got %d", len(a.search.results))
	}
	if !strings.Contains(a.View(), "1 low") {
		t.Fatal("search header should show the low count")
	}

	// removing stock below the minimum makes screws low too
	stock, _ := c2.FetchStockForPart(screw.PK)
	if err := c2.StockRemove(stock[0].PK, 45); err != nil {
		t.Fatal(err)
	}
	if err := c2.StockRemove(stock[0].PK, 99); err == nil {
		t.Fatal("removing more than is in stock should fail")
	}
	a.Update(a.loadAllCmd()())
	if n := len(a.lowParts()); n != 2 {
		t.Fatalf("want 2 low after removal, got %d", n)
	}
}
