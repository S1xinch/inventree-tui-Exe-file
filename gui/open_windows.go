//go:build windows && server

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// openWindow shows the page in a chromeless Edge (or Chrome) app window with
// its own profile so it looks like a normal program. Falls back to the
// default browser.
func openWindow(url string) {
	profile := filepath.Join(os.Getenv("LOCALAPPDATA"), "InventoryApp", "ServerWindow")
	args := []string{"--app=" + url, "--user-data-dir=" + profile, "--window-size=1180,780",
		"--no-first-run", "--no-default-browser-check", "--disable-features=Translate"}
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		for _, exe := range []string{
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
		} {
			if _, err := os.Stat(exe); err == nil {
				cmd := exec.Command(exe, args...)
				if cmd.Start() == nil {
					go cmd.Wait()
					return
				}
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
