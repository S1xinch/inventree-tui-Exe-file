# inv-tui

A fast terminal UI for [InvenTree](https://inventree.org/) home inventory,
built for the Raspberry Pi 1 B+ kiosk (touchscreen + USB keyboard) where the
InvenTree web UI is unusably slow. Single static Go binary, no runtime
dependencies, talks to the standard InvenTree REST API.

Built with [bubbletea](https://github.com/charmbracelet/bubbletea).

## Why it's fast

The entire inventory (parts, stock, locations, categories — a few hundred
records) is loaded in a handful of paginated API calls at startup, then all
searching/filtering happens locally. Keystroke-to-result is instant even on a
700 MHz ARMv6. Stock queries use `location_detail=true` so every result shows
**which bin the item is in** (the stock record alone only carries a location
pk).

## Usage

The main screen is search: just type. All terms must match (name, IPN,
description, keywords, category, or bin path). Matching is typo-tolerant:
terms of 4+ chars tolerate one edit (insert/delete/substitute/transpose),
8+ chars tolerate two, checked word-by-word — so `resistr` finds resistors
and `jakc` finds jacks. Exact matches always rank above fuzzy ones. Results
show name, quantity, and bin inline.

Filters can be mixed into the query in any position: `led bin:20` searches
for "led" within Bin 20 (numeric bin filters match the bin number exactly),
`bin:closet` matches any location whose path contains "closet" (`loc:` is an
alias), and `cat:tools` filters by category.

| Key | Action |
|-----|--------|
| type / `esc` | filter / clear filter |
| `↑` `↓` `pgup` `pgdn` | navigate results |
| `enter` | open part detail |
| `ctrl+n` | create a new part |
| `ctrl+h` | ask hermes (if configured) |
| `ctrl+z` / `ctrl+y` | undo / redo the last change |
| `ctrl+r` / `f5` | re-sync from server |
| `F1` (or `?` in detail view) | help overlay with all shortcuts |
| `F2` / `F3` (or `alt+-` / `alt++`) | smaller / bigger text (persisted across reboots) |
| `ctrl+q` / `ctrl+c` | quit |

Font sizing works by running `setfont` with the console-setup Terminus fonts
(12px–32px), so it only applies on the Linux console; the choice is saved to
the state dir and re-applied at startup.

Part detail view: `a` add stock, `r` remove, `c` count (stocktake), `m` move
to another bin, `n` new stock item in a new bin, `d` delete stock item,
`e` edit part, `D` delete part (deactivates then deletes), `esc` back.

### Hermes handoff

For queries beyond literal search ("what do I have for building a guitar
pedal?"), `ctrl+h` opens a chat with the home hermes agent (an
OpenAI-compatible chat completions API that has its own InvenTree access),
carrying over the current search query. Multi-turn: the conversation persists
until cleared with `ctrl+x`. Replies that arrive while you're elsewhere in
the app show a status hint. Configure with `HERMES_URL` + `HERMES_KEY`
(optional `HERMES_MODEL`, default `hermes-agent`); the ctrl+h binding and
footer hint only appear when configured. Agent replies can take 10–60s; a
thinking indicator with elapsed time shows while it works.

### Undo / mutation journal

InvenTree tracks stock history server-side but has no undo, and deletes are
permanent. So every mutation this binary makes (part create/edit/delete,
stock add/remove/count/move/create/delete) is journaled to
`~/.local/state/inv-tui/journal.jsonl` (override with `INV_TUI_JOURNAL`) with
a pre-image snapshot of the part and its stock taken immediately before the
change — if the snapshot fails, the mutation is aborted. The journal is
capped at the most recent 500 entries (a few MB worst case) since the Pi is
short on disk.

`ctrl+z` undoes the most recent not-yet-undone entry, `ctrl+y` redoes (both
with a confirm prompt). Undo/redo chains compose: each undo is journaled as a
faithful inverse action with its own pre-image, so redo is just "undo the
undo", repeated ctrl+z walks backward through history, and — like any editor
— making a fresh edit clears the redo chain. Older entries: `inv-tui -log 20`
lists them with IDs, `inv-tui -undo <id>` reverses one (works on undo entries
too, which makes it a redo). Quantity ops reverse exactly (add↔remove,
count/move restore the pre-image value); deleted parts/stock are recreated
from the snapshot with the same fields, quantities, and bins (but new
database IDs).

Mutations are serialized: while one (including an undo/redo) is in flight,
starting another is rejected with a "busy" status rather than queued — this
makes a rapid double ctrl+z racing the journal impossible, and a queued-up
destructive op firing late would be worse than asking you to press the key
again. Note the journal only covers edits made through this binary — changes
made via the web UI or hermes aren't captured.

### Touchscreen

On Linux the app reads raw evdev touch events (`/dev/input/event*`,
auto-detected by the BTN_TOUCH capability) and translates gestures itself,
since the framebuffer console can't report mouse events. Tap a row to select
it, tap again to open; in detail view tap the title line to go back. Dragging
scrolls (drag down to move down the list, one line per cell), and a touch
that moved more than one cell never counts as a tap. Set `INV_TUI_TOUCH=off` to disable
or `INV_TUI_TOUCH=/dev/input/event2` to pin a device. The user running
inv-tui needs to be in the `input` group (or root).

## Configuration

Read from `/etc/inv-tui.env`, then `~/.config/inv-tui/env`, then environment
variables, then flags (`-url`, `-user`, `-pass`, `-touch`). See
`env.example`. Credentials are exchanged for an API token at startup.

Sanity check connectivity with `inv-tui -check`.

## Building & deploying

Requires Go ≥ 1.21 on the dev machine only.

```sh
make build     # local build (for development on mac)
make pi        # cross-compile static ARMv6 binary
make deploy    # build + push + install + (re)start kiosk service
```

`deploy.sh` does everything in one shot: cross-compiles, pushes the binary,
the systemd unit, and the local `.env` (gitignored, real credentials) to the
Pi, runs `inv-tui -check` there, disables `getty@tty1`, and enables/restarts
the `inv-tui` kiosk service on tty1. Default target is `root@192.168.4.193`
(override with `PI_HOST=...`). Files are pushed via `cat` over ssh because
the Pi's minimal image has neither sftp-server nor scp.

For a bigger console font on the touchscreen: `sudo dpkg-reconfigure
console-setup` (e.g. Terminus 16x32).

## Tests

`go test ./...` runs the UI model tests. With `INVENTREE_URL`,
`INVENTREE_USERNAME`, `INVENTREE_PASSWORD` set it also runs a live CRUD
round-trip (creates and deletes a throwaway part named "ZZZ inv-tui
self-test part").
