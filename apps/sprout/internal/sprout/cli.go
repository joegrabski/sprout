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

	previewCmd = &cobra.Command{
		Use:   "preview [target]",
		Short: "Manage the preview worktree (runs configured services from a chosen worktree)",
		Long: "Designate one worktree as the preview: a dedicated tmux session runs the\n" +
			"configured [[preview_windows]] services from that worktree. Promoting another\n" +
			"worktree restarts the services from its path.\n\n" +
			"  sprout preview            show current preview status\n" +
			"  sprout preview <target>   promote a worktree to preview\n" +
			"  sprout preview stop       stop the preview session\n" +
			"  sprout preview restart    restart services from the current preview worktree\n" +
			"  sprout preview sync       rewrite [[preview_sync]] configs from live tunnel URLs\n" +
			"  sprout preview logs <service>   show recent output of a preview service",
		Run: runPreview,
	}

	previewStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show the current preview worktree and service state",
		Run:   runPreviewStatus,
	}

	previewStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the preview session and clear the preview worktree",
		Run:   runPreviewStop,
	}

	previewRestartCmd = &cobra.Command{
		Use:   "restart",
		Short: "Restart services from the current preview worktree",
		Run:   runPreviewRestart,
	}

	previewSyncCmd = &cobra.Command{
		Use:   "sync",
		Short: "Rewrite [[preview_sync]] configs from the live tunnel URLs",
		Run:   runPreviewSync,
	}

	previewLogsCmd = &cobra.Command{
		Use:   "logs <service>",
		Short: "Show recent output of a preview service",
		Args:  cobra.ExactArgs(1),
		Run:   runPreviewLogs,
	}

	agentCmd = &cobra.Command{
		Use:   "agent <action> <target>",
		Short: "Manage agents (start, stop, attach)",
		Args:  cobra.ExactArgs(2),
		Run:   runAgent,
	}

	bootstrapCmd = &cobra.Command{
		Use:   "bootstrap [target]",
		Short: "Bootstrap a worktree: symlink local config from the main worktree and run setup commands",
		Run:   runBootstrap,
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

	daemonCmd = &cobra.Command{
		Use:   "daemon",
		Short: "Manage sprout metadata daemon",
	}

	daemonStartCmd = &cobra.Command{
		Use:   "start",
		Short: "Start daemon in background",
		Run:   runDaemonStart,
	}

	daemonStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop daemon",
		Run:   runDaemonStop,
	}

	daemonStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Run:   runDaemonStatus,
	}

	daemonRunCmd = &cobra.Command{
		Use:    "run",
		Short:  "Run daemon in foreground",
		Hidden: true,
		Run:    runDaemonRun,
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
	newCmd.Flags().Bool("no-bootstrap", false, "Do not run bootstrap (symlinks + setup commands) after creating")

	listCmd.Flags().Bool("json", false, "Output in JSON format")

	goCmd.Flags().Bool("attach", false, "Attach to tmux session")
	goCmd.Flags().Bool("no-launch", false, "Do not launch tmux session")

	launchCmd.Flags().Bool("no-attach", false, "Do not attach to tmux session")

	rmCmd.Flags().Bool("force", false, "Force removal")
	rmCmd.Flags().Bool("delete-branch", false, "Delete the branch associated with the worktree")

	previewCmd.Flags().Bool("attach", false, "Attach to the preview session after promoting")
	previewStatusCmd.Flags().Bool("json", false, "Output in JSON format")
	previewRestartCmd.Flags().Bool("attach", false, "Attach to the preview session after restarting")
	previewLogsCmd.Flags().IntP("lines", "n", 200, "Number of lines to capture")
	previewCmd.AddCommand(previewStatusCmd, previewStopCmd, previewRestartCmd, previewSyncCmd, previewLogsCmd)

	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd, daemonRunCmd)
	rootCmd.AddCommand(uiCmd, newCmd, listCmd, goCmd, pathCmd, launchCmd, detachCmd, agentCmd, previewCmd, bootstrapCmd, rmCmd, doctorCmd, shellHookCmd, versionCmd, daemonCmd)
}

func getManager() *Manager {
	done := startProfileStage("config_load")
	defer done()
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

// finishCreate runs the post-creation steps for a new worktree: bootstrap the
// worktree (symlink local config + install deps), start the agent, then launch.
// Bootstrap and launch run before attaching so deps are ready first.
// printBootstrapStep prints a bootstrap command step to stdout (CLI feedback).
func printBootstrapStep(dir, run string) {
	fmt.Println(StyleDim.Render(fmt.Sprintf("→ [%s] %s", dir, run)))
}

func finishCreate(mgr *Manager, path string, launch, noBootstrap bool) {
	if !noBootstrap && mgr.HasBootstrap() {
		fmt.Println(InfoMsg("Bootstrapping worktree..."))
		if _, err := mgr.Bootstrap(BootstrapOptions{Target: path, OnStep: printBootstrapStep}); err != nil {
			fmt.Fprintln(os.Stderr, WarnMsg(fmt.Sprintf("bootstrap failed: %v", err)))
		}
	}
	if mgr.Cfg.AutoStartAgent {
		if _, _, err := mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
			fmt.Fprintln(os.Stderr, WarnMsg(fmt.Sprintf("created worktree but could not auto-start agent: %v", err)))
		}
	}
	if launch {
		if _, err := mgr.Launch(LaunchOptions{Target: path}); err != nil {
			fmt.Fprintln(os.Stderr, WarnMsg(fmt.Sprintf("created worktree but could not launch: %v", err)))
		}
	}
}

func runNew(cmd *cobra.Command, args []string) {
	mgr := getManager()
	from, _ := cmd.Flags().GetString("from")
	fromBranch, _ := cmd.Flags().GetString("from-branch")
	noLaunch, _ := cmd.Flags().GetBool("no-launch")
	noBootstrap, _ := cmd.Flags().GetBool("no-bootstrap")
	launch := mgr.Cfg.AutoLaunch && !noLaunch

	if fromBranch != "" {
		// Existing branch mode
		_, path, err := mgr.NewWorktree(NewOptions{FromBranch: fromBranch})
		if err != nil {
			fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
			os.Exit(1)
		}
		fmt.Println(SuccessMsg(fmt.Sprintf("Created worktree from %s: %s", StyleBranch.Render(fromBranch), StylePath.Render(path))))
		finishCreate(mgr, path, launch, noBootstrap)
		emitCDMarkerIfEnabled(mgr.Cfg, path)
		return
	}

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, ErrorMsg("usage: sprout new <type> <name> [--from <base>] [--no-launch] [--no-bootstrap]"))
		fmt.Fprintln(os.Stderr, StyleDim.Render("       or: sprout new --from-branch <existing-branch>"))
		os.Exit(1)
	}

	branchType := args[0]
	name := strings.Join(args[1:], " ")
	_, path, err := mgr.NewWorktree(NewOptions{
		Type:       branchType,
		Name:       name,
		BaseBranch: from,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Created worktree: %s", StylePath.Render(path))))
	finishCreate(mgr, path, launch, noBootstrap)
	emitCDMarkerIfEnabled(mgr.Cfg, path)
}

func runBootstrap(cmd *cobra.Command, args []string) {
	mgr := getManager()
	if !mgr.HasBootstrap() {
		fmt.Fprintln(os.Stderr, WarnMsg("no bootstrap configured; add bootstrap_links or [[bootstrap]] to .sprout.toml"))
		os.Exit(1)
	}
	target := ""
	if len(args) > 0 {
		target = args[0]
	}
	res, err := mgr.Bootstrap(BootstrapOptions{Target: target, OnStep: printBootstrapStep})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	for _, l := range res.LinksCreated {
		fmt.Println(SuccessMsg("linked " + l))
	}
	if len(res.LinksMissing) > 0 {
		fmt.Println(WarnMsg(fmt.Sprintf("missing in main worktree (not linked): %s", strings.Join(res.LinksMissing, ", "))))
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Bootstrapped %s", StylePath.Render(res.WorktreePath))))
}

func runList(cmd *cobra.Command, args []string) {
	mgr := getManager()
	jsonOut, _ := cmd.Flags().GetBool("json")

	repoDone := startProfileStage("list_require_repo")
	repoRoot, err := mgr.RequireRepo()
	repoDone()
	if err != nil {
		if errors.Is(err, ErrNotGitRepo) {
			fmt.Fprintln(os.Stderr, "error: run this command inside a git worktree")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	loadDone := startProfileStage("list_load_worktrees")
	items, err := listWorktreesFast(mgr, repoRoot)
	loadDone()
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

func runPreview(cmd *cobra.Command, args []string) {
	// No target: show status. With a target: promote it.
	if len(args) == 0 {
		printPreviewStatus(getManager(), false)
		return
	}
	mgr := getManager()
	attach, _ := cmd.Flags().GetBool("attach")
	st, err := mgr.PromotePreview(PreviewOptions{Target: args[0], Attach: attach})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Preview now running from %s (%s)", StyleBranch.Render(st.Branch), StylePath.Render(st.Path))))
	printPreviewServiceURLs(mgr)
	printPreviewSyncWarnings(st.SyncWarnings)
}

func runPreviewSync(cmd *cobra.Command, args []string) {
	mgr := getManager()
	st, err := mgr.SyncPreview("")
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Preview configs synced for %s (%s)", StyleBranch.Render(st.Branch), StylePath.Render(st.Path))))
	printPreviewSyncWarnings(st.SyncWarnings)
}

// printPreviewSyncWarnings surfaces non-fatal preview-sync messages.
func printPreviewSyncWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Println(WarnMsg(w))
	}
}

func runPreviewStatus(cmd *cobra.Command, args []string) {
	jsonOut, _ := cmd.Flags().GetBool("json")
	printPreviewStatus(getManager(), jsonOut)
}

func runPreviewStop(cmd *cobra.Command, args []string) {
	mgr := getManager()
	stopped, err := mgr.StopPreview("")
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if stopped {
		fmt.Println(SuccessMsg("Preview stopped"))
	} else {
		fmt.Println(InfoMsg("Preview was not running"))
	}
}

func runPreviewRestart(cmd *cobra.Command, args []string) {
	mgr := getManager()
	attach, _ := cmd.Flags().GetBool("attach")
	st, err := mgr.RestartPreview(PreviewOptions{Attach: attach})
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg(fmt.Sprintf("Preview restarted from %s (%s)", StyleBranch.Render(st.Branch), StylePath.Render(st.Path))))
	printPreviewServiceURLs(mgr)
	printPreviewSyncWarnings(st.SyncWarnings)
}

func runPreviewLogs(cmd *cobra.Command, args []string) {
	mgr := getManager()
	lines, _ := cmd.Flags().GetInt("lines")
	out, err := mgr.PreviewServiceOutput("", args[0], lines)
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
	}
}

func printPreviewStatus(mgr *Manager, jsonOut bool) {
	repoRoot, err := mgr.RequireRepo()
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	st, services, err := mgr.PreviewStatus(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}

	if jsonOut {
		payload := struct {
			Preview  *PreviewState    `json:"preview"`
			Services []PreviewService `json:"services"`
		}{Preview: st, Services: services}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if st == nil {
		fmt.Println(InfoMsg("No preview worktree set. Run 'sprout preview <target>' to start one."))
	} else {
		fmt.Println(SuccessMsg(fmt.Sprintf("Preview: %s (%s)", StyleBranch.Render(st.Branch), StylePath.Render(st.Path))))
	}
	if len(services) == 0 {
		fmt.Println(StyleDim.Render("No preview services configured. Add [[preview_windows]] to .sprout.toml."))
		return
	}
	for _, svc := range services {
		state := StyleDim.Render("stopped")
		if svc.Running {
			state = StyleClean.Render("running")
		}
		line := fmt.Sprintf("  %s  %s", state, svc.Name)
		if svc.URL != "" {
			line += "  " + StylePath.Render(svc.URL)
		}
		fmt.Println(line)
	}
}

// printPreviewServiceURLs lists the configured service URLs. These come straight
// from config, so it avoids re-scanning tmux state right after a promote.
func printPreviewServiceURLs(mgr *Manager) {
	for i, win := range mgr.Cfg.PreviewWindows {
		if url := strings.TrimSpace(win.URL); url != "" {
			fmt.Println(StyleDim.Render(fmt.Sprintf("  %s → %s", trimWindowConfigName(win, i), url)))
		}
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

func runDaemonStart(cmd *cobra.Command, args []string) {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if err := daemonStartBackground(cfg); err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg("daemon running"))
}

func runDaemonStop(cmd *cobra.Command, args []string) {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if err := daemonStopBackground(cfg); err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	fmt.Println(SuccessMsg("daemon stopped"))
}

func runDaemonStatus(cmd *cobra.Command, args []string) {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	state, err := daemonStateString(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	switch state {
	case "running":
		fmt.Println(SuccessMsg("daemon running"))
	case "starting":
		fmt.Println(WarnMsg("daemon starting"))
	default:
		fmt.Println(InfoMsg("daemon stopped"))
	}
}

func runDaemonRun(cmd *cobra.Command, args []string) {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
	if err := daemonRunForeground(cfg); err != nil {
		fmt.Fprintln(os.Stderr, ErrorMsg(err.Error()))
		os.Exit(1)
	}
}
