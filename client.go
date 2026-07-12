package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client is a minimal InvenTree REST API client (stdlib only).
type Client struct {
	BaseURL  string
	Username string
	Password string
	token    string
	http     *http.Client
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Username: username,
		Password: password,
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

// Login exchanges username/password for an API token (avoids the slow
// Django password hash check on every request).
func (c *Client) Login() error {
	var resp struct {
		Token string `json:"token"`
	}
	if err := c.do("GET", "/api/user/token/", nil, nil, &resp); err != nil {
		return err
	}
	c.token = resp.Token
	return nil
}

func (c *Client) do(method, path string, query url.Values, body any, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Token "+c.token)
	} else {
		req.SetBasicAuth(c.Username, c.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, apiErrorSummary(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// apiErrorSummary flattens a DRF error body ({"field": ["msg"], ...}) into one line.
func apiErrorSummary(data []byte) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		var parts []string
		for k, v := range m {
			parts = append(parts, fmt.Sprintf("%s: %v", k, v))
		}
		sort.Strings(parts)
		s := strings.Join(parts, "; ")
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	s := string(data)
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// ---- Models ----

type Part struct {
	PK           int     `json:"pk"`
	Name         string  `json:"name"`
	IPN          string  `json:"IPN"`
	Description  string  `json:"description"`
	Keywords     string  `json:"keywords"`
	Category     int     `json:"category"`
	CategoryName string  `json:"category_name"`
	TotalInStock float64 `json:"total_in_stock"`
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
}

type StockItem struct {
	PK             int       `json:"pk"`
	Part           int       `json:"part"`
	Quantity       float64   `json:"quantity"`
	Location       *int      `json:"location"`
	LocationDetail *Location `json:"location_detail"`
	Batch          string    `json:"batch"`
	Notes          string    `json:"notes"`
	StatusText     string    `json:"status_text"`
}

// ---- Paginated fetchers ----

func fetchAll[T any](c *Client, path string, query url.Values) ([]T, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", "200")
	var all []T
	offset := 0
	for {
		query.Set("offset", fmt.Sprint(offset))
		var page struct {
			Count   int             `json:"count"`
			Results json.RawMessage `json:"results"`
		}
		if err := c.do("GET", path, query, nil, &page); err != nil {
			return nil, err
		}
		var batch []T
		if err := json.Unmarshal(page.Results, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		offset += len(batch)
		if offset >= page.Count || len(batch) == 0 {
			return all, nil
		}
	}
}

func (c *Client) FetchParts() ([]Part, error) {
	return fetchAll[Part](c, "/api/part/", nil)
}

func (c *Client) FetchStock() ([]StockItem, error) {
	q := url.Values{}
	// location_detail is the key to seeing which bin an item lives in --
	// the bare stock record only carries the location pk.
	q.Set("location_detail", "true")
	q.Set("in_stock", "true")
	return fetchAll[StockItem](c, "/api/stock/", q)
}

func (c *Client) FetchLocations() ([]Location, error) {
	return fetchAll[Location](c, "/api/stock/location/", nil)
}

func (c *Client) FetchCategories() ([]Category, error) {
	return fetchAll[Category](c, "/api/part/category/", nil)
}

func (c *Client) FetchPart(pk int) (Part, error) {
	var p Part
	err := c.do("GET", fmt.Sprintf("/api/part/%d/", pk), nil, nil, &p)
	return p, err
}

func (c *Client) FetchStockForPart(pk int) ([]StockItem, error) {
	q := url.Values{}
	q.Set("part", fmt.Sprint(pk))
	q.Set("location_detail", "true")
	q.Set("in_stock", "true")
	return fetchAll[StockItem](c, "/api/stock/", q)
}

// ---- Mutations ----

func (c *Client) CreatePart(fields map[string]any) (Part, error) {
	var p Part
	err := c.do("POST", "/api/part/", nil, fields, &p)
	return p, err
}

func (c *Client) UpdatePart(pk int, fields map[string]any) (Part, error) {
	var p Part
	err := c.do("PATCH", fmt.Sprintf("/api/part/%d/", pk), nil, fields, &p)
	return p, err
}

// DeletePart deactivates the part first (InvenTree refuses to delete an
// active part), then deletes it.
func (c *Client) DeletePart(pk int) error {
	if _, err := c.UpdatePart(pk, map[string]any{"active": false}); err != nil {
		return err
	}
	return c.do("DELETE", fmt.Sprintf("/api/part/%d/", pk), nil, nil, nil)
}

func (c *Client) CreateStock(part int, quantity float64, location int) (StockItem, error) {
	body := map[string]any{"part": part, "quantity": quantity}
	if location > 0 {
		body["location"] = location
	}
	// The response is a single object, or an array when serial numbers are
	// involved -- accept both.
	var raw json.RawMessage
	if err := c.do("POST", "/api/stock/", nil, body, &raw); err != nil {
		return StockItem{}, err
	}
	var s StockItem
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var many []StockItem
	if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 {
		return many[0], nil
	}
	return StockItem{}, fmt.Errorf("unexpected stock create response: %s", truncate(string(raw), 200))
}

func (c *Client) DeleteStock(pk int) error {
	return c.do("DELETE", fmt.Sprintf("/api/stock/%d/", pk), nil, nil, nil)
}

type stockOp struct {
	PK       int     `json:"pk"`
	Quantity float64 `json:"quantity"`
}

func (c *Client) StockAdd(pk int, qty float64) error {
	return c.do("POST", "/api/stock/add/", nil,
		map[string]any{"items": []stockOp{{pk, qty}}}, nil)
}

func (c *Client) StockRemove(pk int, qty float64) error {
	return c.do("POST", "/api/stock/remove/", nil,
		map[string]any{"items": []stockOp{{pk, qty}}}, nil)
}

func (c *Client) StockCount(pk int, qty float64) error {
	return c.do("POST", "/api/stock/count/", nil,
		map[string]any{"items": []stockOp{{pk, qty}}}, nil)
}

func (c *Client) StockTransfer(pk int, qty float64, location int) error {
	return c.do("POST", "/api/stock/transfer/", nil,
		map[string]any{"items": []stockOp{{pk, qty}}, "location": location}, nil)
}
