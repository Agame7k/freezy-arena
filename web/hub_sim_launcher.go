// Copyright 2026 Team 254. All Rights Reserved.
//
// Opens the hub lighting and field simulators in their own browser windows when Cheesy Arena is started with -hubsim
// or -simfield.

package web

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	simServerWaitTimeout      = 15 * time.Second
	simDefaultScreenWidth     = 1800
	simDefaultScreenHeight    = 1000
	hubSimMaxWindowHeight     = 1100
	fieldSimWindowScreenRatio = 0.92
)

// simWindow is one simulator window to open.
type simWindow struct {
	name   string
	path   string
	x      int
	y      int
	width  int
	height int
}

// OpenSimWindows waits for the web server to come up and then opens app-style browser windows for the requested
// simulators: one per Hub side by side for the hub lighting simulator, and one large window for the field simulator.
// Falls back to tabs in the default browser if Edge or Chrome can't be found.
func (web *Web) OpenSimWindows(port int, hubSim, fieldSim bool) {
	if !waitForWebServer(port, simServerWaitTimeout) {
		log.Printf("Simulator: web server didn't come up on port %d; not opening the simulator windows.", port)
		return
	}

	screenWidth, screenHeight := hubSimScreenSize()
	if screenWidth <= 0 || screenHeight <= 0 {
		screenWidth, screenHeight = simDefaultScreenWidth, simDefaultScreenHeight
	}

	var windows []simWindow
	if fieldSim {
		width := int(float64(screenWidth) * fieldSimWindowScreenRatio)
		height := int(float64(screenHeight) * fieldSimWindowScreenRatio)
		windows = append(
			windows,
			simWindow{"field", "/field_sim", (screenWidth - width) / 2, (screenHeight - height) / 2, width, height},
		)
	}
	if hubSim {
		width := screenWidth / 2
		height := min(screenHeight, hubSimMaxWindowHeight)
		windows = append(
			windows,
			simWindow{"red", "/hub_sim?alliance=red", 0, 0, width, height},
			simWindow{"blue", "/hub_sim?alliance=blue", width, 0, width, height},
		)
	}

	browser := findAppModeBrowser()
	for _, window := range windows {
		url := fmt.Sprintf("http://localhost:%d%s", port, window.path)
		profileDir := filepath.Join(os.TempDir(), "cheesy-arena-hubsim", window.name)
		if browser != "" && simWindowOpen(profileDir) {
			// The window from a previous run is still open and reconnects by itself.
			log.Printf("Simulator: %s is already open.", url)
			continue
		}
		log.Printf("Simulator: opening %s", url)
		if browser == "" {
			openInDefaultBrowser(url)
			continue
		}

		// Give each window its own profile so that the size and position flags are honored.
		cmd := exec.Command(
			browser,
			"--app="+url,
			"--user-data-dir="+profileDir,
			fmt.Sprintf("--window-size=%d,%d", window.width, window.height),
			fmt.Sprintf("--window-position=%d,%d", window.x, window.y),
			"--no-first-run",
			"--no-default-browser-check",
			"--autoplay-policy=no-user-gesture-required",
			// Keep the simulator running and reading its updates while it is covered or minimized.
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
		)
		if err := cmd.Start(); err != nil {
			log.Printf("Simulator: couldn't start %s: %v", browser, err)
			openInDefaultBrowser(url)
			continue
		}
		go cmd.Wait()
	}
}

// waitForWebServer returns true once something is listening on the given local port.
func waitForWebServer(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// findAppModeBrowser returns the path to a Chromium-based browser that supports --app windows, or "" if none is found.
func findAppModeBrowser() string {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, envVar := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
			if dir := os.Getenv(envVar); dir != "" {
				candidates = append(
					candidates,
					filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"),
				)
			}
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge"} {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// openInDefaultBrowser opens the URL in the system's default browser.
func openInDefaultBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Simulator: couldn't open %s in the default browser: %v", url, err)
		return
	}
	go cmd.Wait()
}

// simWindowOpen returns true if a browser is still running with the given simulator window profile, as Chromium keeps
// a lock in the profile directory while it runs.
func simWindowOpen(profileDir string) bool {
	if runtime.GOOS == "windows" {
		lockPath := filepath.Join(profileDir, "lockfile")
		if _, err := os.Stat(lockPath); err != nil {
			return false
		}
		// The running browser holds the lock open, so it can only be removed if it was left behind.
		return os.Remove(lockPath) != nil
	}
	_, err := os.Lstat(filepath.Join(profileDir, "SingletonLock"))
	return err == nil
}
