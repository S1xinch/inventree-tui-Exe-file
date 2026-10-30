package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Config is read from (in increasing priority): /etc/inv-tui.env,
// ~/.config/inv-tui/env, environment variables, command-line flags.
// File format is plain KEY=VALUE lines (see .env.example).
type Config struct {
	DataFile  string // inventory JSON file (default: inventory.json next to the program)
	Touch     string // touch input device: "auto", "off", or /dev/input/eventN
	BuzzerPin int    // GPIO pin for the pigpiod buzzer (default 17; 0 = disable)

	// optional hermes home-agent handoff (ctrl+h); disabled when unset
	HermesURL   string
	HermesKey   string
	HermesModel string
}

func loadEnvFile(path string, into map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			into[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
}

func loadConfig() Config {
	vars := map[string]string{}
	loadEnvFile("/etc/inv-tui.env", vars)
	if home, err := os.UserHomeDir(); err == nil {
		loadEnvFile(filepath.Join(home, ".config", "inv-tui", "env"), vars)
	}
	for _, k := range []string{"INV_TUI_DATA",
		"INV_TUI_TOUCH", "INV_TUI_BUZZER_PIN", "HERMES_URL", "HERMES_KEY", "HERMES_MODEL"} {
		if v := os.Getenv(k); v != "" {
			vars[k] = v
		}
	}
	buzzerPin := 17
	if v := vars["INV_TUI_BUZZER_PIN"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			buzzerPin = n
		}
	}
	cfg := Config{
		DataFile:    vars["INV_TUI_DATA"],
		Touch:       vars["INV_TUI_TOUCH"],
		BuzzerPin:   buzzerPin,
		HermesURL:   vars["HERMES_URL"],
		HermesKey:   vars["HERMES_KEY"],
		HermesModel: vars["HERMES_MODEL"],
	}
	if cfg.DataFile == "" {
		cfg.DataFile = DefaultDataPath()
	}
	if cfg.Touch == "" {
		cfg.Touch = "auto"
	}
	return cfg
}

func main() {
	cfg := loadConfig()
	dataFlag := flag.String("data", cfg.DataFile, "inventory JSON file")
	touchFlag := flag.String("touch", cfg.Touch, "touchscreen evdev device: auto, off, or /dev/input/eventN")
	check := flag.Bool("check", false, "load the inventory file, print a summary, and exit")
	logN := flag.Int("log", 0, "print the last N journal entries and exit")
	undoID := flag.Int("undo", 0, "undo the journal entry with this ID (see -log) and exit")
	flag.Parse()

	cfg.DataFile, cfg.Touch = *dataFlag, *touchFlag

	if *logN > 0 {
		printJournal(*logN)
		return
	}

	client := NewClient(cfg.DataFile)
	if err := client.Login(); err != nil {
		fmt.Fprintf(os.Stderr, "can't open inventory file: %v\n", err)
		pauseIfDoubleClicked()
		os.Exit(1)
	}

	if *check {
		runCheck(client)
		if cfg.HermesURL != "" && cfg.HermesKey != "" {
			fmt.Printf("hermes: configured (%s)\n", cfg.HermesURL)
		} else {
			fmt.Println("hermes: disabled")
		}
		return
	}

	if *undoID != 0 {
		runUndo(client, *undoID)
		return
	}

	app := NewApp(client, uint(cfg.BuzzerPin))
	if cfg.HermesURL != "" && cfg.HermesKey != "" {
		app.hermes = NewHermesClient(cfg.HermesURL, cfg.HermesKey, cfg.HermesModel)
	}
	app.fonts.applySaved() // restore persisted console font size (best-effort)
	prog := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	startTouchInput(prog, cfg.Touch)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		pauseIfDoubleClicked()
		os.Exit(1)
	}
}

func runCheck(c *Client) {
	parts, err := c.FetchParts()
	fatalIf(err)
	stock, err := c.FetchStock()
	fatalIf(err)
	locs, err := c.FetchLocations()
	fatalIf(err)
	cats, err := c.FetchCategories()
	fatalIf(err)
	fmt.Printf("file: %s\n", c.BaseURL)
	fmt.Printf("ok: %d parts, %d stock items, %d locations, %d categories\n",
		len(parts), len(stock), len(locs), len(cats))
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printJournal(n int) {
	entries, err := openJournal().ReadAll()
	fatalIf(err)
	if len(entries) == 0 {
		fmt.Println("journal is empty (" + journalPath() + ")")
		return
	}
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	for _, e := range entries {
		fmt.Printf("#%-4d %s  %-14s %s\n", e.ID, e.Time, e.Action, e.Summary)
	}
}

func runUndo(c *Client, id int) {
	j := openJournal()
	entries, err := j.ReadAll()
	fatalIf(err)
	for i := range entries {
		if entries[i].ID == id {
			summary, err := j.Reverse(c, &entries[i])
			fatalIf(err)
			verb := "undid"
			if entries[i].UndoOf != 0 {
				verb = "redid"
			}
			fmt.Printf("%s #%d (%s): %s\n", verb, id, entries[i].Summary, summary)
			return
		}
	}
	fatalIf(fmt.Errorf("no journal entry #%d (see -log)", id))
}

// pauseIfDoubleClicked keeps an error on screen long enough to read when the
// program was launched by double-click (its console closes on exit).
func pauseIfDoubleClicked() {
	fmt.Fprintln(os.Stderr, "\npress Enter to close")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
