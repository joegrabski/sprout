package sprout

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// previewTunnelPollTimeout bounds how long we wait for the referenced
	// tunnels to appear after a preview session first brings ngrok up.
	previewTunnelPollTimeout = 15 * time.Second
	// previewTunnelFastTimeout is used when the tunnel agent was left running
	// across a switch: the tunnels are already resolvable, so a missing one is
	// reported quickly instead of blocking the full poll.
	previewTunnelFastTimeout = 1 * time.Second
	previewTunnelPollEvery   = 500 * time.Millisecond
	previewTunnelHTTPTimeout = 2 * time.Second
)

// tunnelFetcher resolves tunnel name -> public URL from a tunnel provider's
// local API. It is a field on Manager so tests can inject a fake without hitting
// the network.
type tunnelFetcher func(apiURL string) (map[string]string, error)

// fetchNgrokTunnels reads the ngrok agent's local API and returns a map of
// tunnel name -> public URL. The response shape is:
//
//	{ "tunnels": [ { "name": "drive", "public_url": "https://abc.ngrok.app" }, ... ] }
func fetchNgrokTunnels(apiURL string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), previewTunnelHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: previewTunnelHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunnel API %s returned %s", apiURL, resp.Status)
	}
	var payload struct {
		Tunnels []struct {
			Name      string `json:"name"`
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(payload.Tunnels))
	for _, t := range payload.Tunnels {
		if t.Name == "" || t.PublicURL == "" {
			continue
		}
		// ngrok can advertise both an http and https endpoint per named
		// tunnel; prefer https when both are present.
		existing, ok := out[t.Name]
		if !ok || (strings.HasPrefix(t.PublicURL, "https://") && !strings.HasPrefix(existing, "https://")) {
			out[t.Name] = t.PublicURL
		}
	}
	return out, nil
}

// requiredTunnels returns the set of tunnel names referenced across all sync
// entries, so the poll knows when enough tunnels are up to proceed.
func (m *Manager) requiredTunnels() map[string]bool {
	req := map[string]bool{}
	for _, sync := range m.Cfg.PreviewSyncs {
		for _, set := range sync.Sets {
			if name := strings.TrimSpace(set.Tunnel); name != "" {
				req[name] = true
			}
		}
	}
	return req
}

// syncPreviewConfigs resolves the live tunnel URLs and rewrites every configured
// [[preview_sync]] file for the given preview worktree. Files are only written
// when their contents change; when a changed file lists reload_windows, those
// windows in session are restarted so the change is picked up (e.g. a Metro
// bundler reloading the mobile app's config). It never returns a hard error for
// a missing tunnel or file — those become warnings so a promote still succeeds.
func (m *Manager) syncPreviewConfigs(session, worktreePath string, tunnelsJustStarted bool) ([]string, error) {
	if len(m.Cfg.PreviewSyncs) == 0 {
		return nil, nil
	}
	fetch := m.tunnelFetch
	if fetch == nil {
		fetch = fetchNgrokTunnels
	}
	apiURL := strings.TrimSpace(m.Cfg.PreviewTunnelAPI)
	if apiURL == "" {
		apiURL = "http://127.0.0.1:4040/api/tunnels"
	}

	// A tunnel agent that was just (re)started needs time to publish its tunnels;
	// one left running across a switch is already resolvable, so fail fast rather
	// than freezing the switch waiting for a tunnel that will not appear.
	timeout := previewTunnelFastTimeout
	if tunnelsJustStarted {
		timeout = previewTunnelPollTimeout
	}

	required := m.requiredTunnels()
	tunnels, err := m.pollTunnels(fetch, apiURL, required, timeout)
	if err != nil {
		return nil, fmt.Errorf("resolving tunnels from %s: %w", apiURL, err)
	}

	var warnings []string
	for _, sync := range m.Cfg.PreviewSyncs {
		file := resolvePaneDir(sync.File, worktreePath)
		if file == "" {
			warnings = append(warnings, "preview_sync entry has no file; skipped")
			continue
		}
		changed, w, err := applyPreviewSync(file, sync, tunnels)
		warnings = append(warnings, w...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		if changed {
			m.reloadPreviewWindows(session, sync.ReloadWindows, worktreePath, &warnings)
		}
	}
	return warnings, nil
}

// pollTunnels calls fetch until every required tunnel is present or the timeout
// elapses. It returns whatever tunnels were resolved on the last attempt so that
// a partial set still drives the syncs it can (missing ones become warnings).
func (m *Manager) pollTunnels(fetch tunnelFetcher, apiURL string, required map[string]bool, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	var last map[string]string
	var lastErr error
	for {
		tunnels, err := fetch(apiURL)
		if err == nil {
			last = tunnels
			lastErr = nil
			if hasAllTunnels(tunnels, required) {
				return tunnels, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(previewTunnelPollEvery)
	}
	if last != nil {
		return last, nil
	}
	return nil, lastErr
}

func hasAllTunnels(tunnels map[string]string, required map[string]bool) bool {
	for name := range required {
		if _, ok := tunnels[name]; !ok {
			return false
		}
	}
	return true
}

// applyPreviewSync reads file, applies each set from the resolved tunnels, and
// writes it back only if the encoded bytes changed. It returns whether the file
// changed plus any per-key warnings (e.g. a tunnel that isn't up yet).
func applyPreviewSync(file string, sync PreviewSync, tunnels map[string]string) (bool, []string, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return false, nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return false, nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	var warnings []string
	mutated := false
	for _, set := range sync.Sets {
		path := strings.TrimSpace(set.Path)
		if path == "" {
			continue
		}
		url, ok := tunnels[strings.TrimSpace(set.Tunnel)]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("tunnel %q not up; left %s unchanged", set.Tunnel, path))
			continue
		}
		tmpl := set.Template
		if strings.TrimSpace(tmpl) == "" {
			tmpl = "{url}"
		}
		value := strings.ReplaceAll(tmpl, "{url}", url)
		changed, err := setJSONPath(root, path, value)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		mutated = mutated || changed
	}

	// Only rewrite when a value actually changed. This avoids reformatting a
	// file (and needlessly triggering reload_windows) when the URLs are already
	// current — the common case on a plain feature switch.
	if !mutated {
		return false, warnings, nil
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, warnings, err
	}
	out = append(out, '\n')
	if err := os.WriteFile(file, out, 0o644); err != nil {
		return false, warnings, err
	}
	return true, warnings, nil
}

// setJSONPath sets a dotted path (e.g. "EndpointConfig.DriveApi") on a decoded
// JSON object, creating intermediate objects as needed. It reports whether the
// value actually changed, and errors if an intermediate segment already holds a
// non-object value.
func setJSONPath(root map[string]any, dotted, value string) (bool, error) {
	segments := strings.Split(dotted, ".")
	cur := root
	for i, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return false, fmt.Errorf("empty segment in path %q", dotted)
		}
		if i == len(segments)-1 {
			if existing, ok := cur[seg].(string); ok && existing == value {
				return false, nil
			}
			cur[seg] = value
			return true, nil
		}
		next, ok := cur[seg]
		if !ok || next == nil {
			m := map[string]any{}
			cur[seg] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return false, fmt.Errorf("path %q crosses non-object at %q", dotted, seg)
		}
		cur = m
	}
	return false, nil
}

// reloadPreviewWindows restarts the named preview windows in session so they
// pick up a just-changed synced config. Unknown window names become warnings.
func (m *Manager) reloadPreviewWindows(session string, names []string, worktreePath string, warnings *[]string) {
	if session == "" || len(names) == 0 {
		return
	}
	windows := m.previewWindows()
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		win, idx, ok := findPreviewWindow(windows, name)
		if !ok {
			*warnings = append(*warnings, fmt.Sprintf("reload_windows: unknown preview window %q", name))
			continue
		}
		winName := trimWindowConfigName(win, idx)
		if m.tmuxWindowExists(session, winName) {
			if err := runCmdQuiet("", "tmux", "kill-window", "-t", session+":"+winName); err != nil {
				*warnings = append(*warnings, fmt.Sprintf("reload %s: %v", winName, err))
				continue
			}
		}
		if err := m.buildPreviewWindow(session, winName, win, worktreePath); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("reload %s: %v", winName, err))
		}
	}
}

// findPreviewWindow locates a preview window by its resolved name or configured
// Name, returning the window and its index within the ordered list.
func findPreviewWindow(windows []WindowConfig, name string) (WindowConfig, int, bool) {
	for i, win := range windows {
		if trimWindowConfigName(win, i) == name || strings.EqualFold(strings.TrimSpace(win.Name), name) {
			return win, i, true
		}
	}
	return WindowConfig{}, 0, false
}
