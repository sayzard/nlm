//go:build linux

package auth

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectChrome(debug bool) Browser {
	// Try standard Chrome first
	if path, err := exec.LookPath("google-chrome"); err == nil {
		version := getChromeVersion(path)
		return Browser{
			Type:    BrowserChrome,
			Path:    path,
			Name:    "Google Chrome",
			Version: version,
		}
	}

	// Try Chromium as fallback
	if path, err := exec.LookPath("chromium"); err == nil {
		version := getChromeVersion(path)
		return Browser{
			Type:    BrowserChrome,
			Path:    path,
			Name:    "Chromium",
			Version: version,
		}
	}

	return Browser{Type: BrowserUnknown}
}

func getChromeVersion(path string) string {
	cmd := exec.Command(path, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func getProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "google-chrome")
}

func getChromePath() string {
	for _, name := range []string{"google-chrome", "chrome", "chromium"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// getBrowserPathForProfile finds the browser executable path on Linux.
func getBrowserPathForProfile(browserName string) string {
	var binaryName string

	switch browserName {
	case "Brave":
		binaryName = "brave-browser"
	case "Chrome Canary":
		// Linux typically has no official Canary; use google-chrome-unstable
		binaryName = "google-chrome-unstable"
	default:
		// Fall back to standard chrome or chromium
		return getChromePath()
	}

	// On Linux, prefer exec.LookPath to find binary in $PATH
	if path, err := exec.LookPath(binaryName); err == nil {
		return path
	}

	// Fallback: check common hardcoded paths (e.g. /usr/bin)
	commonPaths := []string{
		filepath.Join("/usr/bin", binaryName),
		filepath.Join("/usr/local/bin", binaryName),
		filepath.Join("/snap/bin", binaryName), // Snap installs
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// getConfigDir returns the config base directory (follows XDG spec).
func getConfigDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return xdgConfig
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func getCanaryProfilePath() string {
	// Config path for google-chrome-unstable
	return filepath.Join(getConfigDir(), "google-chrome-unstable")
}

func getBraveProfilePath() string {
	// Brave config path on Linux
	return filepath.Join(getConfigDir(), "BraveSoftware", "Brave-Browser")
}
