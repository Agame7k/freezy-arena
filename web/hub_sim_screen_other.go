// Copyright 2026 Team 254. All Rights Reserved.
//
// Fallback screen size lookup for placing the hub lighting simulator windows on non-Windows systems.

//go:build !windows

package web

// hubSimScreenSize returns zeros so that the simulator windows use their default size.
func hubSimScreenSize() (int, int) {
	return 0, 0
}
