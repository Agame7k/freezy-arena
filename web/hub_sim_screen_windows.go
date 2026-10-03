// Copyright 2026 Team 254. All Rights Reserved.
//
// Windows screen size lookup for placing the hub lighting simulator windows.

package web

import "syscall"

const (
	smCxScreen     = 0
	smCyFullScreen = 17
)

// hubSimScreenSize returns the primary screen width and usable height (above the taskbar), or zeros if unknown.
func hubSimScreenSize() (int, int) {
	getSystemMetrics := syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
	if getSystemMetrics.Find() != nil {
		return 0, 0
	}
	width, _, _ := getSystemMetrics.Call(smCxScreen)
	height, _, _ := getSystemMetrics.Call(smCyFullScreen)
	return int(width), int(height)
}
