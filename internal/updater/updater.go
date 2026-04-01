package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

const (
	checkInterval = 24 * time.Hour
	apiURL        = "https://api.github.com/repos/samahlstrom/forge-cli/releases/latest"
	httpTimeout   = 5 * time.Second
)

type cacheEntry struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "forge", "update-check.json")
}

func readCache() *cacheEntry {
	path := cachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

func writeCache(entry cacheEntry) {
	path := cachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644) //nolint:errcheck
}

// RefreshInBackground fires a goroutine to fetch the latest release from GitHub
// and cache it. Skips the request if the cache is still fresh.
func RefreshInBackground() {
	cache := readCache()
	if cache != nil && time.Since(cache.CheckedAt) < checkInterval {
		return
	}
	go func() {
		client := &http.Client{Timeout: httpTimeout}
		resp, err := client.Get(apiURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			return
		}
		defer resp.Body.Close()

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.TagName == "" {
			return
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		writeCache(cacheEntry{CheckedAt: time.Now(), LatestVersion: latest})
	}()
}

// NotifyIfAvailable prints a banner when the cached latest version is newer than
// currentVersion. Silent in CI, dev builds, or when FORGE_NO_UPDATE_CHECK is set.
func NotifyIfAvailable(currentVersion string) {
	if os.Getenv("CI") != "" || os.Getenv("FORGE_NO_UPDATE_CHECK") != "" {
		return
	}
	if currentVersion == "dev" || currentVersion == "" {
		return
	}
	// Don't nag after forge upgrade — the user already acted.
	if len(os.Args) > 1 && os.Args[1] == "upgrade" {
		return
	}

	cache := readCache()
	if cache == nil || !isNewer(cache.LatestVersion, currentVersion) {
		return
	}

	bold := color.New(color.Bold)
	fmt.Printf("\n  %s %s → %s\n",
		bold.Sprint("Update available:"),
		color.YellowString(currentVersion),
		color.CyanString(cache.LatestVersion),
	)
	fmt.Printf("  Run: %s\n\n", color.CyanString("brew upgrade forge"))
}

// isNewer reports whether latest is a higher semver than current.
func isNewer(latest, current string) bool {
	l := parseSemver(latest)
	c := parseSemver(current)
	for i := range l {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		// Strip pre-release or build metadata (e.g. "3-dirty", "3+meta").
		if j := strings.IndexAny(p, "-+"); j >= 0 {
			p = p[:j]
		}
		result[i], _ = strconv.Atoi(p)
	}
	return result
}
