// pibeep: play buzzer sound effects / tones through pigpiod.
//
//	pibeep ok                     named effect (tap ok error undo redo startup hermes)
//	pibeep 880:300                880 Hz for 300 ms
//	pibeep 200-2000:800           sweep
//	pibeep 660:100 880:100 0:50 1100:200:30   sequence with rest and 30% duty
//	pibeep -pin 17 ...
//
// Effects render as single pigpio DMA waveforms: exact pitch, zero CPU during
// playback. Names and tone segments can be mixed freely.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/AndrewYatzkan/inventree-tui/buzz"
)

func main() {
	pin := flag.Uint("pin", 17, "BCM GPIO pin number")
	flag.Parse()
	if flag.NArg() == 0 {
		var names []string
		for n := range buzz.SoundEffects {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(os.Stderr, "usage: pibeep [-pin 17] EFFECT|FREQ[-FREQ][:MS[:DUTY]] ...\n  effects: %v\n", names)
		os.Exit(2)
	}

	var segs []buzz.Segment
	for _, arg := range flag.Args() {
		if fx, ok := buzz.SoundEffects[arg]; ok {
			segs = append(segs, fx...)
			segs = append(segs, buzz.Segment{Dur: 250 * time.Millisecond}) // gap between effects
			continue
		}
		s, err := buzz.ParseSegments([]string{arg})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(2)
		}
		segs = append(segs, s...)
	}

	conn, err := buzz.DialPigpiod("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pigpiod: %v (is the daemon running?)\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	if err := conn.Play(*pin, segs); err != nil {
		fmt.Fprintf(os.Stderr, "play: %v\n", err)
		os.Exit(1)
	}
}
