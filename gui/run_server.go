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
	if running, same := runningCopy(addr); running {
		if same {
			// the same version is already open: just show another window
			if !noWindow {
				openWindow(url)
			}
			return
		}
		// an older (or different) version is still running: replace it
		stopCopy(addr)
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

// runningCopy reports whether Inventory is already serving on addr, and
// whether it is this exact version.
func runningCopy(addr string) (running, sameVersion bool) {
	c := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := c.Get("http://" + addr + "/api/ping")
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var body map[string]string
	if json.NewDecoder(resp.Body).Decode(&body) != nil || body["app"] != "inventory" {
		return false, false
	}
	return true, body["version"] == version
}

// stopCopy asks the running copy to quit and waits for the port to free up.
func stopCopy(addr string) {
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/quit", nil)
	req.Header.Set("X-Inventory", "1")
	c := http.Client{Timeout: 2 * time.Second}
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
	for i := 0; i < 40; i++ {
		if ln, err := net.Listen("tcp", addr); err == nil {
			ln.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
