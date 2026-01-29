package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// LocalState maps the Chrome Local State file structure.
type LocalState struct {
	Profile struct {
		InfoCache map[string]struct {
			Name       string  `json:"name"`
			ActiveTime float64 `json:"active_time"` // Chrome timestamp
		} `json:"info_cache"`
	} `json:"profile"`
}

type ProfileInfo struct {
	DirName     string
	DisplayName string
	LastUsed    time.Time
	HasCookies  bool
}

func main() {
	fmt.Println("🚀 nlm-lite auth tool (smart profile detection)")
	fmt.Println("==================================================")

	// 1. Find Chrome data path
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	chromeUserData := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")

	if _, err := os.Stat(chromeUserData); os.IsNotExist(err) {
		fmt.Println("❌ Chrome data folder not found.")
		return
	}

	// 2. Scan profiles and auto-select best one
	targetProfile := detectBestProfile(chromeUserData)
	if targetProfile == nil {
		fmt.Println("❌ No usable profile found.")
		return
	}

	fmt.Printf("\n🎯 Selected profile: %s (%s)\n", targetProfile.DisplayName, targetProfile.DirName)
	fmt.Printf("   Last used: %s\n", targetProfile.LastUsed.Format("2006-01-02 15:04:05"))
	fmt.Println("==================================================")

	// 3. Create temp directory
	tempDir, err := os.MkdirTemp("", "nlm-go-auth-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)
	fmt.Printf("📂 Temp workspace: %s\n", tempDir)

	// 4. Copy data
	// 4-1. Copy Local State
	copyFile(filepath.Join(chromeUserData, "Local State"), filepath.Join(tempDir, "Local State"))

	// 4-2. Copy profile folder
	sourceProfilePath := filepath.Join(chromeUserData, targetProfile.DirName)
	destProfilePath := filepath.Join(tempDir, targetProfile.DirName)
	os.MkdirAll(destProfilePath, 0755)

	filesToCopy := []string{"Cookies", "Login Data", "Web Data", "Preferences"}
	for _, fname := range filesToCopy {
		copyFile(filepath.Join(sourceProfilePath, fname), filepath.Join(destProfilePath, fname))
	}
	fmt.Println("✅ Auth data copied.")

	// 5. Launch Chrome for auth
	runBrowserAuth(tempDir, targetProfile.DirName)
}

// detectBestProfile scans all profiles and returns the best one (recent use + has cookies).
func detectBestProfile(userDataDir string) *ProfileInfo {
	// Parse Local State
	localStatePath := filepath.Join(userDataDir, "Local State")
	file, err := os.Open(localStatePath)
	if err != nil {
		// If Local State is missing, assume Default only
		return &ProfileInfo{DirName: "Default", DisplayName: "Default", LastUsed: time.Now()}
	}
	defer file.Close()

	var state LocalState
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		return &ProfileInfo{DirName: "Default", DisplayName: "Default", LastUsed: time.Now()}
	}

	var profiles []ProfileInfo

	// Build profile list
	for dirName, info := range state.Profile.InfoCache {
		// Chrome Timestamp -> Go Time
		// (Microseconds since 1601-01-01)
		seconds := info.ActiveTime/1000000 - 11644473600
		lastUsed := time.Unix(int64(seconds), 0)

		// Check if cookie file exists
		cookiePath := filepath.Join(userDataDir, dirName, "Cookies")
		hasCookies := false
		if stat, err := os.Stat(cookiePath); err == nil && stat.Size() > 0 {
			hasCookies = true
		} else {
			// Check Network subfolder (newer Chrome)
			cookiePath = filepath.Join(userDataDir, dirName, "Network", "Cookies")
			if stat, err := os.Stat(cookiePath); err == nil && stat.Size() > 0 {
				hasCookies = true
			}
		}

		profiles = append(profiles, ProfileInfo{
			DirName:     dirName,
			DisplayName: info.Name,
			LastUsed:    lastUsed,
			HasCookies:  hasCookies,
		})
	}

	// Sort: prefer profiles with cookies, then by most recently used
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].HasCookies != profiles[j].HasCookies {
			return profiles[i].HasCookies // profiles with cookies first
		}
		return profiles[i].LastUsed.After(profiles[j].LastUsed) // then by recent use
	})

	// Print scan result (for debugging)
	fmt.Printf("🔍 Detected profiles (%d):\n", len(profiles))
	for i, p := range profiles {
		mark := " "
		if i == 0 {
			mark = "*" // selected profile
		}
		cookieStatus := "No Cookies"
		if p.HasCookies {
			cookieStatus = "Has Cookies"
		}
		fmt.Printf(" %s [%d] %s (%s) - %s\n", mark, i+1, p.DisplayName, p.DirName, cookieStatus)
	}

	if len(profiles) > 0 {
		return &profiles[0]
	}
	return nil
}

func runBrowserAuth(userDataDir, profileDirName string) {
	// Chrome launch options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(userDataDir),
		chromedp.Flag("profile-directory", profileDirName), // use detected profile name
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// Suppress unnecessary pipe error logs by passing no-op to WithLogf/WithErrorf
	ctx, cancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(func(string, ...interface{}) {}),
		chromedp.WithErrorf(func(string, ...interface{}) {}),
	)
	defer cancel()

	fmt.Println("🌍 Connecting to NotebookLM...")

	var token string
	var cookies []*network.Cookie

	// Timeout 30 seconds
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://notebooklm.google.com/"),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),

		// Extract token
		chromedp.Evaluate(`window.WIZ_global_data?.SNlM0e || ""`, &token),

		// Extract cookies
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithUrls([]string{"https://notebooklm.google.com"}).Do(ctx)
			return err
		}),
	)

	if err != nil {
		fmt.Printf("❌ Browser error: %v\n", err)
		return
	}

	if token == "" {
		fmt.Println("❌ Token not found. (Selected profile may not be logged in)")
		return
	}

	fmt.Printf("\n🎉 Auth successful!\n")
	fmt.Printf("🔑 Token: %s...\n", token[:20])

	var cookieStrs []string
	for _, c := range cookies {
		cookieStrs = append(cookieStrs, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}
	cookieHeader := strings.Join(cookieStrs, "; ")
	fmt.Printf("🍪 Cookies: %d loaded\n", len(cookies))

	envContent := fmt.Sprintf("NLM_AUTH_TOKEN='%s'\nNLM_COOKIES='%s'\n", token, cookieHeader)

	cwd, _ := os.Getwd()
	envPath := filepath.Join(cwd, ".env.nlm")
	os.WriteFile(envPath, []byte(envContent), 0644)

	fmt.Printf("💾 Saved to: %s\n", envPath)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
