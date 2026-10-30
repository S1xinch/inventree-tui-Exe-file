// Inventory: the windowed version of inv-tui.
//
// Everything is stored in inventory.json next to the program (the same file
// the terminal version uses). On Windows the interface runs inside the
// program's own window using the WebView2 component built into Windows, and
// the page talks to the Go code directly: no server, no port, no network.
//
// On macOS/Linux (development only) it serves the page on 127.0.0.1 instead
// so it can be opened and tested in a normal browser.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	dataFlag := flag.String("data", "", "inventory JSON file (default: inventory.json next to the program)")
	portFlag := flag.Int("port", 47615, "development only: local port for the browser version")
	noWindow := flag.Bool("no-window", false, "development only: don't open a browser")
	flag.Parse()

	dataPath := *dataFlag
	if dataPath == "" {
		dataPath = DefaultDataPath()
	}
	store := NewClient(dataPath)
	if err := store.Login(); err != nil {
		fatal("Can't open the inventory file:\n%s\n\n%v", dataPath, err)
	}
	run(newServer(store), *portFlag, *noWindow)
}

// call runs one API request in memory, straight through the same handlers
// the development server uses. This is how the Windows window talks to the
// program: no sockets involved.
func (s *server) call(method, path, body string) (string, error) {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = "127.0.0.1:0"
	req.Header.Set("X-Inventory", "1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		var e struct{ Error string }
		if json.Unmarshal(rec.Body.Bytes(), &e) == nil && e.Error != "" {
			return "", errors.New(e.Error)
		}
		return "", errors.New(strings.TrimSpace(rec.Body.String()))
	}
	return rec.Body.String(), nil
}

// ---- server ----

type server struct {
	store *Client
	mux   http.Handler

	mu       sync.Mutex
	lastPing time.Time
	byeAt    time.Time // last time a window said it was closing
	modTime  time.Time
}

func newServer(store *Client) *server {
	s := &server{store: store, lastPing: time.Now()}
	s.modTime = s.fileModTime()
	s.mux = s.routes()
	return s
}

// waitUntilClosed returns when the app window has gone: shortly after it
// says goodbye (unless it pings again, e.g. after a reload), or after a long
// silence. The silence limit is generous because Windows slows timers in
// minimised windows to about once a minute.
func (s *server) waitUntilClosed() {
	const (
		byeGrace   = 4 * time.Second
		maxSilence = 3 * time.Minute
	)
	for {
		time.Sleep(time.Second)
		s.mu.Lock()
		ping, bye := s.lastPing, s.byeAt
		s.mu.Unlock()
		if !bye.IsZero() && bye.After(ping) && time.Since(bye) > byeGrace {
			return
		}
		if time.Since(ping) > maxSilence {
			return
		}
	}
}

func (s *server) fileModTime() time.Time {
	if fi, err := os.Stat(s.store.path); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// syncFromDisk reloads the file if something else (the terminal version, or
// a restored backup) changed it, so we never write over newer data.
func (s *server) syncFromDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mt := s.fileModTime()
	if mt.Equal(s.modTime) {
		return nil
	}
	if err := s.store.Reload(); err != nil {
		return err
	}
	s.modTime = s.fileModTime()
	return nil
}

func (s *server) markWritten() {
	s.mu.Lock()
	s.modTime = s.fileModTime()
	s.mu.Unlock()
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(web)))

	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.lastPing = time.Now()
		s.mu.Unlock()
		writeJSON(w, map[string]string{"app": "inventory"})
	})
	mux.HandleFunc("POST /api/bye", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.byeAt = time.Now()
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/state", s.handle(func(r *http.Request) (any, error) {
		return s.state()
	}))

	// parts
	mux.HandleFunc("POST /api/parts", s.handle(func(r *http.Request) (any, error) {
		var body struct {
			Fields   map[string]any `json:"fields"`
			Qty      float64        `json:"qty"`
			Location int            `json:"location"`
		}
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		p, err := s.store.CreatePart(body.Fields)
		if err != nil {
			return nil, err
		}
		if body.Qty > 0 {
			if _, err := s.store.CreateStock(p.PK, body.Qty, body.Location); err != nil {
				return nil, err
			}
		}
		return p, nil
	}))
	mux.HandleFunc("PUT /api/parts/{id}", s.handle(func(r *http.Request) (any, error) {
		var fields map[string]any
		if err := decode(r, &fields); err != nil {
			return nil, err
		}
		return s.store.UpdatePart(pathID(r), fields)
	}))
	mux.HandleFunc("DELETE /api/parts/{id}", s.handle(func(r *http.Request) (any, error) {
		return nil, s.store.DeletePart(pathID(r))
	}))

	// stock
	mux.HandleFunc("POST /api/stock", s.handle(func(r *http.Request) (any, error) {
		var body struct {
			Part     int     `json:"part"`
			Qty      float64 `json:"qty"`
			Location int     `json:"location"`
		}
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		return s.store.CreateStock(body.Part, body.Qty, body.Location)
	}))
	mux.HandleFunc("POST /api/stock/{id}/{op}", s.handle(func(r *http.Request) (any, error) {
		var body struct {
			Qty      float64 `json:"qty"`
			Location int     `json:"location"`
		}
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		id := pathID(r)
		switch r.PathValue("op") {
		case "add":
			return nil, s.store.StockAdd(id, body.Qty)
		case "remove":
			return nil, s.store.StockRemove(id, body.Qty)
		case "count":
			return nil, s.store.StockCount(id, body.Qty)
		case "move":
			return nil, s.store.StockTransfer(id, body.Qty, body.Location)
		}
		return nil, errors.New("unknown stock action")
	}))
	mux.HandleFunc("DELETE /api/stock/{id}", s.handle(func(r *http.Request) (any, error) {
		return nil, s.store.DeleteStock(pathID(r))
	}))

	// bins and categories
	mux.HandleFunc("POST /api/locations", s.handle(func(r *http.Request) (any, error) {
		var body struct{ Path string }
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		return s.store.CreateLocation(body.Path)
	}))
	mux.HandleFunc("POST /api/categories", s.handle(func(r *http.Request) (any, error) {
		var body struct{ Path string }
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		return s.store.CreateCategory(body.Path)
	}))
	return mux
}

type state struct {
	File       string      `json:"file"`
	Parts      []Part      `json:"parts"`
	Stock      []StockItem `json:"stock"`
	Locations  []Location  `json:"locations"`
	Categories []Category  `json:"categories"`
}

func (s *server) state() (state, error) {
	st := state{File: s.store.path}
	var err error
	if st.Parts, err = s.store.FetchParts(); err != nil {
		return st, err
	}
	if st.Stock, err = s.store.FetchStock(); err != nil {
		return st, err
	}
	if st.Locations, err = s.store.FetchLocations(); err != nil {
		return st, err
	}
	st.Categories, err = s.store.FetchCategories()
	return st, err
}

// handle wraps an API call: only accept requests from our own page, pick up
// outside edits first, and answer with JSON.
func (s *server) handle(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := s.syncFromDisk(); err != nil {
			writeErr(w, err)
			return
		}
		out, err := fn(r)
		if r.Method != http.MethodGet {
			s.markWritten()
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		if out == nil {
			out = map[string]bool{"ok": true}
		}
		writeJSON(w, out)
	}
}

// sameOrigin blocks other websites open in a browser from poking the API.
func sameOrigin(r *http.Request) bool {
	if !strings.HasPrefix(r.Host, "127.0.0.1:") && !strings.HasPrefix(r.Host, "localhost:") {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		return false
	}
	return r.Method == http.MethodGet || r.Header.Get("X-Inventory") == "1"
}

func pathID(r *http.Request) int {
	n, _ := strconv.Atoi(r.PathValue("id"))
	return n
}

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
