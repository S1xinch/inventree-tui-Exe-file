package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func mouseOf(t *testing.T, m tea.Msg) tea.MouseMsg {
	t.Helper()
	mm, ok := m.(tea.MouseMsg)
	if !ok {
		t.Fatalf("expected MouseMsg, got %T", m)
	}
	return mm
}

func TestTouchTap(t *testing.T) {
	g := &touchGesture{}
	// press immediately selects the row (1 click event)
	pressMsgs := g.press(10, 5)
	if len(pressMsgs) != 1 {
		t.Fatalf("press should emit 1 selection click, got %d", len(pressMsgs))
	}
	sel := mouseOf(t, pressMsgs[0])
	if sel.Action != tea.MouseActionPress || sel.Button != tea.MouseButtonLeft || sel.X != 10 || sel.Y != 5 {
		t.Errorf("bad press-select: %+v", sel)
	}
	if msgs := g.move(10, 5); len(msgs) != 0 {
		t.Fatalf("no-movement move should emit nothing")
	}
	// release after a tap emits a second click to open the selected row
	msgs := g.release()
	if len(msgs) != 2 {
		t.Fatalf("tap release should emit press+release to open, got %d msgs", len(msgs))
	}
	p, r := mouseOf(t, msgs[0]), mouseOf(t, msgs[1])
	if p.Action != tea.MouseActionPress || p.Button != tea.MouseButtonLeft {
		t.Errorf("bad tap open press: %+v", p)
	}
	if r.Action != tea.MouseActionRelease {
		t.Errorf("bad tap release: %+v", r)
	}
}

func TestTouchJitterStillTap(t *testing.T) {
	g := &touchGesture{}
	g.press(10, 5)
	g.move(11, 5) // 1 cell of jitter is within the slop
	g.move(10, 6)
	if msgs := g.release(); len(msgs) != 2 {
		t.Fatalf("jittery tap should still open, got %d msgs", len(msgs))
	}
}

func TestTouchDragScrolls(t *testing.T) {
	g := &touchGesture{}
	if msgs := g.press(10, 20); len(msgs) != 1 {
		t.Fatalf("press should emit 1 selection click, got %d", len(msgs))
	}
	// drag finger up 5 cells -> 5 wheel-up events
	var wheels []tea.MouseMsg
	for _, m := range g.move(10, 15) {
		wheels = append(wheels, mouseOf(t, m))
	}
	if len(wheels) != 5 {
		t.Fatalf("expected 5 scroll events, got %d", len(wheels))
	}
	for _, w := range wheels {
		if w.Button != tea.MouseButtonWheelUp || !w.Shift {
			t.Errorf("expected shift+wheel-up, got %+v", w)
		}
	}
	// drag back down 2 cells -> 2 wheel-down events
	downs := g.move(10, 17)
	if len(downs) != 2 || mouseOf(t, downs[0]).Button != tea.MouseButtonWheelDown {
		t.Fatalf("expected 2 wheel-down events, got %v", downs)
	}
	// a drag must not also click on release
	if msgs := g.release(); len(msgs) != 0 {
		t.Fatalf("drag release should not click, got %v", msgs)
	}
}

func TestTouchZoneScroll(t *testing.T) {
	g := &touchGesture{}
	// no zone while not dragging
	g.press(5, 5)
	if dir, _ := g.ZoneScroll(30); dir != 0 {
		t.Error("no zone until drag starts")
	}
	// simulate a real drag so maxMove > tapSlop
	g.move(5, 10)
	g.move(5, 20) // 15 cells from start, clearly a drag

	// rows=30, center=15, deadzone=7 (center/2)
	// finger at Y=2 (distFromCenter=13, > deadzone=7) → dir -1
	g.curY = 2
	if dir, interval := g.ZoneScroll(30); dir != -1 || interval == 0 {
		t.Errorf("near top edge should scroll up fast, got dir=%d interval=%v", dir, interval)
	}
	// finger at Y=28 → dir +1
	g.curY = 28
	if dir, interval := g.ZoneScroll(30); dir != 1 || interval == 0 {
		t.Errorf("near bottom edge should scroll down fast, got dir=%d interval=%v", dir, interval)
	}
	// finger in neutral middle (Y=15, deadzone covers center±7 = 8..22)
	g.curY = 15
	if dir, _ := g.ZoneScroll(30); dir != 0 {
		t.Errorf("middle should be neutral (dir=0), got %d", dir)
	}
	// speed: closer to edge = shorter interval.
	// rows=30, center=15, deadzone=7: active zone is Y<8 and Y>22.
	// Y=2 (dist=13): near edge → fast. Y=8 (dist=7): just outside deadzone → slow.
	g.curY = 2
	_, fast := g.ZoneScroll(30)
	g.curY = 8
	_, slow := g.ZoneScroll(30)
	if fast >= slow {
		t.Errorf("edge should be faster than near-deadzone: fast=%v slow=%v", fast, slow)
	}
}

func TestTouchScrollStep(t *testing.T) {
	touch := tea.MouseMsg{Shift: true, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	wheel := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	if scrollStep(touch) != 1 || scrollStep(wheel) != 3 {
		t.Fatalf("touch drags scroll 1 line, wheel notches 3")
	}
}

func TestStatusExpires(t *testing.T) {
	a := testApp()
	a.setStatus("no more sizes that way", true)
	a.Update(statusTickMsg{})
	if a.status == "" {
		t.Fatal("fresh status should survive a tick")
	}
	a.statusAt = a.statusAt.Add(-statusErrTTL - time.Second)
	a.Update(statusTickMsg{})
	if a.status != "" {
		t.Fatalf("expired status should clear, got %q", a.status)
	}
	// info messages expire on the shorter TTL
	a.setStatus("loaded 421 parts", false)
	a.statusAt = a.statusAt.Add(-statusTTL - time.Second)
	a.Update(statusTickMsg{})
	if a.status != "" {
		t.Fatalf("expired info status should clear, got %q", a.status)
	}
}

func TestHelpOverlay(t *testing.T) {
	a := testApp()
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.mode != modeHelp {
		t.Fatalf("F1 should open help, mode=%v", a.mode)
	}
	v := a.View()
	for _, want := range []string{"Shortcuts", "undo / redo", "drag", "text size"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
	// the whole page must fit the screen: at 100x30 it renders two columns
	if got := len(strings.Split(strings.TrimRight(v, "\n"), "\n")); got > 30 {
		t.Errorf("help overflows the screen: %d lines", got)
	}
	// and in a narrow window it falls back to one column, still bounded
	a.width, a.height = 60, 40
	v = a.View()
	if got := len(strings.Split(strings.TrimRight(v, "\n"), "\n")); got > 40 {
		t.Errorf("narrow help overflows: %d lines", got)
	}
	if !strings.Contains(v, "Part detail") {
		t.Errorf("narrow help missing detail section")
	}
	a.width, a.height = 100, 30
	key(a, "x") // any key closes
	if a.mode != modeSearch {
		t.Fatalf("any key should close help, mode=%v", a.mode)
	}
	// works from detail too, and returns there
	key(a, "enter")
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	key(a, "q")
	if a.mode != modeDetail {
		t.Fatalf("help should return to detail, mode=%v", a.mode)
	}
}
