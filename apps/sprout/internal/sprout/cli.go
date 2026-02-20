package sprout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "sprout",
		Short: "sprout - git worktree manager with interactive TUI",
		Long:  GetBannerANSI() + "\nsprout - git worktree manager with interactive TUI",
		Run: func(cmd *cobra.Command, args []string) {
			mgr := getManager()
			os.Exit(RunUI(mgr))
		},
	}

	uiCmd = &cobra.Command{
		Use:   "ui",
		Short: "Launch the interactive TUI",
		Run: func(cmd *cobra.Command, args []string) {
			mgr := getManager()
			os.Exit(RunUI(mgr))
		},
	}

	newCmd = &cobra.Command{
		Use:   "new [type] [name]",
		Short: "Create a new worktree",
		Run:   runNew,
	}

	listCmd = &cobra.Command{
		Use:   "list",
		Short: "List worktrees",
		Run:   runList,
	}

	goCmd = &cobra.Command{
		Use:   "go <target>",
		Short: "Go to a worktree",
		Run:   runGo,
	}

	pathCmd = &cobra.Command{
		Use:   "path <target>",
		Short: "Get the path of a worktree",
		Run:   runPath,
	}

	launchCmd = &cobra.Command{
		Use:   "launch <target>",
		Short: "Launch a tmux session for a worktree",
		Run:   runLaunch,
	}

	detachCmd = &cobra.Command{
		Use:   "detach <target>",
		Short: "Detach from a tmux session",
		Run:   runDetach,
	}

	agentCmd = &cobra.Command{
		Use:   "agent <action> <target>",
		Short: "Manage agents (start, stop, attach)",
		Args:  cobra.ExactArgs(2),
		Run:   runAgent,
	}

	rmCmd = &cobra.Command{
		Use:   "rm <target>",
		Short: "Remove a worktree",
		Run:   runRemove,
	}

	doctorCmd = &cobra.Command{
		Use:   "doctor",
		Short: "Check system health",
		Run:   runDoctor,
	}

	shellHookCmd = &cobra.Command{
		Use:   "shell-hook <shell>",
		Short: "Generate shell hook",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			hook, err := ShellHook(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(hook)
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
)

func emitCDMarkerIfEnabled(cfg Config, path string) {
	if cfg.EmitCDMarker {
		fmt.Printf("__SPROUT_CD__=%s\n", path)
	}
}

func init() {
	newCmd.Flags().String("from", "", "Base branch to create from")
	newCmd.Flags().String("from-branch", "", "Existing branch to create worktree from")
	newCmd.Flags().Bool("no-launch", false, "Do not launch tmux session")

	listCmd.Flags().Bool("json", false, "Output in JSON format")

	goCmd.Flags().Bool("attach", false, "Attach to tmux session")
	goCmd.Flags().Bool("no-launch", false, "Do not launch tmux session")

	launchCmd.Flags().Bool("no-attach", false, "Do not attach to tmux session")

	rmCmd.Flags().Bool("force", false, "Force removal")
	rmCmd.Flags().Bool("delete-branch", false, "Delete the branch associated with the worktree")

	rootCmd.AddCommand(uiCmd, newCmd, listCmd, goCmd, pathCmd, launchCmd, detachCmd, agentCmd, rmCmd, doctorCmd, shellHookCmd, versionCmd)
}

func getManager() *Manager {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	return NewManager(cfg)
}

func Run(args []string) int {
	// We pass nothing to Execute() as it uses os.Args by default.
	// But if we want to pass specific args, we can.
	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}

func runNew(cmd *cobra.Command, args []string) {
	mgr := getManager()
	from, _ := cmd.Flags().GetString("from")
	fromBranch, _ := cmd.Flags().GetString("from-branch")
	noLaunch, _ := cmd.Flags().GetBool("no-launch")

	if fromBranch != "" {
		// Existing branch mode
		launch := mgr.Cfg.AutoLaunch && !noLaunch
		_, path, err := mgr.NewWorktree(NewOptions{
			FromBranch: fromBranch,
			Launch:     launch,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
			os.Exit(1)
		}
		if mgr.Cfg.AutoStartAgent {
			if _, _, err := mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
				fmt.Fprintln(os.Stderr, WarnMsg(fmt.Sprintf("created worktree but could not auto-start agent: %v", err)))
			}
		}
		fmt.Println(SuccessMsg(fmt.Sprintf("Created worktree from %s: %s", StyleBranch.Render(fromBranch), StylePath.Render(path))))
		emitCDMarkerIfEnabled(mgr.Cfg, path)
		return
	}

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout new <type> <name> [--from <base>] [--no-launch]"))
		fmt.Fprintln(os.Stderr, StyleDim.Render("       or: sprout new --from-branch <existing-branch>"))
		os.Exit(1)
	}

	launch := mgr.Cfg.AutoLaunch && !noLaunch
	branchType := args[0]
	name := strings.Join(args[1:], " ")
	_, path, err := mgr.NewWorktree(NewOptions{
		Type:       branchType,
		Name:       name,
		BaseBranch: from,
		Launch:     launch,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if mgr.Cfg.AutoStartAgent {
		if _, _, err := mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
			fmt.Fprintln(os.Stderr, WarnMsg(fmt.Sprintf("created worktree but could not auto-start agent: %v", err)))
		}
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Created worktree: %s", StylePath.Render(path))))
	emitCDMarkerIfEnabled(mgr.Cfg, path)
}

func runList(cmd *cobra.Command, args []string) {
	mgr := getManager()
	jsonOut, _ := cmd.Flags().GetBool("json")

	items, err := mgr.ListWorktrees()
	if err != nil {
		if errors.Is(err, ErrNotGitRepo) {
			fmt.Fprintln(os.Stderr, "error: run this command inside a git worktree")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorGreen)).
		Headers("CUR", "BRANCH", "STATUS", "TMUX", "AGENT", "PATH")

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

		// Styles
		curStr := cur
		if it.Current {
			curStr = StyleCurrentWorktree.Render(cur)
		}

		branchStr := StyleBranch.Render(branch)
		if it.Current {
			branchStr = StyleCurrentWorktree.Render(branch)
		}

		statusStr := StyleClean.Render(status)
		if it.Dirty {
			statusStr = StyleDirty.Render(status)
		}

		tmuxStr := StyleDim.Render(it.TmuxState)
		if it.TmuxState == "yes" {
			tmuxStr = StyleClean.Render(it.TmuxState)
		}

		agentStr := StyleDim.Render(it.AgentState)
		if it.AgentState == "yes" {
			agentStr = StyleClean.Render(it.AgentState)
		}

		pathStr := StylePath.Render(it.Path)

		t.Row(curStr, branchStr, statusStr, tmuxStr, agentStr, pathStr)
	}

	fmt.Println(t)
}

func runGo(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout go <target> [--attach] [--no-launch]"))
		os.Exit(1)
	}
	mgr := getManager()
	attach, _ := cmd.Flags().GetBool("attach")
	noLaunch, _ := cmd.Flags().GetBool("no-launch")

	path, err := mgr.Go(GoOptions{Target: args[0], Launch: !noLaunch, Attach: attach})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(StylePath.Render(path)))
	emitCDMarkerIfEnabled(mgr.Cfg, path)
}

func runPath(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout path <target>"))
		os.Exit(1)
	}
	mgr := getManager()
	path, err := mgr.Path(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(StylePath.Render(path))
}

func runLaunch(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout launch <target> [--no-attach]"))
		os.Exit(1)
	}
	mgr := getManager()
	noAttach, _ := cmd.Flags().GetBool("no-attach")
	path, err := mgr.Launch(LaunchOptions{Target: args[0], NoAttach: noAttach})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Launched %s", StylePath.Render(path))))
}

func runDetach(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout detach <target>"))
		os.Exit(1)
	}
	mgr := getManager()
	path, detached, err := mgr.Detach(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if detached {
		fmt.Println(SuccessMsg(fmt.Sprintf("Detached %s", StylePath.Render(path))))
	} else {
		fmt.Println(InfoMsg(fmt.Sprintf("Session not running: %s", StylePath.Render(path))))
	}
}

func runAgent(cmd *cobra.Command, args []string) {
	mgr := getManager()
	action := args[0]
	target := args[1]
	switch action {
	case "start":
		path, already, err := mgr.StartAgent(AgentOptions{Target: target, Attach: false})
		if err != nil {
			fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
			os.Exit(1)
		}
		if already {
			fmt.Println(InfoMsg(fmt.Sprintf("Agent already running: %s", StylePath.Render(path))))
		} else {
			fmt.Println(SuccessMsg(fmt.Sprintf("Agent started: %s", StylePath.Render(path))))
		}
	case "attach":
		path, err := mgr.AttachAgent(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
			os.Exit(1)
		}
		fmt.Println(SuccessMsg(fmt.Sprintf("Agent attached: %s", StylePath.Render(path))))
	case "stop":
		path, stopped, err := mgr.StopAgent(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
			os.Exit(1)
		}
		if stopped {
			fmt.Println(SuccessMsg(fmt.Sprintf("Agent stopped: %s", StylePath.Render(path))))
		} else {
			fmt.Println(InfoMsg(fmt.Sprintf("Agent not running: %s", StylePath.Render(path))))
		}
	default:
		fmt.Fprintln(os.Stderr, ErrorMsg(fmt.Sprintf("unknown action for agent: %s", action)))
		os.Exit(1)
	}
}

func runRemove(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout rm <target> [--delete-branch] [--force]"))
		os.Exit(1)
	}
	mgr := getManager()
	force, _ := cmd.Flags().GetBool("force")
	deleteBranch, _ := cmd.Flags().GetBool("delete-branch")

	path, warnings, err := mgr.Remove(RemoveOptions{Target: args[0], Force: force, DeleteBranch: deleteBranch})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, WarnMsg(w))
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Removed %s", StylePath.Render(path))))
}

func runDoctor(cmd *cobra.Command, args []string) {
	mgr := getManager()
	report := mgr.Doctor()
	for _, line := range report.Lines {
		if strings.HasPrefix(line, "ok") {
			fmt.Println(SuccessMsg(strings.TrimPrefix(line, "ok   ")))
		} else if strings.HasPrefix(line, "miss") {
			fmt.Println(ErrorMsg(strings.TrimPrefix(line, "miss ")))
		} else if strings.HasPrefix(line, "warn") {
			fmt.Println(WarnMsg(strings.TrimPrefix(line, "warn ")))
		} else {
			fmt.Println(line)
		}
	}
	os.Exit(report.ExitCode)
}
