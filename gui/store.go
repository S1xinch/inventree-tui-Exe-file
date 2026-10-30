package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Client is the local, file-backed inventory store (same file format as the
// terminal version in the repo root, so both can share one inventory.json).
type Client struct {
	BaseURL string // path of the data file (shown on the loading screen)
	path    string

	mu sync.Mutex
	db storeDB
}

type storeDB struct {
	NextID     int         `json:"next_id"`
	Parts      []Part      `json:"parts"`
	Stock      []StockItem `json:"stock"`
	Locations  []Location  `json:"locations"`
	Categories []Category  `json:"categories"`
}

// NewClient opens (or creates) the inventory file at path.
func NewClient(path string) *Client {
	return &Client{BaseURL: path, path: path}
}

// DefaultDataPath puts inventory.json next to the executable, so the data
// travels with the program and is easy to find and back up.
func DefaultDataPath() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return filepath.Join(filepath.Dir(exe), "inventory.json")
	}
	return "inventory.json"
}

// Login loads the data file. A missing file starts an empty inventory.
func (c *Client) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		c.db = storeDB{NextID: 1}
		return c.saveLocked()
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &c.db); err != nil {
		return fmt.Errorf("%s is not valid inventory JSON: %w", c.path, err)
	}
	if c.db.NextID < 1 {
		c.db.NextID = 1
	}
	return nil
}

// saveLocked writes atomically: temp file, then rename, so a crash mid-write
// never leaves a half-written inventory behind.
func (c *Client) saveLocked() error {
	data, err := json.MarshalIndent(c.db, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(c.path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Client) newID() int {
	id := c.db.NextID
	c.db.NextID++
	return id
}

// ---- Models ----

type Part struct {
	PK           int     `json:"pk"`
	Name         string  `json:"name"`
	IPN          string  `json:"IPN"`
	Description  string  `json:"description"`
	Keywords     string  `json:"keywords"`
	Category     int     `json:"category"`
	CategoryName string  `json:"category_name,omitempty"`
	TotalInStock float64 `json:"total_in_stock,omitempty"`
	MinimumStock float64 `json:"minimum_stock"`
	Units        string  `json:"units"`
	Active       bool    `json:"active"`
	Link         string  `json:"link"`
}

type Location struct {
	PK         int    `json:"pk"`
	Name       string `json:"name"`
	PathString string `json:"pathstring"`
	Parent     *int   `json:"parent"`
}

type Category struct {
	PK         int    `json:"pk"`
	Name       string `json:"name"`
	PathString string `json:"pathstring"`
	Parent     *int   `json:"parent,omitempty"`
}

type StockItem struct {
	PK             int       `json:"pk"`
	Part           int       `json:"part"`
	Quantity       float64   `json:"quantity"`
	Location       *int      `json:"location"`
	LocationDetail *Location `json:"location_detail,omitempty"`
	Batch          string    `json:"batch"`
	Notes          string    `json:"notes"`
	StatusText     string    `json:"status_text,omitempty"`
}

// ---- Lookups (call with mu held) ----

func (c *Client) partIdx(pk int) int {
	for i := range c.db.Parts {
		if c.db.Parts[i].PK == pk {
			return i
		}
	}
	return -1
}

func (c *Client) stockIdx(pk int) int {
	for i := range c.db.Stock {
		if c.db.Stock[i].PK == pk {
			return i
		}
	}
	return -1
}

func (c *Client) location(pk int) *Location {
	for i := range c.db.Locations {
		if c.db.Locations[i].PK == pk {
			l := c.db.Locations[i]
			return &l
		}
	}
	return nil
}

func (c *Client) categoryName(pk int) string {
	for _, cat := range c.db.Categories {
		if cat.PK == pk {
			return cat.PathString
		}
	}
	return ""
}

// decoratePart fills in the derived fields the server used to compute.
func (c *Client) decoratePart(p Part) Part {
	p.CategoryName = c.categoryName(p.Category)
	p.TotalInStock = 0
	for _, s := range c.db.Stock {
		if s.Part == p.PK && s.Quantity > 0 {
			p.TotalInStock += s.Quantity
		}
	}
	return p
}

func (c *Client) decorateStock(s StockItem) StockItem {
	s.LocationDetail = nil
	if s.Location != nil {
		s.LocationDetail = c.location(*s.Location)
	}
	s.StatusText = "OK"
	return s
}

// ---- Fetchers ----

func (c *Client) FetchParts() ([]Part, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Part, 0, len(c.db.Parts))
	for _, p := range c.db.Parts {
		out = append(out, c.decoratePart(p))
	}
	return out, nil
}

func (c *Client) FetchStock() ([]StockItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []StockItem
	for _, s := range c.db.Stock {
		if s.Quantity > 0 {
			out = append(out, c.decorateStock(s))
		}
	}
	return out, nil
}

func (c *Client) FetchLocations() ([]Location, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Location(nil), c.db.Locations...), nil
}

func (c *Client) FetchCategories() ([]Category, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Category(nil), c.db.Categories...), nil
}

func (c *Client) FetchPart(pk int) (Part, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.partIdx(pk)
	if i < 0 {
		return Part{}, fmt.Errorf("part %d not found", pk)
	}
	return c.decoratePart(c.db.Parts[i]), nil
}

func (c *Client) FetchStockForPart(pk int) ([]StockItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []StockItem
	for _, s := range c.db.Stock {
		if s.Part == pk && s.Quantity > 0 {
			out = append(out, c.decorateStock(s))
		}
	}
	return out, nil
}

// ---- Part mutations ----

// applyPartFields copies the editable fields the form/journal send.
func applyPartFields(p *Part, fields map[string]any) error {
	for k, v := range fields {
		switch k {
		case "name":
			p.Name = asString(v)
		case "IPN":
			p.IPN = asString(v)
		case "description":
			p.Description = asString(v)
		case "keywords":
			p.Keywords = asString(v)
		case "units":
			p.Units = asString(v)
		case "link":
			p.Link = asString(v)
		case "category":
			p.Category = int(asFloat(v))
		case "minimum_stock":
			p.MinimumStock = asFloat(v)
		case "active":
			if b, ok := v.(bool); ok {
				p.Active = b
			}
		}
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name: this field may not be blank")
	}
	return nil
}

func (c *Client) CreatePart(fields map[string]any) (Part, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := Part{Active: true}
	if err := applyPartFields(&p, fields); err != nil {
		return Part{}, err
	}
	p.PK = c.newID()
	c.db.Parts = append(c.db.Parts, p)
	if err := c.saveLocked(); err != nil {
		return Part{}, err
	}
	return c.decoratePart(p), nil
}

func (c *Client) UpdatePart(pk int, fields map[string]any) (Part, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.partIdx(pk)
	if i < 0 {
		return Part{}, fmt.Errorf("part %d not found", pk)
	}
	p := c.db.Parts[i]
	if err := applyPartFields(&p, fields); err != nil {
		return Part{}, err
	}
	c.db.Parts[i] = p
	if err := c.saveLocked(); err != nil {
		return Part{}, err
	}
	return c.decoratePart(p), nil
}

// DeletePart removes the part and all of its stock.
func (c *Client) DeletePart(pk int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.partIdx(pk)
	if i < 0 {
		return fmt.Errorf("part %d not found", pk)
	}
	c.db.Parts = append(c.db.Parts[:i], c.db.Parts[i+1:]...)
	kept := c.db.Stock[:0]
	for _, s := range c.db.Stock {
		if s.Part != pk {
			kept = append(kept, s)
		}
	}
	c.db.Stock = kept
	return c.saveLocked()
}

// ---- Stock mutations ----

func (c *Client) CreateStock(part int, quantity float64, location int) (StockItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.partIdx(part) < 0 {
		return StockItem{}, fmt.Errorf("part %d not found", part)
	}
	if quantity < 0 {
		return StockItem{}, errors.New("quantity must be zero or more")
	}
	s := StockItem{PK: c.newID(), Part: part, Quantity: quantity}
	if location > 0 {
		if c.location(location) == nil {
			return StockItem{}, fmt.Errorf("bin %d not found", location)
		}
		loc := location
		s.Location = &loc
	}
	c.db.Stock = append(c.db.Stock, s)
	if err := c.saveLocked(); err != nil {
		return StockItem{}, err
	}
	return c.decorateStock(s), nil
}

func (c *Client) DeleteStock(pk int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.stockIdx(pk)
	if i < 0 {
		return fmt.Errorf("stock item %d not found", pk)
	}
	c.db.Stock = append(c.db.Stock[:i], c.db.Stock[i+1:]...)
	return c.saveLocked()
}

func (c *Client) setQty(pk int, f func(old float64) (float64, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.stockIdx(pk)
	if i < 0 {
		return fmt.Errorf("stock item %d not found", pk)
	}
	q, err := f(c.db.Stock[i].Quantity)
	if err != nil {
		return err
	}
	c.db.Stock[i].Quantity = q
	return c.saveLocked()
}

func (c *Client) StockAdd(pk int, qty float64) error {
	return c.setQty(pk, func(old float64) (float64, error) {
		if qty <= 0 {
			return 0, errors.New("quantity must be greater than zero")
		}
		return old + qty, nil
	})
}

func (c *Client) StockRemove(pk int, qty float64) error {
	return c.setQty(pk, func(old float64) (float64, error) {
		if qty <= 0 {
			return 0, errors.New("quantity must be greater than zero")
		}
		if qty > old {
			return 0, fmt.Errorf("can't remove %s, only %s in stock", fmtQty(qty), fmtQty(old))
		}
		return old - qty, nil
	})
}

func (c *Client) StockCount(pk int, qty float64) error {
	return c.setQty(pk, func(float64) (float64, error) {
		if qty < 0 {
			return 0, errors.New("count can't be negative")
		}
		return qty, nil
	})
}

// StockTransfer moves qty to another bin. Moving everything relocates the
// item; moving part of it splits off a new stock item, like InvenTree does.
func (c *Client) StockTransfer(pk int, qty float64, location int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.stockIdx(pk)
	if i < 0 {
		return fmt.Errorf("stock item %d not found", pk)
	}
	var dest *int
	if location > 0 {
		if c.location(location) == nil {
			return fmt.Errorf("bin %d not found", location)
		}
		loc := location
		dest = &loc
	}
	s := c.db.Stock[i]
	if qty <= 0 || qty > s.Quantity {
		return fmt.Errorf("can't move %s, only %s in stock", fmtQty(qty), fmtQty(s.Quantity))
	}
	if qty == s.Quantity {
		c.db.Stock[i].Location = dest
	} else {
		c.db.Stock[i].Quantity -= qty
		c.db.Stock = append(c.db.Stock, StockItem{PK: c.newID(), Part: s.Part, Quantity: qty, Location: dest})
	}
	return c.saveLocked()
}

// ---- Bins and categories ----

// CreateLocation creates a bin from a path like "Garage/Shelf 2/Box A",
// creating any missing parent bins along the way. Returns the deepest one.
func (c *Client) CreateLocation(path string) (Location, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := splitPath(path)
	if len(names) == 0 {
		return Location{}, errors.New("bin name can't be blank")
	}
	var parent *int
	var cur Location
	for depth, name := range names {
		full := strings.Join(names[:depth+1], "/")
		found := false
		for _, l := range c.db.Locations {
			if strings.EqualFold(l.PathString, full) {
				cur, found = l, true
				break
			}
		}
		if !found {
			cur = Location{PK: c.newID(), Name: name, PathString: full, Parent: parent}
			c.db.Locations = append(c.db.Locations, cur)
		}
		pk := cur.PK
		parent = &pk
	}
	sort.Slice(c.db.Locations, func(i, j int) bool {
		return c.db.Locations[i].PathString < c.db.Locations[j].PathString
	})
	return cur, c.saveLocked()
}

// CreateCategory works like CreateLocation: "Electronics/Resistors".
func (c *Client) CreateCategory(path string) (Category, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := splitPath(path)
	if len(names) == 0 {
		return Category{}, errors.New("category name can't be blank")
	}
	var parent *int
	var cur Category
	for depth, name := range names {
		full := strings.Join(names[:depth+1], "/")
		found := false
		for _, cat := range c.db.Categories {
			if strings.EqualFold(cat.PathString, full) {
				cur, found = cat, true
				break
			}
		}
		if !found {
			cur = Category{PK: c.newID(), Name: name, PathString: full, Parent: parent}
			c.db.Categories = append(c.db.Categories, cur)
		}
		pk := cur.PK
		parent = &pk
	}
	sort.Slice(c.db.Categories, func(i, j int) bool {
		return c.db.Categories[i].PathString < c.db.Categories[j].PathString
	})
	return cur, c.saveLocked()
}

func splitPath(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---- small helpers ----

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		fmt.Sscan(strings.TrimSpace(n), &f)
		return f
	}
	return 0
}

func fmtQty(q float64) string {
	if q == float64(int64(q)) {
		return fmt.Sprintf("%d", int64(q))
	}
	return fmt.Sprintf("%.2f", q)
}

// Reload re-reads the file so edits made by the terminal version show up.
func (c *Client) Reload() error { return c.Login() }
