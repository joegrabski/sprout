package sprout

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	BaseBranch           string
	WorktreeRootTemplate string
	AutoLaunch           bool
	AutoStartAgent       bool
	SessionTools         []string
	LaunchNvim           bool
	LaunchLazygit        bool
	AgentCommand         string
	DefaultAgentType     string
	AgentCommands        map[string]string
	SessionPrefix        string
	EmitCDMarker         bool
}

func DefaultConfig() Config {
	return Config{
		BaseBranch:           "main",
		WorktreeRootTemplate: "../{repo}.worktrees",
		AutoLaunch:           true,
		AutoStartAgent:       true,
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
		SessionPrefix: "sprout",
	}
}

func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

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
		}
	}

	// 2. Repo-level config (.sprout.toml at git root), overrides global
	if repoRoot, err := findGitRoot("."); err == nil {
		repoConfigPath := filepath.Join(repoRoot, ".sprout.toml")
		if _, err := os.Stat(repoConfigPath); err == nil {
			if err := parseTOMLFlat(repoConfigPath, &cfg); err != nil {
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
		default:
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
}
