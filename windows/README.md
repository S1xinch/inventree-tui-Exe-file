# Windows build

`inv-tui.exe` is an unofficial Windows build of inv-tui, cross-compiled from this repo's source with Go 1.24 (`GOOS=windows GOARCH=amd64`).

- Untested on Windows. Touchscreen and font-size features are Linux-only.
- Needs an InvenTree server to connect to.
- Run it from Command Prompt or Windows Terminal, not by double-clicking:

```
inv-tui.exe -url http://your-server:8000 -user USER -pass PASS
inv-tui.exe -check   # test the connection
```

Windows SmartScreen may warn because the file is unsigned. Choose "More info" then "Run anyway".
