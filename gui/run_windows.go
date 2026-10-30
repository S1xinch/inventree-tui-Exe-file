//go:build windows && !server

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	webview2 "github.com/jchv/go-webview2"
)

// run opens the program's own window with the interface inside it. The page
// calls window.invApi(method, path, body), which goes straight to srv.call.
func run(srv *server, _ int, _ bool) {
	dataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "InventoryApp", "WebView2")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataDir,
		WindowOptions: webview2.WindowOptions{
			Title:  "Inventory",
			Width:  1180,
			Height: 780,
			IconId: 1, // the app icon embedded by go-winres (gui/winres/winres.json)
			Center: true,
		},
	})
	if w == nil {
		fatal("Inventory needs the Microsoft Edge WebView2 Runtime, which comes with Windows 10 and 11.\n\n" +
			"It seems to be missing. Install it free from:\nhttps://go.microsoft.com/fwlink/p/?LinkId=2124703")
	}
	defer w.Destroy()
	if err := w.Bind("invApi", srv.call); err != nil {
		fatal("Couldn't start the window: %v", err)
	}
	html, err := pageHTML()
	if err != nil {
		fatal("Couldn't load the interface: %v", err)
	}
	w.SetHtml(html)
	w.Run()
}

// pageHTML returns index.html with the icon inlined, since a page loaded
// from memory has no address to fetch icon.png from.
func pageHTML() (string, error) {
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return "", err
	}
	icon, err := webFS.ReadFile("web/icon.png")
	if err != nil {
		return "", err
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(icon)
	return strings.ReplaceAll(string(page), `"icon.png"`, `"`+dataURI+`"`), nil
}
