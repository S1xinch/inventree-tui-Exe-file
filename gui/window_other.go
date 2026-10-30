//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// openWindow opens the page in the default browser (macOS/Linux are for
// development; the Windows build gets a real app window).
func openWindow(url string, wait bool) bool {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	exec.Command(opener, url).Start()
	return false
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
