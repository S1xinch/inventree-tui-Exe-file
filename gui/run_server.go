//go:build !windows || server

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// run (local-server version): serve the page on 127.0.0.1 only (never on
// the network) and open it in a window. The app quits a few seconds after
// the window closes. If it's already running, just open another window.
func run(srv *server, port int, noWindow bool) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "http://" + addr + "/"
	if alreadyRunning(addr) {
		if !noWindow {
			openWindow(url)
		}
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("Can't listen on %s: %v", addr, err)
	}
	go func() { log.Println(http.Serve(ln, srv.mux)) }()
	if noWindow {
		select {}
	}
	openWindow(url)
	srv.waitUntilClosed()
}

func alreadyRunning(addr string) bool {
	c := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := c.Get("http://" + addr + "/api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body map[string]string
	return json.NewDecoder(resp.Body).Decode(&body) == nil && body["app"] == "inventory"
}
