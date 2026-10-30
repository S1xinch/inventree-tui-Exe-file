package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The Windows window talks to the program only through srv.call.
func TestDirectCalls(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "inventory.json"))
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	srv := newServer(c)

	out, err := srv.call("POST", "/api/locations", `{"path":"Garage/Shelf 2"}`)
	if err != nil {
		t.Fatal(err)
	}
	var loc Location
	json.Unmarshal([]byte(out), &loc)

	if _, err := srv.call("POST", "/api/parts", `{"fields":{"name":"AA batteries","minimum_stock":4},"qty":1,"location":`+itoa(loc.PK)+`}`); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.call("POST", "/api/parts", `{"fields":{"name":""}}`); err == nil || !strings.Contains(err.Error(), "blank") {
		t.Fatalf("blank name should fail with a readable error, got %v", err)
	}
	out, err = srv.call("GET", "/api/state", "")
	if err != nil {
		t.Fatal(err)
	}
	var st state
	json.Unmarshal([]byte(out), &st)
	if len(st.Parts) != 1 || st.Parts[0].TotalInStock != 1 || len(st.Locations) != 2 {
		t.Fatalf("unexpected state: %+v", st)
	}
	stockPK := st.Stock[0].PK
	if _, err := srv.call("POST", "/api/stock/"+itoa(stockPK)+"/remove", `{"qty":5}`); err == nil {
		t.Fatal("removing more than in stock should fail")
	}
	if _, err := srv.call("POST", "/api/stock/"+itoa(stockPK)+"/add", `{"qty":3}`); err != nil {
		t.Fatal(err)
	}
	out, _ = srv.call("GET", "/api/state", "")
	json.Unmarshal([]byte(out), &st)
	if st.Parts[0].TotalInStock != 4 {
		t.Fatalf("want 4 in stock, got %v", st.Parts[0].TotalInStock)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
