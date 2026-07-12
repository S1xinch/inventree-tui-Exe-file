package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHermesClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k123" {
			t.Errorf("unexpected auth header %q", got)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Bin 57"}}]}`))
	}))
	defer srv.Close()

	h := NewHermesClient(srv.URL, "k123", "")
	got, err := h.Ask([]chatMsg{{Role: "user", Content: "where?"}})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if got != "Bin 57" {
		t.Errorf("got %q", got)
	}
}

func TestHermesClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	h := NewHermesClient(srv.URL, "bad", "")
	if _, err := h.Ask([]chatMsg{{Role: "user", Content: "x"}}); err == nil ||
		!strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestHermesView(t *testing.T) {
	a := testApp()
	a.hermes = NewHermesClient("http://example.invalid", "k", "")

	// ctrl+h from search carries the query over
	a.search.input.SetValue("guitar jack")
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if a.mode != modeHermes {
		t.Fatalf("expected hermes mode, got %v", a.mode)
	}
	if got := a.hermesView.input.Value(); got != "guitar jack" {
		t.Errorf("expected query carried over, got %q", got)
	}

	// enter sends: transcript gains the user msg, busy spinner on
	key(a, "enter")
	if !a.hermesView.busy || len(a.hermesView.transcript) != 1 {
		t.Fatalf("expected busy with 1 transcript msg, got busy=%v len=%d",
			a.hermesView.busy, len(a.hermesView.transcript))
	}
	if !strings.Contains(a.View(), "thinking") {
		t.Errorf("expected thinking indicator:\n%s", a.View())
	}

	// reply lands (even routed through root Update) and renders
	a.Update(hermesReplyMsg{content: "It's in **Workshop/Bin 57**."})
	if a.hermesView.busy {
		t.Errorf("busy should clear on reply")
	}
	v := a.View()
	if !strings.Contains(v, "Workshop/Bin 57") || strings.Contains(v, "**") {
		t.Errorf("reply should render with markdown bold stripped:\n%s", v)
	}

	// esc returns to search; reply while away sets a status hint
	key(a, "esc")
	if a.mode != modeSearch {
		t.Fatalf("esc should return to search")
	}
	a.Update(hermesReplyMsg{content: "late reply"})
	if !strings.Contains(a.status, "hermes replied") {
		t.Errorf("expected status hint for reply while away, got %q", a.status)
	}
}

func TestHermesHiddenWhenUnconfigured(t *testing.T) {
	a := testApp() // no hermes client
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if a.mode != modeSearch {
		t.Errorf("ctrl+h should be a no-op without hermes config")
	}
	if strings.Contains(a.View(), "hermes") {
		t.Errorf("footer should not advertise hermes when unconfigured")
	}
}
