package sprout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 2 * time.Second
	updateCacheFile     = "update.json"
	updateRepo          = "joegrabski/sprout"
)

type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func shouldCheckForUpdates(cfg Config) bool {
	return cfg.UpdateCheck
}

func updateCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sprout", updateCacheFile), nil
}

func readUpdateCache() (updateCache, error) {
	path, err := updateCachePath()
	if err != nil {
		return updateCache{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, err
	}
	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return updateCache{}, err
	}
	return cache, nil
}

func writeUpdateCache(cache updateCache) {
	path, err := updateCachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func latestReleaseTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sprout-update-check")
	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("update check failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return "", errors.New("update check missing tag name")
	}
	return strings.TrimSpace(payload.TagName), nil
}

func parseSemver(value string) ([3]int, bool) {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return [3]int{}, false
	}
	raw = strings.TrimPrefix(raw, "v")
	if idx := strings.IndexAny(raw, "+-"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func isNewerVersion(latest, current string) bool {
	latestV, ok := parseSemver(latest)
	if !ok {
		return false
	}
	currentV, ok := parseSemver(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if latestV[i] > currentV[i] {
			return true
		}
		if latestV[i] < currentV[i] {
			return false
		}
	}
	return false
}

func checkForUpdate(current string, cfg Config) (string, bool) {
	if strings.TrimSpace(current) == "" || strings.EqualFold(strings.TrimSpace(current), "dev") {
		return "", false
	}
	if !shouldCheckForUpdates(cfg) {
		return "", false
	}
	cache, err := readUpdateCache()
	if err == nil && time.Since(cache.CheckedAt) < updateCheckInterval {
		if cache.Latest != "" && isNewerVersion(cache.Latest, current) {
			return cache.Latest, true
		}
		return "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	latest, err := latestReleaseTag(ctx)
	if err != nil {
		return "", false
	}
	writeUpdateCache(updateCache{CheckedAt: time.Now(), Latest: latest})
	if isNewerVersion(latest, current) {
		return latest, true
	}
	return "", false
}
