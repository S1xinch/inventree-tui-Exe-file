//go:build !windows

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

// run (development): serve the page on 127.0.0.1 and open a browser. The
// app quits a few seconds after the page closes.
func run(srv *server, port int, noWindow bool) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("Can't listen on %s: %v", addr, err)
	}
	go func() { log.Println(http.Serve(ln, srv.mux)) }()
	url := "http://" + addr + "/"
	if noWindow {
		select {}
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	exec.Command(opener, url).Start()
	srv.waitUntilClosed()
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
