package main

// Tap-vs-drag discrimination for the touchscreen. The evdev reader feeds
// finger positions (already scaled to terminal cells) through this state
// machine, which emits bubbletea mouse messages:
//
//   - press: immediately emits a left-click so the row under the finger is
//     selected right away; this anchors the scroll to where you touched.
//   - drag: one wheel event per cell of vertical movement, live while the
//     finger moves. No tap fires on release after a drag.
//   - zone scroll: when the finger rests in the top or bottom third of the
//     screen during a drag, zone() returns a continuous scroll direction so
//     the caller can auto-repeat without the finger needing to keep moving.
//   - tap (release without meaningful movement): emits a second click on the
//     start cell, which opens the already-selected row (single-tap-to-open).

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const tapSlop = 1 // max cells of movement for a touch to still count as a tap

type touchGesture struct {
	down           bool
	startX, startY int
	lastY          int // last Y we emitted scroll for
	curX, curY     int // most recent finger position
	maxMove        int // max cell distance from start during this touch
}

// ZoneScroll returns the direction and repeat interval for auto-scroll when
// the finger is held still in the top or bottom portion of the screen.
// The further from center, the shorter the interval (faster scroll).
// Returns dir=0 when in the neutral middle zone.
func (g *touchGesture) ZoneScroll(rows int) (dir int, interval time.Duration) {
	if !g.down || g.maxMove <= tapSlop || rows < 3 {
		return 0, 0
	}
	center := rows / 2
	distFromCenter := abs(g.curY - center)
	// Neutral deadzone covers the middle half of the screen
	deadzone := center / 2
	if distFromCenter < deadzone {
		return 0, 0
	}
	if g.curY < center {
		dir = -1
	} else {
		dir = 1
	}
	// Scale interval: 400ms at the deadzone edge → 60ms at the screen edge
	active := float64(distFromCenter-deadzone) / float64(center-deadzone)
	if active > 1 {
		active = 1
	}
	ms := 400 - int(active*340) // 400→60ms
	if ms < 60 {
		ms = 60
	}
	return dir, time.Duration(ms) * time.Millisecond
}

func (g *touchGesture) press(x, y int) []tea.Msg {
	g.down = true
	g.startX, g.startY = x, y
	g.lastY = y
	g.maxMove = 0
	// Immediately select the row under the finger so dragging feels anchored
	// to the touch point. If the gesture turns out to be a tap, the release
	// emits a second click which opens the (now-selected) row.
	return []tea.Msg{
		tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	}
}

func (g *touchGesture) move(x, y int) []tea.Msg {
	if !g.down {
		return nil
	}
	g.curX, g.curY = x, y
	g.maxMove = max(g.maxMove, max(abs(x-g.startX), abs(y-g.startY)))
	var msgs []tea.Msg
	// Shift marks these as touch-drag scrolls: handlers scroll 1 line per
	// event (content tracks the finger 1:1) instead of 3 for a wheel notch.
	for y < g.lastY { // finger moving up
		g.lastY--
		msgs = append(msgs, tea.MouseMsg{X: x, Y: y, Shift: true,
			Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	}
	for y > g.lastY { // finger moving down
		g.lastY++
		msgs = append(msgs, tea.MouseMsg{X: x, Y: y, Shift: true,
			Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	}
	return msgs
}

func (g *touchGesture) release() []tea.Msg {
	if !g.down {
		return nil
	}
	g.down = false
	if g.maxMove > tapSlop {
		return nil // drag — selection already moved on press, don't also open
	}
	// Tap: row was already selected on press; a second click on the same row
	// opens it (single-tap-to-open on touchscreen).
	return []tea.Msg{
		tea.MouseMsg{X: g.startX, Y: g.startY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
		tea.MouseMsg{X: g.startX, Y: g.startY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
