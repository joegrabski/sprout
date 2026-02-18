package sprout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var Version = "dev"

func Run(args []string) int {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	mgr := NewManager(cfg)

	if len(args) == 0 {
		return RunUI(mgr)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "ui":
		return RunUI(mgr)
	case "new":
		return runNew(mgr, rest)
	case "list":
		return runList(mgr, rest)
	case "go":
		return runGo(mgr, rest)
	case "path":
		return runPath(mgr, rest)
	case "launch":
		return runLaunch(mgr, rest)
	case "detach":
		return runDetach(mgr, rest)
	case "agent":
		return runAgent(mgr, rest)
	case "rm", "remove":
		return runRemove(mgr, rest)
	case "doctor":
		return runDoctor(mgr, rest)
	case "shell-hook":
		return runShellHook(rest)
	case "version", "--version", "-v":
		fmt.Println(Version)
		return 0
	case "help", "--help", "-h":
		printHelp(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command: %s\n", cmd)
		printHelp(os.Stderr)
		return 1
	}
}

func runNew(mgr *Manager, args []string) int {
	base := ""
	noLaunch := false
	positionals := []string{}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--from":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --from requires a branch")
				return 1
			}
			base = args[i+1]
			i++
		case "--no-launch":
			noLaunch = true
		case "-h", "--help":
			fmt.Fprintln(os.Stdout, "usage: sprout new <type> <name> [--from <base>] [--no-launch]")
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "error: unknown option for new: %s\n", a)
				return 1
			}
			positionals = append(positionals, a)
		}
	}

	if len(positionals) < 2 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout new <type> <name> [--from <base>] [--no-launch]")
		return 1
	}
	launch := mgr.Cfg.AutoLaunch && !noLaunch
	branchType := positionals[0]
	name := strings.Join(positionals[1:], " ")
	_, path, err := mgr.NewWorktree(NewOptions{
		Type:       branchType,
		Name:       name,
		BaseBranch: base,
		Launch:     launch,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if mgr.Cfg.AutoStartAgent {
		if _, _, err := mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
			fmt.Fprintf(os.Stderr, "warn: created worktree but could not auto-start agent: %v\n", err)
		}
	}
	fmt.Println(path)
	emitCDMarkerIfEnabled(mgr.Cfg, path)
	return 0
}

func runList(mgr *Manager, args []string) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(os.Stderr, "error: usage: sprout list [--json]")
			return 1
		}
	}

	items, err := mgr.ListWorktrees()
	if err != nil {
		if errors.Is(err, ErrNotGitRepo) {
			fmt.Fprintln(os.Stderr, "error: run this command inside a git worktree")
			return 1
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Printf("%-3s %-35s %-7s %-6s %-6s %s\n", "CUR", "BRANCH", "STATUS", "TMUX", "AGENT", "PATH")
	for _, it := range items {
		cur := ""
		if it.Current {
			cur = "*"
		}
		branch := it.Branch
		if branch == "" {
			branch = "detached"
		}
		status := "clean"
		if it.Dirty {
			status = "dirty"
		}
		fmt.Printf("%-3s %-35s %-7s %-6s %-6s %s\n", cur, branch, status, it.TmuxState, it.AgentState, it.Path)
	}
	return 0
}

func runPath(mgr *Manager, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout path <branch-or-worktree>")
		return 1
	}
	path, err := mgr.Path(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println(path)
	return 0
}

func runGo(mgr *Manager, args []string) int {
	attach := false
	launch := true
	positionals := []string{}
	for _, a := range args {
		switch a {
		case "--attach":
			attach = true
		case "--no-launch":
			launch = false
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "error: unknown option for go: %s\n", a)
				return 1
			}
			positionals = append(positionals, a)
		}
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout go <branch-or-worktree> [--attach] [--no-launch]")
		return 1
	}
	path, err := mgr.Go(GoOptions{Target: positionals[0], Launch: launch, Attach: attach})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println(path)
	emitCDMarkerIfEnabled(mgr.Cfg, path)
	return 0
}

func runLaunch(mgr *Manager, args []string) int {
	noAttach := false
	positionals := []string{}
	for _, a := range args {
		switch a {
		case "--no-attach":
			noAttach = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "error: unknown option for launch: %s\n", a)
				return 1
			}
			positionals = append(positionals, a)
		}
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout launch <branch-or-worktree> [--no-attach]")
		return 1
	}
	path, err := mgr.Launch(LaunchOptions{Target: positionals[0], NoAttach: noAttach})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println(path)
	return 0
}

func runDetach(mgr *Manager, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout detach <branch-or-worktree>")
		return 1
	}
	path, detached, err := mgr.Detach(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if detached {
		fmt.Printf("detached %s\n", path)
	} else {
		fmt.Printf("session not running: %s\n", path)
	}
	return 0
}

func runAgent(mgr *Manager, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout agent <start|stop|attach> <branch-or-worktree>")
		return 1
	}
	action := args[0]
	target := args[1]
	switch action {
	case "start":
		path, already, err := mgr.StartAgent(AgentOptions{Target: target, Attach: false})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if already {
			fmt.Printf("agent already running: %s\n", path)
		} else {
			fmt.Printf("agent started: %s\n", path)
		}
		return 0
	case "attach":
		path, err := mgr.AttachAgent(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("agent attached: %s\n", path)
		return 0
	case "stop":
		path, stopped, err := mgr.StopAgent(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if stopped {
			fmt.Printf("agent stopped: %s\n", path)
		} else {
			fmt.Printf("agent not running: %s\n", path)
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "error: usage: sprout agent <start|stop|attach> <branch-or-worktree>")
		return 1
	}
}

func runRemove(mgr *Manager, args []string) int {
	force := false
	deleteBranch := false
	positionals := []string{}
	for _, a := range args {
		switch a {
		case "--force":
			force = true
		case "--delete-branch":
			deleteBranch = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "error: unknown option for rm: %s\n", a)
				return 1
			}
			positionals = append(positionals, a)
		}
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout rm <branch-or-worktree> [--delete-branch] [--force]")
		return 1
	}
	path, warnings, err := mgr.Remove(RemoveOptions{Target: positionals[0], Force: force, DeleteBranch: deleteBranch})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warn: %s\n", w)
	}
	fmt.Printf("removed %s\n", path)
	return 0
}

func runDoctor(mgr *Manager, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout doctor")
		return 1
	}
	report := mgr.Doctor()
	for _, line := range report.Lines {
		fmt.Println(line)
	}
	return report.ExitCode
}

func runShellHook(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: sprout shell-hook <zsh|bash|fish>")
		return 1
	}
	hook, err := ShellHook(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Print(hook)
	return 0
}

func emitCDMarkerIfEnabled(cfg Config, path string) {
	if cfg.EmitCDMarker {
		fmt.Printf("__SPROUT_CD__=%s\n", path)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `sprout - git worktree manager with interactive TUI

Usage:
  sprout                      # launch TUI
  sprout ui
  sprout new <type> <name> [--from <base>] [--no-launch]
  sprout list [--json]
  sprout go <branch-or-worktree> [--attach] [--no-launch]
  sprout path <branch-or-worktree>
  sprout launch <branch-or-worktree> [--no-attach]
  sprout detach <branch-or-worktree>
  sprout agent <start|stop|attach> <branch-or-worktree>
  sprout rm <branch-or-worktree> [--delete-branch] [--force]
  sprout doctor
  sprout shell-hook <zsh|bash|fish>
  sprout version
  sprout help

Examples:
  sprout new feat checkout-redesign
  sprout go feat/checkout-redesign
  sprout detach feat/checkout-redesign
  sprout agent start feat/checkout-redesign
  sprout rm feat/checkout-redesign --delete-branch
  eval "$(sprout shell-hook zsh)"
`)
}
