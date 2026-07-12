package main

// Console font switching (F2 smaller / F3 bigger). The TUI runs on the Linux
// framebuffer console, where text size is a console property, not an escape
// code -- so we shell out to setfont(8) with the Terminus fonts that ship
// with console-setup. The chosen size is persisted and re-applied at startup,
// so the kiosk keeps its size across reboots. No-op off the Linux console.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Terminus sizes shipped by console-setup, ascending.
var fontSizes = []string{"12x6", "14", "16", "18x10", "20x10", "22x11", "24x12", "28x14", "32x16"}

const defaultFontIdx = 2 // "16", the usual console default

type fontCtl struct {
	tty   string
	paths []string // resolved font file per size, "" if not installed
	idx   int
	saved bool // a saved preference exists (re-apply at startup)
}

func fontStatePath() string {
	return filepath.Join(filepath.Dir(journalPath()), "font")
}

func newFontCtl() *fontCtl {
	f := &fontCtl{idx: defaultFontIdx}
	if t, err := os.Readlink("/proc/self/fd/1"); err == nil && strings.HasPrefix(t, "/dev/tty") {
		f.tty = t
	}
	for _, sz := range fontSizes {
		f.paths = append(f.paths, findConsoleFont(sz))
	}
	if b, err := os.ReadFile(fontStatePath()); err == nil {
		want := strings.TrimSpace(string(b))
		for i, sz := range fontSizes {
			if sz == want {
				f.idx, f.saved = i, true
				break
			}
		}
	}
	return f
}

// findConsoleFont picks a regular-weight Terminus font file for a size,
// preferring broader unicode coverage.
func findConsoleFont(size string) string {
	for _, prefix := range []string{"Uni3", "Uni2", "Lat15", "*"} {
		matches, _ := filepath.Glob("/usr/share/consolefonts/" + prefix + "-Terminus" + size + ".psf.gz")
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

// applySaved restores the persisted font size at startup (best-effort).
func (f *fontCtl) applySaved() {
	if f.saved && f.tty != "" {
		f.apply()
	}
}

func (f *fontCtl) apply() error {
	return f.setIdx(f.idx, true)
}

// setIdx switches to fontSizes[i]; persist=false is used for temporary
// switches (the help overlay) that shouldn't overwrite the saved preference.
func (f *fontCtl) setIdx(i int, persist bool) error {
	if f.tty == "" {
		return errors.New("font size only works on the Linux console")
	}
	p := f.paths[i]
	if p == "" {
		return fmt.Errorf("font size %s not installed", fontSizes[i])
	}
	if out, err := exec.Command("setfont", "-C", f.tty, p).CombinedOutput(); err != nil {
		return fmt.Errorf("setfont: %v: %s", err, strings.TrimSpace(string(out)))
	}
	f.idx = i
	if persist {
		os.MkdirAll(filepath.Dir(fontStatePath()), 0o755)
		os.WriteFile(fontStatePath(), []byte(fontSizes[i]), 0o644)
	}
	return nil
}

// change steps to the next installed size in the given direction.
func (f *fontCtl) change(delta int) error {
	i := f.idx
	for {
		i += delta
		if i < 0 || i >= len(fontSizes) {
			return errors.New("no more sizes that way")
		}
		if f.paths[i] != "" {
			return f.setIdx(i, true)
		}
	}
}
