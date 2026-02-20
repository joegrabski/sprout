import React, { useState, useEffect } from "react";
import { Bot, GitBranch, Terminal, Zap } from "lucide-react";
import styles from "../../pages/index.module.css";

export function TmuxSection() {
  const [activeWindow, setActiveWindow] = useState(0);
  const [visible, setVisible] = useState(true);

  const windows = [
    {
      id: 0,
      label: "0:main*",
      header: "neovim + server",
      content: (
        <div className={styles.tmuxWindow}>
          <div className={styles.tmuxPane} style={{ flex: 2 }}>
            <div className={styles.tmuxPaneHeader}>
              <Terminal size={10} />
              <span>neovim</span>
            </div>
            <div className={styles.tmuxPaneContent}>
              <div className={styles.tuiDim}>1 func main() {"{"}</div>
              <div className={styles.tuiDim}>2   fmt.Println("hello sprout")</div>
              <div className={styles.tuiDim}>3 {"}"}</div>
              <div className={styles.tuiGreen} style={{ marginTop: "auto" }}>
                -- INSERT --
              </div>
            </div>
          </div>
          <div className={styles.tmuxDivider} />
          <div className={styles.tmuxPane} style={{ flex: 1 }}>
            <div className={styles.tmuxPaneHeader}>
              <Zap size={10} />
              <span>dev server</span>
            </div>
            <div className={styles.tmuxPaneContent}>
              <div className={styles.tuiGreen}>[ready] listening on :8080</div>
              <div className={styles.tuiMuted}>GET /api/health 200 OK</div>
              <div className={styles.tuiMuted}>POST /api/worktrees 201 Created</div>
            </div>
          </div>
        </div>
      ),
    },
    {
      id: 1,
      label: "1:agent",
      header: "codex agent",
      content: (
        <div className={styles.tmuxWindow}>
          <div className={styles.tmuxPane} style={{ flex: 1 }}>
            <div className={styles.tmuxPaneHeader}>
              <Bot size={10} />
              <span>codex agent</span>
            </div>
            <div className={styles.tmuxPaneContent}>
              <div>
                <span className={styles.tuiDim}>&gt; </span>
                <span className={styles.tuiMuted}>
                  Thinking about the architecture...
                </span>
              </div>
              <div>
                <span className={styles.tuiDim}>&gt; </span>
                <span className={styles.tuiMuted}>
                  Applying changes to internal/sprout/manager.go
                </span>
              </div>
              <div className={styles.tuiGreen}>
                ✓ Done. Ready for next instruction.
              </div>
            </div>
          </div>
        </div>
      ),
    },
    {
      id: 2,
      label: "2:git",
      header: "lazygit",
      content: (
        <div className={styles.tmuxWindow}>
          <div className={styles.tmuxPane} style={{ flex: 1 }}>
            <div className={styles.tmuxPaneHeader}>
              <GitBranch size={10} />
              <span>lazygit</span>
            </div>
            <div className={styles.tmuxPaneContent}>
              <div className={styles.tuiGreen}>Staged Changes</div>
              <div className={styles.tuiMuted}>
                {"  "}M{"  "}internal/sprout/config.go
              </div>
              <div style={{ marginTop: "1rem" }}>Unstaged Changes</div>
              <div className={styles.tuiRed}>
                {"  "}M{"  "}internal/sprout/ui.go
              </div>
            </div>
          </div>
        </div>
      ),
    },
  ];

  useEffect(() => {
    const id = setInterval(() => {
      setVisible(false);
      setTimeout(() => {
        setActiveWindow((w) => (w + 1) % windows.length);
        setVisible(true);
      }, 300);
    }, 4500);
    return () => clearInterval(id);
  }, [windows.length]);

  return (
    <section className={styles.tmuxSection}>
      <div className="container">
        <div className={styles.tmuxLayout}>
          <div className={styles.tmuxTerminal}>
            <div className={styles.tmuxFrame}>
              <div
                style={{
                  flex: 1,
                  display: "flex",
                  flexDirection: "column",
                  opacity: visible ? 1 : 0,
                  transition: "opacity 0.3s ease",
                }}
              >
                {windows[activeWindow].content}
              </div>
              {/* Tmux Status Bar */}
              <div className={styles.tmuxStatus}>
                <div className={styles.tmuxStatusLeft}>[sprout-feat]</div>
                <div className={styles.tmuxTabs}>
                  {windows.map((w) => (
                    <span
                      key={w.id}
                      className={
                        activeWindow === w.id
                          ? styles.tmuxTabActive
                          : styles.tmuxTab
                      }
                    >
                      {w.label}
                    </span>
                  ))}
                </div>
                <div className={styles.tmuxStatusRight}>Feb 20 14:30</div>
              </div>
            </div>
          </div>

          <div className={styles.tmuxCopy}>
            <span className={styles.sectionLabel}>Customization</span>
            <h2 className={styles.sectionTitle}>
              Configure your ideal environment.
            </h2>
            <p className={styles.tuiDescription}>
              Define every window and pane exactly how you work. Sprout scaffolds
              your entire workspace using simple TOML rules — from code editors
              to auto-running development servers.
            </p>
            <div className={styles.tmuxConfigBox}>
              <div className={styles.tmuxConfigHeader}>.sprout.toml</div>
              <pre className={styles.tmuxConfigCode}>
                {`# Match the layout shown on the left
[[windows]]
name = "main"
layout = "main-horizontal"
panes = [
  { run = "nvim ." },
  { run = "go run main.go" }
]

[[windows]]
name = "agent"
panes = [{ run = "codex" }]

[[windows]]
name = "git"
panes = [{ run = "lazygit" }]`}
              </pre>
            </div>
            <ul className={styles.tuiPoints}>
              <li>Define project-specific split layouts</li>
              <li>Auto-run your dev server or logs on launch</li>
              <li>Perfectly consistent environments every time</li>
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}
