package buzz

// Minimal pigpiod socket client (localhost:8888) -- just enough to build and
// transmit DMA waveforms for the kiosk buzzer on GPIO17. Waveforms are timed
// by DMA, so tones stay pitch-perfect regardless of CPU load, and playback
// costs the caller nothing.

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	piCmdModes = 0
	piCmdWVCLR = 27
	piCmdWVAG  = 28
	piCmdWVHLT = 33
	piCmdWVCRE = 49
	piCmdWVDEL = 50
	piCmdWVTX  = 51
)

type PigpioConn struct {
	conn net.Conn
}

func DialPigpiod(addr string) (*PigpioConn, error) {
	if addr == "" {
		addr = "localhost:8888" // pigpiod may bind v4 or v6 loopback
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &PigpioConn{conn: conn}, nil
}

func (p *PigpioConn) Close() error { return p.conn.Close() }

func (p *PigpioConn) cmd(cmd, p1, p2 uint32, ext []byte) (int32, error) {
	buf := make([]byte, 16+len(ext))
	binary.LittleEndian.PutUint32(buf[0:], cmd)
	binary.LittleEndian.PutUint32(buf[4:], p1)
	binary.LittleEndian.PutUint32(buf[8:], p2)
	binary.LittleEndian.PutUint32(buf[12:], uint32(len(ext)))
	copy(buf[16:], ext)
	if _, err := p.conn.Write(buf); err != nil {
		return 0, err
	}
	var resp [16]byte
	if _, err := readFull(p.conn, resp[:]); err != nil {
		return 0, err
	}
	res := int32(binary.LittleEndian.Uint32(resp[12:]))
	if res < 0 {
		return res, fmt.Errorf("pigpiod error %d (cmd %d)", res, cmd)
	}
	return res, nil
}

func readFull(c net.Conn, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

// pulse mirrors pigpio's gpioPulse_t.
type pulse struct {
	on, off, usDelay uint32
}

// Segment is one piece of a sound: a (possibly swept) square tone, or
// silence when Freq0 == Freq1 == 0.
type Segment struct {
	Freq0, Freq1 float64
	Dur          time.Duration
	Duty         float64 // 0..1, 0 means default 0.5
}

// segmentPulses renders a segment into wave pulses for the given pin.
func segmentPulses(pin uint, seg Segment) []pulse {
	mask := uint32(1) << pin
	if seg.Duty == 0 {
		seg.Duty = 0.5
	}
	if seg.Freq0 == 0 && seg.Freq1 == 0 {
		return []pulse{{0, mask, uint32(seg.Dur.Microseconds())}}
	}
	var pulses []pulse
	elapsed := 0.0 // microseconds
	total := float64(seg.Dur.Microseconds())
	for elapsed < total {
		f := seg.Freq0 + (seg.Freq1-seg.Freq0)*(elapsed/total)
		if f < 20 {
			f = 20
		}
		period := 1e6 / f
		high := uint32(period * seg.Duty)
		low := uint32(period) - high
		pulses = append(pulses, pulse{mask, 0, high}, pulse{0, mask, low})
		elapsed += period
	}
	pulses = append(pulses, pulse{0, mask, 0}) // end low
	return pulses
}

// Play renders the segments as one DMA waveform on the pin and transmits it.
// Returns once transmission has started (playback is hardware-timed).
func (p *PigpioConn) Play(pin uint, segs []Segment) error {
	if _, err := p.cmd(piCmdModes, uint32(pin), 1, nil); err != nil { // output
		return err
	}
	if _, err := p.cmd(piCmdWVCLR, 0, 0, nil); err != nil {
		return err
	}
	var pulses []pulse
	for _, seg := range segs {
		pulses = append(pulses, segmentPulses(pin, seg)...)
	}
	ext := make([]byte, 12*len(pulses))
	for i, pl := range pulses {
		binary.LittleEndian.PutUint32(ext[i*12:], pl.on)
		binary.LittleEndian.PutUint32(ext[i*12+4:], pl.off)
		binary.LittleEndian.PutUint32(ext[i*12+8:], pl.usDelay)
	}
	if _, err := p.cmd(piCmdWVAG, 0, 0, ext); err != nil {
		return err
	}
	id, err := p.cmd(piCmdWVCRE, 0, 0, nil)
	if err != nil {
		return err
	}
	_, err = p.cmd(piCmdWVTX, uint32(id), 0, nil)
	return err
}

// ParseSegments parses "FREQ[-FREQ][:MS[:DUTY]]" tokens (freq 0 = rest).
func ParseSegments(tokens []string) ([]Segment, error) {
	var segs []Segment
	for _, tok := range tokens {
		seg := Segment{Dur: 200 * time.Millisecond, Duty: 0.5}
		parts := strings.Split(tok, ":")
		if len(parts) > 3 {
			return nil, fmt.Errorf("too many fields in %q", tok)
		}
		freq := parts[0]
		if from, to, ok := strings.Cut(freq, "-"); ok {
			a, err1 := strconv.ParseFloat(from, 64)
			b, err2 := strconv.ParseFloat(to, 64)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad sweep %q", freq)
			}
			seg.Freq0, seg.Freq1 = a, b
		} else {
			a, err := strconv.ParseFloat(freq, 64)
			if err != nil {
				return nil, fmt.Errorf("bad frequency %q", freq)
			}
			seg.Freq0, seg.Freq1 = a, a
		}
		if len(parts) >= 2 {
			ms, err := strconv.Atoi(parts[1])
			if err != nil || ms < 0 {
				return nil, fmt.Errorf("bad duration %q", parts[1])
			}
			seg.Dur = time.Duration(ms) * time.Millisecond
		}
		if len(parts) == 3 {
			d, err := strconv.Atoi(parts[2])
			if err != nil || d < 1 || d > 99 {
				return nil, fmt.Errorf("bad duty %q (1-99)", parts[2])
			}
			seg.Duty = float64(d) / 100
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// Buzzer is a non-blocking wrapper: callers fire-and-forget via Play(name).
// A background goroutine serializes waveform transmissions; if a new sound
// arrives while the channel is full, it's dropped (no build-up).
// Tap sounds are rate-limited so rapid scroll/keystroke events produce a
// pleasant clicking rhythm rather than an overwhelming burst.
type Buzzer struct {
	conn    *PigpioConn
	pin     uint
	ch      chan []Segment
	mu      sync.Mutex
	lastTap time.Time
	muted   bool
}

const tapInterval = 60 * time.Millisecond

func NewBuzzer(pin uint) *Buzzer {
	conn, err := DialPigpiod("")
	if err != nil {
		return nil // no pigpiod; all Play() calls become no-ops
	}
	b := &Buzzer{conn: conn, pin: pin, ch: make(chan []Segment, 4)}
	go func() {
		for segs := range b.ch {
			conn.Play(pin, segs) // DMA starts and returns immediately
		}
	}()
	return b
}

// ToggleMute flips the mute state and returns the new state.
func (b *Buzzer) ToggleMute() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.muted = !b.muted
	return b.muted
}

// Muted reports whether the buzzer is currently muted.
func (b *Buzzer) Muted() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.muted
}

// Play plays a named sound effect. Safe to call from any goroutine; no-op if
// the Buzzer is nil (pigpiod not available).
func (b *Buzzer) Play(name string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	muted := b.muted
	b.mu.Unlock()
	if muted {
		return
	}
	segs, ok := SoundEffects[name]
	if !ok {
		return
	}
	// rate-limit tap/key sounds so rapid scroll/typing doesn't flood
	if name == "tap" || name == "key" {
		b.mu.Lock()
		since := time.Since(b.lastTap)
		if since < tapInterval {
			b.mu.Unlock()
			return
		}
		b.lastTap = time.Now()
		b.mu.Unlock()
	}
	select {
	case b.ch <- segs:
	default: // busy — drop
	}
}

// SoundEffects are the named kiosk sounds (also playable via `pibeep NAME`).
var SoundEffects = map[string][]Segment{
	"tap":     mustSegs("4000:18"),
	"key":     mustSegs("3500:12"),
	"ok":      mustSegs("1047:60 0:15 1568:90"),
	"error":   mustSegs("220:110 0:60 196:170"),
	"undo":    mustSegs("1568:60 0:15 1047:90"),
	"redo":    mustSegs("1047:50 0:10 1319:50 0:10 1568:80"),
	"startup": mustSegs("659:80 880:80 1319:150"),
	"hermes":  mustSegs("1175:70 0:20 1760:110"),
}

func mustSegs(s string) []Segment {
	segs, err := ParseSegments(strings.Fields(s))
	if err != nil {
		panic(err)
	}
	return segs
}
