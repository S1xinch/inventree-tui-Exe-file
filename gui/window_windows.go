package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

// openWindow shows the app in a chromeless Edge (or Chrome) window with its
// own profile, so it looks like a normal program and the launch blocks until
// that window is closed. Returns true if it waited for the window.
func openWindow(url string, wait bool) bool {
	profile := filepath.Join(os.Getenv("LOCALAPPDATA"), "InventoryApp", "window")
	args := []string{
		"--app=" + url,
		"--user-data-dir=" + profile,
		"--window-size=1180,780",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
	}
	for _, exe := range browserPaths() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe, args...)
		if !wait {
			return cmd.Start() == nil
		}
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	// no Edge/Chrome found: fall back to the default browser
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	return false
}

func browserPaths() []string {
	var out []string
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		out = append(out,
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
	}
	return out
}

// fatal shows an error box (there is no console to print to) and exits.
func fatal(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	user32 := syscall.NewLazyDLL("user32.dll")
	box := user32.NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("Inventory")
	const mbIconError = 0x10
	box.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
	os.Exit(1)
}
