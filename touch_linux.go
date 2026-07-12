//go:build linux

package main

// Touchscreen support for the Linux framebuffer console, where the terminal
// itself has no idea a touchscreen exists. We read raw evdev events from
// /dev/input/eventN, scale the absolute touch coordinates to terminal cells,
// and inject them into bubbletea as mouse clicks.
//
// Best-effort and experimental: if anything fails we silently fall back to
// keyboard-only operation.

import (
	"encoding/binary"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"unsafe"

	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	evKey    = 0x01
	evAbs    = 0x03
	absX     = 0x00
	absY     = 0x01
	absMTX   = 0x35 // ABS_MT_POSITION_X
	absMTY   = 0x36 // ABS_MT_POSITION_Y
	btnTouch = 0x14a
)

type absInfo struct {
	Value, Minimum, Maximum, Fuzz, Flat, Resolution int32
}

func eviocgabs(axis int) uint {
	// EVIOCGABS(abs) = _IOR('E', 0x40 + abs, struct input_absinfo)
	const iocRead = 2
	size := uint(unsafe.Sizeof(absInfo{}))
	return (iocRead << 30) | (size << 16) | ('E' << 8) | uint(0x40+axis)
}

func getAbs(fd int, axis int) (absInfo, error) {
	var ai absInfo
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(eviocgabs(axis)), uintptr(unsafe.Pointer(&ai)))
	if errno != 0 {
		return ai, errno
	}
	return ai, nil
}

// hasBtnTouch reports whether the device advertises the BTN_TOUCH key --
// the discriminator between touchscreens and regular mice/trackpads.
func hasBtnTouch(fd int) bool {
	// EVIOCGBIT(EV_KEY, len) = _IOC(_IOC_READ, 'E', 0x20 + EV_KEY, len)
	var bits [96]byte // covers KEY_MAX (0x2ff) bits
	const iocRead = 2
	req := uint(iocRead<<30) | uint(len(bits))<<16 | ('E' << 8) | uint(0x20+evKey)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&bits[0])))
	if errno != 0 {
		return false
	}
	return bits[btnTouch/8]&(1<<(btnTouch%8)) != 0
}

// findTouchDevice scans /dev/input/event* for a device that reports BTN_TOUCH
// plus absolute X/Y with a sane range (i.e. a touchscreen).
func findTouchDevice() string {
	devs, _ := filepath.Glob("/dev/input/event*")
	sort.Strings(devs)
	for _, dev := range devs {
		f, err := os.Open(dev)
		if err != nil {
			continue
		}
		fd := int(f.Fd())
		ok := hasBtnTouch(fd)
		if ok {
			ai, err := getAbs(fd, absX)
			if err != nil || ai.Maximum <= ai.Minimum {
				ai, err = getAbs(fd, absMTX)
				ok = err == nil && ai.Maximum > ai.Minimum
			}
		}
		f.Close()
		if ok {
			return dev
		}
	}
	return ""
}

// startTouchInput launches a goroutine feeding touch taps into the program.
func startTouchInput(prog *tea.Program, device string) {
	if device == "off" {
		return
	}
	go func() {
		dev := device
		if dev == "auto" || dev == "" {
			dev = findTouchDevice()
			if dev == "" {
				return
			}
		}
		f, err := os.Open(dev)
		if err != nil {
			return
		}
		defer f.Close()
		fd := int(f.Fd())

		// Prefer multitouch axes if present; fall back to plain ABS_X/Y.
		xAxis, yAxis := absMTX, absMTY
		xi, err := getAbs(fd, xAxis)
		if err != nil || xi.Maximum <= xi.Minimum {
			xAxis, yAxis = absX, absY
			xi, err = getAbs(fd, xAxis)
			if err != nil || xi.Maximum <= xi.Minimum {
				return
			}
		}
		yi, err := getAbs(fd, yAxis)
		if err != nil || yi.Maximum <= yi.Minimum {
			return
		}

		// struct input_event: struct timeval (2 longs) + u16 type + u16 code + s32 value.
		// long is 4 bytes on linux/arm (GOARM=6), 8 on arm64.
		wordSize := bits.UintSize / 8
		evSize := 2*wordSize + 8
		buf := make([]byte, evSize)

		g := &touchGesture{}
		send := func(msgs []tea.Msg) {
			for _, m := range msgs {
				prog.Send(m)
			}
		}
		// Zone auto-scroll: while the finger rests outside the neutral middle
		// zone, fire scroll events at a rate proportional to distance from
		// center (far from center = fast; near center = slow or none).
		go func() {
			for {
				_, rows := termCells()
				dir, interval := g.ZoneScroll(rows)
				if dir == 0 {
					time.Sleep(30 * time.Millisecond)
					continue
				}
				time.Sleep(interval)
				// re-check after sleeping — finger may have lifted or moved
				_, rows = termCells()
				if d, _ := g.ZoneScroll(rows); d == dir {
					btn := tea.MouseButtonWheelDown
					if dir < 0 {
						btn = tea.MouseButtonWheelUp
					}
					prog.Send(tea.MouseMsg{X: g.curX, Y: g.curY, Shift: true,
						Action: tea.MouseActionPress, Button: btn})
				}
			}
		}()
		curX, curY := -1, -1
		pendingPress := false // BTN_TOUCH arrived before the first position
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			etype := binary.LittleEndian.Uint16(buf[2*wordSize:])
			code := binary.LittleEndian.Uint16(buf[2*wordSize+2:])
			value := int32(binary.LittleEndian.Uint32(buf[2*wordSize+4:]))

			cols, rows := termCells()
			switch {
			case etype == evAbs && (int(code) == xAxis || int(code) == yAxis):
				if int(code) == xAxis {
					curX = scale(int(value), int(xi.Minimum), int(xi.Maximum), cols)
				} else {
					curY = scale(int(value), int(yi.Minimum), int(yi.Maximum), rows)
				}
				if curX < 0 || curY < 0 {
					break
				}
				if pendingPress {
					pendingPress = false
					send(g.press(curX, curY))
				} else {
					send(g.move(curX, curY))
				}
			case etype == evKey && code == btnTouch:
				if value == 1 {
					if curX >= 0 && curY >= 0 {
						send(g.press(curX, curY))
					} else {
						pendingPress = true
					}
				} else {
					pendingPress = false
					send(g.release())
				}
			}
		}
	}()
}

func scale(v, lo, hi, cells int) int {
	if hi <= lo || cells <= 0 {
		return 0
	}
	c := (v - lo) * cells / (hi - lo + 1)
	if c < 0 {
		c = 0
	}
	if c >= cells {
		c = cells - 1
	}
	return c
}

func termCells() (cols, rows int) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		return 80, 24
	}
	return cols, rows
}
