package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// fatal shows an error box (there is no console to print to) and exits.
func fatal(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	box := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("Inventory")
	const mbIconError = 0x10
	box.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
	os.Exit(1)
}
