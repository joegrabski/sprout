package sprout

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type SessionLayout struct {
	Windows []WindowLayout
}

type WindowLayout struct {
	Name  string
	Panes []PaneLayout
}

type PaneLayout struct {
	Command string
}

// WindowConfig defines a named tmux window with panes for the structured config.
type WindowConfig struct {
	Name   string       `toml:"name"`
	Role   string       `toml:"role"`   // optional semantic role: agent
	Layout string       `toml:"layout"` // tmux layout: even-horizontal, even-vertical, tiled, main-horizontal, main-vertical
	URL    string       `toml:"url"`    // optional URL this window/service advertises; for a worktree-independent preview service (e.g. an ngrok agent) it's also probed on promote so a live instance is reused instead of started twice
	Panes  []PaneConfig `toml:"panes"`
}

// PaneConfig defines a single tmux pane within a window.
type PaneConfig struct {
	Dir string `toml:"dir"` // working dir: abs path, ~/..., {worktree}/..., relative-to-worktree, or empty for worktree root
	Run string `toml:"run"` // command to execute
}

// PreviewSyncSet describes one key inside a synced config file: the dotted JSON
// path to write and the ngrok tunnel whose live public URL supplies the value.
type PreviewSyncSet struct {
	Path     string `toml:"path"`     // dotted JSON path, e.g. "EndpointConfig.DriveApi"
	Tunnel   string `toml:"tunnel"`   // ngrok tunnel name to read the public URL from
	Template string `toml:"template"` // optional value template; "{url}" is replaced by the tunnel URL (default "{url}")
}

// PreviewSync describes a JSON file whose keys are kept in sync with the live
// ngrok tunnel URLs whenever a worktree is promoted to preview.
type PreviewSync struct {
	File          string           `toml:"file"`           // target file: abs path, ~/..., {worktree}/..., relative-to-worktree
	ReloadWindows []string         `toml:"reload_windows"` // preview windows to restart only when this file's contents change
	Sets          []PreviewSyncSet `toml:"set"`            // keys to write from tunnel URLs
}

type Config struct {
	BaseBranch           string
	WorktreeRootTemplate string
	AutoLaunch           bool
	AutoStartAgent       bool
	UpdateCheck          bool
	SessionTools         []string
	LaunchNvim           bool
	LaunchLazygit        bool
	AgentCommand         string
	DefaultAgentType     string
	AgentCommands        map[string]string
	SessionPrefix        string
	EmitCDMarker         bool
	DaemonEnabled        bool
	DaemonSocketPath     string
	DaemonRefreshMs      int
	DaemonStaleAfterMs   int
	SessionLayouts       map[string]SessionLayout
	Windows              []WindowConfig // ordered window/pane definitions from [[windows]]
	PreviewWindows       []WindowConfig // long-running services for the preview worktree from [[preview_windows]]
	PreviewSessionSuffix string         // tmux session suffix for the dedicated preview session
	PreviewCommandPrefix string         // optional prefix wrapped around each preview service command (e.g. "portless run")
	PreviewAutoAttach    bool           // attach to the preview session after promoting (default false: run detached)
	BootstrapLinks       []string       // gitignored paths to symlink from the main worktree into each new worktree
	BootstrapSteps       []PaneConfig   // commands to run when bootstrapping a worktree (from [[bootstrap]])
	PreviewSyncs         []PreviewSync  // config files kept in sync with live tunnel URLs on promote (from [[preview_sync]])
	PreviewTunnelAPI     string         // ngrok agent local API used to resolve tunnel name -> public URL
}

func DefaultConfig() Config {
	return Config{
		BaseBranch:           "main",
		WorktreeRootTemplate: "../{repo}.worktrees",
		AutoLaunch:           true,
		AutoStartAgent:       true,
		UpdateCheck:          true,
		SessionTools:         defaultSessionTools(),
		LaunchNvim:           true,
		LaunchLazygit:        true,
		AgentCommand:         "codex",
		DefaultAgentType:     "codex",
		AgentCommands: map[string]string{
			"codex":  "codex",
			"aider":  "aider",
			"claude": "claude",
			"gemini": "gemini",
		},
		SessionPrefix:        "sprout",
		PreviewSessionSuffix: "preview",
		PreviewTunnelAPI:     "http://127.0.0.1:4040/api/tunnels",
		DaemonEnabled:        true,
		DaemonSocketPath:     "~/.cache/sprout/daemon.sock",
		DaemonRefreshMs:      750,
		DaemonStaleAfterMs:   3000,
	}
}

func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

	// Resolve git context once for structured config scoping and repo-local config.
	repoRoot := ""
	repoName := ""
	if detectedRoot, err := findGitRoot("."); err == nil {
		repoRoot = detectedRoot
		repoName = filepath.Base(repoRoot)
	}

	// 1. Global config
	globalPath := os.Getenv("SPROUT_CONFIG")
	if globalPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			globalPath = filepath.Join(home, ".config", "sprout", "config.toml")
		}
	}
	if globalPath != "" {
		if _, err := os.Stat(globalPath); err == nil {
			if err := parseTOMLFlat(globalPath, &cfg); err != nil {
				return cfg, err
			}
			if err := parseTOMLStructured(globalPath, &cfg, repoName, false); err != nil {
				return cfg, err
			}
		}
	}

	// 2. Repo-level config (.sprout.toml at git root), overrides global
	if repoRoot != "" {
		repoConfigPath := filepath.Join(repoRoot, ".sprout.toml")
		if _, err := os.Stat(repoConfigPath); err == nil {
			if err := parseTOMLFlat(repoConfigPath, &cfg); err != nil {
				return cfg, err
			}
			if err := parseTOMLStructured(repoConfigPath, &cfg, "", true); err != nil {
				return cfg, err
			}
		}
	}

	// 3. Env var overrides (highest priority)
	applyEnvOverrides(&cfg)
	if os.Getenv("SPROUT_EMIT_CD_MARKER") == "1" {
		cfg.EmitCDMarker = true
	}
	return cfg, nil
}

// findGitRoot walks up from dir until it finds a directory containing .git.
func findGitRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("not inside a git repository")
		}
		abs = parent
	}
}

func parseTOMLFlat(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	lineNum := 0
	for s.Scan() {
		lineNum++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue
		}
		line = stripComment(line)
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "base_branch":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid base_branch: %w", path, lineNum, err)
			}
			cfg.BaseBranch = v
		case "worktree_root_template":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid worktree_root_template: %w", path, lineNum, err)
			}
			cfg.WorktreeRootTemplate = v
		case "auto_launch":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid auto_launch: %w", path, lineNum, err)
			}
			cfg.AutoLaunch = v
		case "auto_start_agent":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid auto_start_agent: %w", path, lineNum, err)
			}
			cfg.AutoStartAgent = v
		case "update_check":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid update_check: %w", path, lineNum, err)
			}
			cfg.UpdateCheck = v
		case "session_tools":
			v, err := parseStringArray(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid session_tools: %w", path, lineNum, err)
			}
			cfg.SessionTools = normalizeSessionTools(v)
		case "launch_nvim":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid launch_nvim: %w", path, lineNum, err)
			}
			cfg.LaunchNvim = v
			cfg.SessionTools = setLegacySessionTool(cfg.SessionTools, "nvim", v)
		case "launch_lazygit":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid launch_lazygit: %w", path, lineNum, err)
			}
			cfg.LaunchLazygit = v
			cfg.SessionTools = setLegacySessionTool(cfg.SessionTools, "lazygit", v)
		case "agent_command":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid agent_command: %w", path, lineNum, err)
			}
			cfg.AgentCommand = v
		case "default_agent_type":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid default_agent_type: %w", path, lineNum, err)
			}
			cfg.DefaultAgentType = strings.ToLower(strings.TrimSpace(v))
		case "session_prefix":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid session_prefix: %w", path, lineNum, err)
			}
			cfg.SessionPrefix = v
		case "preview_session_suffix":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid preview_session_suffix: %w", path, lineNum, err)
			}
			cfg.PreviewSessionSuffix = v
		case "preview_command_prefix":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid preview_command_prefix: %w", path, lineNum, err)
			}
			cfg.PreviewCommandPrefix = v
		case "preview_auto_attach":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid preview_auto_attach: %w", path, lineNum, err)
			}
			cfg.PreviewAutoAttach = v
		case "preview_tunnel_api":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid preview_tunnel_api: %w", path, lineNum, err)
			}
			cfg.PreviewTunnelAPI = v
		case "bootstrap_links":
			v, err := parseStringArray(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid bootstrap_links: %w", path, lineNum, err)
			}
			cfg.BootstrapLinks = v
		case "daemon_enabled":
			v, err := parseBool(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid daemon_enabled: %w", path, lineNum, err)
			}
			cfg.DaemonEnabled = v
		case "daemon_socket_path":
			v, err := parseString(value)
			if err != nil {
				return fmt.Errorf("%s:%d invalid daemon_socket_path: %w", path, lineNum, err)
			}
			cfg.DaemonSocketPath = v
		case "daemon_refresh_ms":
			v, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("%s:%d invalid daemon_refresh_ms: %w", path, lineNum, err)
			}
			cfg.DaemonRefreshMs = v
		case "daemon_stale_after_ms":
			v, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("%s:%d invalid daemon_stale_after_ms: %w", path, lineNum, err)
			}
			cfg.DaemonStaleAfterMs = v
		default:
			if strings.HasPrefix(key, "window_") {
				// Format: window_<winname> = ["cmd1", "cmd2"]
				// This defines a global window layout (applies to all repos)
				winName := strings.TrimPrefix(key, "window_")
				v, err := parseStringArray(value)
				if err == nil {
					if cfg.SessionLayouts == nil {
						cfg.SessionLayouts = map[string]SessionLayout{}
					}
					// Use "*" as key for global layouts
					layout := cfg.SessionLayouts["*"]
					window := WindowLayout{Name: winName}
					for _, cmd := range v {
						window.Panes = append(window.Panes, PaneLayout{Command: cmd})
					}
					layout.Windows = append(layout.Windows, window)
					cfg.SessionLayouts["*"] = layout
				}
			}
			if strings.HasPrefix(key, "layout_") {
				// Format: layout_<repo>_win_<winname>_pane_<panenum> = "command"
				// e.g. layout_sprout_win_main_pane_0 = "nvim ."
				parts := strings.Split(key, "_")
				if len(parts) >= 6 && parts[2] == "win" && parts[4] == "pane" {
					repo := parts[1]
					winName := parts[3]
					paneIdx, _ := strconv.Atoi(parts[5])

					if cfg.SessionLayouts == nil {
						cfg.SessionLayouts = map[string]SessionLayout{}
					}
					layout := cfg.SessionLayouts[repo]

					// Find or create window
					winIdx := -1
					for i, w := range layout.Windows {
						if w.Name == winName {
							winIdx = i
							break
						}
					}
					if winIdx == -1 {
						layout.Windows = append(layout.Windows, WindowLayout{Name: winName})
						winIdx = len(layout.Windows) - 1
					}

					// Ensure panes array is large enough
					for len(layout.Windows[winIdx].Panes) <= paneIdx {
						layout.Windows[winIdx].Panes = append(layout.Windows[winIdx].Panes, PaneLayout{})
					}
					v, _ := parseString(value)
					layout.Windows[winIdx].Panes[paneIdx].Command = v
					cfg.SessionLayouts[repo] = layout
				}
			}
			if strings.HasPrefix(key, "agent_command_") {
				v, err := parseString(value)
				if err != nil {
					return fmt.Errorf("%s:%d invalid %s: %w", path, lineNum, key, err)
				}
				agentType := strings.TrimPrefix(key, "agent_command_")
				agentType = strings.ToLower(strings.TrimSpace(agentType))
				if agentType != "" {
					if cfg.AgentCommands == nil {
						cfg.AgentCommands = map[string]string{}
					}
					cfg.AgentCommands[agentType] = v
				}
			}
		}
	}

	if err := s.Err(); err != nil {
		return err
	}
	return nil
}

func stripComment(line string) string {
	inQuotes := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuotes = !inQuotes
		case '#':
			if !inQuotes {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return strings.TrimSpace(line)
}

func parseString(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
		unquoted, err := strconv.Unquote(v)
		if err != nil {
			return "", err
		}
		return unquoted, nil
	}
	return v, nil
}

func parseBool(v string) (bool, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %s", v)
	}
}

func defaultSessionTools() []string {
	return []string{"agent", "lazygit", "nvim"}
}

func parseStringArray(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return []string{}, nil
	}
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil, fmt.Errorf("expected array syntax like [\"agent\", \"nvim\"]")
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return []string{}, nil
	}
	rawItems := splitArrayItems(inner)
	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		parsed, err := parseString(part)
		if err != nil {
			return nil, err
		}
		parsed = strings.TrimSpace(parsed)
		if parsed == "" {
			continue
		}
		items = append(items, parsed)
	}
	return items, nil
}

func splitArrayItems(value string) []string {
	items := []string{}
	var b strings.Builder
	inQuotes := false
	escape := false

	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch ch {
		case '"':
			b.WriteByte(ch)
			if !escape {
				inQuotes = !inQuotes
			}
			escape = false
		case '\\':
			b.WriteByte(ch)
			if inQuotes {
				escape = !escape
			}
		case ',':
			if inQuotes {
				b.WriteByte(ch)
				escape = false
			} else {
				items = append(items, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(ch)
			escape = false
		}
	}
	items = append(items, b.String())
	return items
}

func normalizeSessionTools(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		tool := strings.TrimSpace(raw)
		if tool == "" {
			continue
		}
		switch strings.ToLower(tool) {
		case "agent":
			out = append(out, "agent")
		case "lazygit":
			out = append(out, "lazygit")
		case "nvim", "neovim":
			out = append(out, "nvim")
		default:
			out = append(out, tool)
		}
	}
	return out
}

func setLegacySessionTool(tools []string, tool string, enabled bool) []string {
	normalized := normalizeSessionTools(tools)
	target := ""
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "nvim", "neovim":
		target = "nvim"
	case "lazygit":
		target = "lazygit"
	case "agent":
		target = "agent"
	default:
		return normalized
	}

	out := make([]string, 0, len(normalized)+1)
	hasTarget := false
	for _, existing := range normalized {
		if existing == target {
			hasTarget = true
			if enabled {
				out = append(out, existing)
			}
			continue
		}
		out = append(out, existing)
	}
	if enabled && !hasTarget {
		out = append(out, target)
	}
	return out
}

func parseSessionToolsEnv(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(value, "[") {
		return parseStringArray(value)
	}

	parts := strings.Split(value, ",")
	tools := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		tools = append(tools, item)
	}
	return tools, nil
}

func parseStringListEnv(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(value, "[") {
		return parseStringArray(value)
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SPROUT_BASE_BRANCH"); v != "" {
		cfg.BaseBranch = v
	}
	if v := os.Getenv("SPROUT_WORKTREE_ROOT_TEMPLATE"); v != "" {
		cfg.WorktreeRootTemplate = v
	}
	if v := os.Getenv("SPROUT_AUTO_LAUNCH"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.AutoLaunch = b
		}
	}
	if v := os.Getenv("SPROUT_AUTO_START_AGENT"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.AutoStartAgent = b
		}
	}
	if v := os.Getenv("SPROUT_UPDATE_CHECK"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.UpdateCheck = b
		}
	}
	if v := os.Getenv("SPROUT_LAUNCH_NVIM"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.LaunchNvim = b
			cfg.SessionTools = setLegacySessionTool(cfg.SessionTools, "nvim", b)
		}
	}
	if v := os.Getenv("SPROUT_LAUNCH_LAZYGIT"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.LaunchLazygit = b
			cfg.SessionTools = setLegacySessionTool(cfg.SessionTools, "lazygit", b)
		}
	}
	if v := os.Getenv("SPROUT_SESSION_TOOLS"); v != "" {
		if tools, err := parseSessionToolsEnv(v); err == nil {
			cfg.SessionTools = normalizeSessionTools(tools)
		}
	}
	if v := os.Getenv("SPROUT_AGENT_COMMAND"); v != "" {
		cfg.AgentCommand = v
	}
	if v := os.Getenv("SPROUT_DEFAULT_AGENT_TYPE"); v != "" {
		cfg.DefaultAgentType = strings.ToLower(strings.TrimSpace(v))
	}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := parts[1]
		if !strings.HasPrefix(key, "SPROUT_AGENT_COMMAND_") {
			continue
		}
		agentType := strings.TrimPrefix(key, "SPROUT_AGENT_COMMAND_")
		agentType = strings.ToLower(strings.TrimSpace(agentType))
		if agentType == "" {
			continue
		}
		if cfg.AgentCommands == nil {
			cfg.AgentCommands = map[string]string{}
		}
		cfg.AgentCommands[agentType] = val
	}
	if v := os.Getenv("SPROUT_SESSION_PREFIX"); v != "" {
		cfg.SessionPrefix = v
	}
	if v := os.Getenv("SPROUT_PREVIEW_SESSION_SUFFIX"); v != "" {
		cfg.PreviewSessionSuffix = v
	}
	if v, ok := os.LookupEnv("SPROUT_PREVIEW_COMMAND_PREFIX"); ok {
		cfg.PreviewCommandPrefix = v
	}
	if v := os.Getenv("SPROUT_PREVIEW_AUTO_ATTACH"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.PreviewAutoAttach = b
		}
	}
	if v, ok := os.LookupEnv("SPROUT_PREVIEW_TUNNEL_API"); ok {
		cfg.PreviewTunnelAPI = v
	}
	if v := os.Getenv("SPROUT_BOOTSTRAP_LINKS"); v != "" {
		if items, err := parseStringListEnv(v); err == nil {
			cfg.BootstrapLinks = items
		}
	}
	if v := os.Getenv("SPROUT_DAEMON_ENABLED"); v != "" {
		if b, err := parseBool(v); err == nil {
			cfg.DaemonEnabled = b
		}
	}
	if v := os.Getenv("SPROUT_DAEMON_SOCKET"); v != "" {
		cfg.DaemonSocketPath = v
	}
	if v := os.Getenv("SPROUT_DAEMON_REFRESH_MS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.DaemonRefreshMs = n
		}
	}
	if v := os.Getenv("SPROUT_DAEMON_STALE_AFTER_MS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.DaemonStaleAfterMs = n
		}
	}
}

// parseTOMLStructured uses BurntSushi/toml to decode the structured [[windows]]
// sections from a config file. It is separate from parseTOMLFlat so existing
// flat key=value handling is unchanged.
//
// isRepoConfig=true  → reads top-level [[windows]] (from .sprout.toml)
// isRepoConfig=false → reads [[repos.<repoName>.windows]] (from global config)
func parseTOMLStructured(path string, cfg *Config, repoName string, isRepoConfig bool) error {
	type rawRepo struct {
		Windows        []WindowConfig `toml:"windows"`
		PreviewWindows []WindowConfig `toml:"preview_windows"`
		Bootstrap      []PaneConfig   `toml:"bootstrap"`
		PreviewSync    []PreviewSync  `toml:"preview_sync"`
	}
	type rawFile struct {
		Windows        []WindowConfig     `toml:"windows"`
		PreviewWindows []WindowConfig     `toml:"preview_windows"`
		Bootstrap      []PaneConfig       `toml:"bootstrap"`
		PreviewSync    []PreviewSync      `toml:"preview_sync"`
		Repos          map[string]rawRepo `toml:"repos"`
	}

	var raw rawFile
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return err
	}

	if isRepoConfig {
		if len(raw.Windows) > 0 {
			cfg.Windows = raw.Windows
		}
		if len(raw.PreviewWindows) > 0 {
			cfg.PreviewWindows = raw.PreviewWindows
		}
		if len(raw.Bootstrap) > 0 {
			cfg.BootstrapSteps = raw.Bootstrap
		}
		if len(raw.PreviewSync) > 0 {
			cfg.PreviewSyncs = raw.PreviewSync
		}
	} else if repoName != "" {
		if repoCfg, ok := raw.Repos[repoName]; ok {
			if len(repoCfg.Windows) > 0 {
				cfg.Windows = repoCfg.Windows
			}
			if len(repoCfg.PreviewWindows) > 0 {
				cfg.PreviewWindows = repoCfg.PreviewWindows
			}
			if len(repoCfg.Bootstrap) > 0 {
				cfg.BootstrapSteps = repoCfg.Bootstrap
			}
			if len(repoCfg.PreviewSync) > 0 {
				cfg.PreviewSyncs = repoCfg.PreviewSync
			}
		}
	}
	return nil
}
