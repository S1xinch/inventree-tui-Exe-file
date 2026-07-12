package main

// Hermes is the home agent (OpenAI-compatible chat completions API) with its
// own InvenTree access. The TUI hands off "advanced" queries to it: ctrl+h
// from search opens a chat view, carrying over the current query.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type HermesClient struct {
	URL   string // base, e.g. http://192.168.4.244:8642
	Key   string
	Model string
	http  *http.Client
}

func NewHermesClient(url, key, model string) *HermesClient {
	if model == "" {
		model = "hermes-agent"
	}
	return &HermesClient{
		URL:   strings.TrimRight(url, "/"),
		Key:   key,
		Model: model,
		// the agent may run tools server-side; give it plenty of time
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (h *HermesClient) Ask(messages []chatMsg) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":    h.Model,
		"messages": messages,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", h.URL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+h.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message chatMsg `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("hermes: %s: %s", resp.Status, truncate(string(data), 200))
	}
	if out.Error != nil {
		return "", fmt.Errorf("hermes: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("hermes: empty response")
	}
	return out.Choices[0].Message.Content, nil
}

// ---------- Hermes chat view ----------

type HermesView struct {
	input      textinput.Model
	transcript []chatMsg
	scroll     int
	busy       bool
	tick       int
}

type hermesReplyMsg struct {
	content string
	err     error
}

type hermesTickMsg struct{}

func (a *App) openHermes(prefill string) {
	if a.hermesView.input.Value() == "" && prefill != "" {
		a.hermesView.input.SetValue(prefill)
		a.hermesView.input.CursorEnd()
	}
	a.hermesView.input.Prompt = "> "
	a.hermesView.input.Placeholder = "ask hermes anything about the inventory..."
	a.hermesView.input.CharLimit = 500
	a.hermesView.input.Focus()
	a.mode = modeHermes
	a.setStatus("", false)
}

func hermesTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return hermesTickMsg{} })
}

func (a *App) updateHermes(msg tea.Msg) (tea.Model, tea.Cmd) {
	hv := &a.hermesView
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			a.mode = modeSearch
			return a, nil
		case "enter":
			q := strings.TrimSpace(hv.input.Value())
			if q == "" || hv.busy {
				return a, nil
			}
			hv.transcript = append(hv.transcript, chatMsg{Role: "user", Content: q})
			hv.input.SetValue("")
			hv.busy = true
			hv.tick = 0
			hv.scroll = 1 << 30 // stick to bottom
			history := append([]chatMsg(nil), hv.transcript...)
			h := a.hermes
			ask := func() tea.Msg {
				content, err := h.Ask(history)
				return hermesReplyMsg{content: content, err: err}
			}
			return a, tea.Batch(ask, hermesTick())
		case "up":
			hv.scroll -= 1
		case "down":
			hv.scroll += 1
		case "pgup":
			hv.scroll -= a.hermesListHeight()
		case "pgdown":
			hv.scroll += a.hermesListHeight()
		case "ctrl+x":
			hv.transcript = nil
			hv.scroll = 0
		default:
			var cmd tea.Cmd
			hv.input, cmd = hv.input.Update(msg)
			return a, cmd
		}
		return a, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				hv.scroll -= scrollStep(msg)
			case tea.MouseButtonWheelDown:
				hv.scroll += scrollStep(msg)
			case tea.MouseButtonLeft:
				if msg.Y == 0 {
					a.mode = modeSearch
				}
			}
		}
		return a, nil

	case hermesTickMsg:
		if hv.busy {
			hv.tick++
			return a, hermesTick()
		}
		return a, nil
	}

	var cmd tea.Cmd
	hv.input, cmd = hv.input.Update(msg)
	return a, cmd
}

// handleHermesReply is called from the root Update so replies land even if
// the user navigated away while hermes was thinking.
func (a *App) handleHermesReply(msg hermesReplyMsg) {
	hv := &a.hermesView
	hv.busy = false
	if msg.err != nil {
		hv.transcript = append(hv.transcript, chatMsg{Role: "assistant", Content: "error: " + msg.err.Error()})
	} else {
		hv.transcript = append(hv.transcript, chatMsg{Role: "assistant", Content: msg.content})
	}
	hv.scroll = 1 << 30
	if a.mode != modeHermes {
		a.setStatus("hermes replied -- ctrl+h to view", false)
	}
}

func (a *App) hermesListHeight() int {
	return max(1, a.height-3) // title, input, bottom bar
}

func (a *App) viewHermes() string {
	hv := &a.hermesView
	var b strings.Builder
	b.WriteString(styTitle.Render(truncate("← Hermes", a.width)) + "\n")

	// build wrapped transcript lines
	var lines []string
	for _, m := range hv.transcript {
		prefix, body := "you", m.Content
		if m.Role == "assistant" {
			prefix = "hermes"
			body = strings.ReplaceAll(body, "**", "")
		}
		for _, para := range strings.Split(body, "\n") {
			wrapped := wrap(para, a.width-9)
			if len(wrapped) == 0 {
				wrapped = []string{""}
			}
			for _, l := range wrapped {
				lines = append(lines, styDim.Render(padRight(prefix, 7))+" "+l)
				prefix = ""
			}
		}
		lines = append(lines, "")
	}
	if hv.busy {
		dots := strings.Repeat(".", hv.tick%4)
		lines = append(lines, styBin.Render(fmt.Sprintf("hermes is thinking%s (%ds)", dots, hv.tick/2)))
	}

	h := a.hermesListHeight()
	maxScroll := max(0, len(lines)-h)
	if hv.scroll > maxScroll {
		hv.scroll = maxScroll
	}
	if hv.scroll < 0 {
		hv.scroll = 0
	}
	for row := 0; row < h; row++ {
		idx := hv.scroll + row
		if idx < len(lines) {
			b.WriteString(truncate(lines[idx], a.width) + "\n")
		} else {
			b.WriteString("\n")
		}
	}

	hv.input.Width = max(10, a.width-4)
	b.WriteString(truncate(hv.input.View(), a.width) + "\n")
	b.WriteString(a.bottomBar("enter:send  ↑↓/pgup/pgdn:scroll  ctrl+x:clear chat  esc:back"))
	return b.String()
}
