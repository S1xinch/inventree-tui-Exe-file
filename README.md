# Home Inventory

Keeps track of the stuff around the house: what you've got, which box or shelf it's in, and when you're running low on something.

It runs completely offline. There's no account, no server and nothing to set up. Everything is saved in one file, `inventory.json`, in the same folder as the program.

This started as a fork of [inventree-tui](https://github.com/AndrewYatzkan/inventree-tui) by Andrew Yatzkan, a terminal front end for an InvenTree server. I swapped the server out for a local file, added low-stock warnings, and built a normal Windows app on top of it.

## Download

Everything is in the [`windows`](windows/) and [`mac`](mac/) folders.

| File | What it is |
|------|------------|
| `windows/Inventory.exe` | The main app. Opens in its own window. Start here. |
| `windows/Inventory Server.exe` | Same app, but runs through a small server on your own PC (`127.0.0.1:47615`). Try this if the main one won't open, or if you'd like to use it from your browser. |
| `windows/inv-tui.exe` | The original keyboard-only version that runs in a terminal. |
| `mac/inv-tui-mac` | Terminal version for Apple Silicon Macs. |

Put the program in its own folder (something like `C:\Inventory`) and double-click it. The first time, Windows may say it "protected your PC" because the file isn't signed. Click **More info**, then **Run anyway**.

All three Windows programs use the same `inventory.json`, so you can switch between them. Just don't edit in two at the same time.

## Using it

**Adding things.** Click **Add item**. Only the name is required, but it's worth filling in:

- **Min stock**: the point where you want a warning. If you keep 4 AA batteries and want to know when you're down to fewer, put 4. Leave it at 0 if you don't care.
- **How many now** and **Bin**: how many you've got and where they live.
- **Category**: optional, handy for grouping things like `Electronics/Batteries`.

**Bins and categories.** There's no separate screen for these. Type a new name into the Bin or Category box and it gets created. A slash makes one inside another, so `Garage/Shelf 2` puts Shelf 2 inside Garage.

**Changing stock.** Click an item to see every bin it's in. Use **-1** and **+1** for quick changes, or **More** to set an exact count, move some to another bin, or take it out of a bin. **Put some in a bin** adds stock to a new or existing bin.

**Searching.** Type in the box at the top. It matches names, descriptions, keywords, categories and bin names.

## Low stock warnings

The app checks when it opens, every two minutes after that, and straight away whenever something drops below its minimum. Anything that's short:

- shows its quantity in red
- appears in a red banner at the top, emptiest first
- is counted on the **Low stock** button, which also filters the list down to just those items

Click anywhere in the window once and it'll ask whether it can show Windows notifications as well.

Only items with a Min stock above 0 are checked.

## Your data

Everything lives in `inventory.json` next to the program. To back up, copy that file. To move to another PC, copy the program and the file together.

You can point any version at a different file with `-data`, for example `Inventory.exe -data D:\Backup\inventory.json`.

## The terminal version

`inv-tui.exe` and `inv-tui-mac` do the same job from the keyboard. Type to search, `enter` opens an item, `ctrl+n` adds one, `ctrl+l` lists what's low, `ctrl+z` and `ctrl+y` undo and redo, and `F1` shows every shortcut. On a Mac, run it from Terminal:

```
chmod +x inv-tui-mac
xattr -d com.apple.quarantine inv-tui-mac
./inv-tui-mac
```

It still works on a Raspberry Pi with a small touchscreen, which is what the original was made for. The `deploy.sh`, `Makefile` and `inv-tui.service` files are from that setup.

## Building it yourself

You need Go 1.21 or newer.

```
# Windows app (no server)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H=windowsgui -s -w" -o windows/Inventory.exe ./gui

# Windows app, local server version
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags server -ldflags "-H=windowsgui -s -w" -o "windows/Inventory Server.exe" ./gui

# Terminal versions
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o windows/inv-tui.exe .
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o mac/inv-tui-mac .
```

The app icon and version details are in `gui/winres/winres.json`. After changing them, run `go-winres make` inside `gui`.

Run the tests with `go test ./...`.
