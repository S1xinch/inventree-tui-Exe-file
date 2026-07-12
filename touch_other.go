//go:build !linux

package main

import tea "github.com/charmbracelet/bubbletea"

// Touch input is only supported on Linux (evdev); no-op elsewhere.
func startTouchInput(prog *tea.Program, device string) {}
