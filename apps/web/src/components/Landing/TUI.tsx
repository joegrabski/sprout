import React, { useState, useEffect } from "react";
import clsx from "clsx";
import { Terminal } from "lucide-react";
import styles from "../../pages/index.module.css";

type AgentLine =
  | { t: "cmd"; text: string }
  | { t: "thought"; text: string }
  | { t: "success"; text: string }
  | { t: "blank" };

const TUI_AGENT_LINES: AgentLine[][] = [
  // feat/checkout
  [
    { t: "cmd", text: "Reading src/hooks/useCart.ts..." },
    { t: "blank" },
    { t: "thought", text: "The cart total isn't guarded, null on" },
    { t: "thought", text: "first render will throw. Adding optional" },
    { t: "thought", text: "chaining and a zero fallback." },
    { t: "blank" },
    { t: "cmd", text: "Editing src/hooks/useCart.ts..." },
    { t: "success", text: "Patched: cart?.total ?? 0" },
    { t: "cmd", text: "Running tests..." },
    { t: "success", text: "12 passed, 0 failed" },
  ],
  // fix/auth-bug
  [
    { t: "cmd", text: "Reading src/auth/token.ts..." },
    { t: "blank" },
    { t: "thought", text: "refreshToken fires without checking if" },
    { t: "thought", text: "a refresh is already in progress," },
    { t: "thought", text: "duplicate requests under load. Adding" },
    { t: "thought", text: "an isRefreshing guard." },
    { t: "blank" },
    { t: "cmd", text: "Editing src/auth/token.ts..." },
    { t: "success", text: "Added guard on line 47" },
    { t: "success", text: "8 passed, 0 failed" },
  ],
  // chore/cleanup
  [
    { t: "cmd", text: "Scanning for unused imports..." },
    { t: "blank" },
    { t: "thought", text: "Found 3 files with dead imports." },
    { t: "thought", text: "Removing them to keep the bundle lean." },
    { t: "blank" },
    { t: "cmd", text: "Editing 3 files..." },
    { t: "success", text: "Removed 12 unused imports" },
    { t: "success", text: "0 warnings, 0 errors" },
  ],
];

const TUI_SCENES = [
  {
    worktree: "feat/checkout",
    agentStatus: "ready",
    rowIndex: 1,
  },
  {
    worktree: "fix/auth-bug",
    agentStatus: "busy",
    rowIndex: 2,
  },
  {
    worktree: "chore/cleanup",
    agentStatus: "running",
    rowIndex: 3,
  },
];

const TUI_ROWS = [
  { label: "main", status: "dirty", agent: "n/a" },
  { label: "feat/checkout", status: "dirty", agent: "ready" },
  { label: "fix/auth-bug", status: "dirty", agent: "busy" },
  { label: "chore/cleanup", status: "clean", agent: "running" },
];

const TUI_TOTAL_STEPS = TUI_SCENES.length * 2;

export function TUISection() {
  const [step, setStep] = useState(0);
  const [visible, setVisible] = useState(true);

  const sceneIdx = Math.floor(step / 2);
  const tab = step % 2 === 0 ? "agent" : "diff";
  const scene = TUI_SCENES[sceneIdx];

  const [visibleLines, setVisibleLines] = useState(0);

  useEffect(() => {
    const id = setInterval(() => {
      setVisible(false);
      setTimeout(() => {
        setStep((s) => (s + 1) % TUI_TOTAL_STEPS);
        setVisible(true);
      }, 250);
    }, 5500);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    setVisibleLines(0);
    if (tab !== "agent") return;
    const total = TUI_AGENT_LINES[sceneIdx].length;
    let n = 0;
    const id = setInterval(() => {
      n++;
      setVisibleLines(n);
      if (n >= total) clearInterval(id);
    }, 160);
    return () => clearInterval(id);
  }, [step]);

  return (
    <section className={styles.tuiSection}>
      <div className="container">
        <div className={styles.tuiLayout}>
          <div className={styles.tuiCopy}>
            <span className={styles.sectionLabel}>Interface</span>
            <h2 className={styles.sectionTitle}>
              A TUI built for <span className={styles.titleGradient}>parallel work.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Run <code>sprout ui</code> for a three-pane interface: every branch, its tmux session, git state, and running agent, all visible at once.
            </p>
            <ul className={styles.tuiPoints}>
              <li>Jump between worktrees with a single keypress</li>
              <li>Watch live agent output and git diffs side by side</li>
              <li>Start, stop, and attach to agents without switching tabs</li>
            </ul>
          </div>

          <div className={styles.tuiFrame}>
            <div className={styles.terminalChrome}>
              <div className={styles.terminalDots}>
                <span className={styles.terminalDot} style={{ background: "#ff5f57" }} />
                <span className={styles.terminalDot} style={{ background: "#febc2e" }} />
                <span className={styles.terminalDot} style={{ background: "#28c840" }} />
              </div>
              <div className={styles.terminalWindowTitle}>
                <Terminal size={12} />
                <span>sprout ui</span>
              </div>
            </div>
            <div className={styles.tuiBody}>
              {/* Pane 1: Status */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[1]-Status</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <span className={styles.tuiGreen}>✓</span> <span className={styles.tuiMuted}>myapp</span>
                <span className={styles.tuiDim}> → </span>
                <span className={styles.tuiGreen}>main</span>
                <span className={styles.tuiDim}>{"  "}selected: </span>
                <span className={styles.tuiGreen} style={{ transition: "opacity 0.25s", opacity: visible ? 1 : 0 }}>
                  {scene.worktree}
                </span>
                <span className={styles.tuiDim}>{"  "}agent: </span>
                <span className={styles.tuiGreen}>{scene.agentStatus}</span>
              </div>

              {/* Pane 2: Details */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[2]-Details</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <div className={styles.tuiTabs}>
                  <span className={tab === "agent" ? styles.tuiTabActive : styles.tuiTabInactive}>AGENT OUTPUT</span>
                  <span className={styles.tuiDim}> │ </span>
                  <span className={tab === "diff" ? styles.tuiTabActive : styles.tuiTabInactive}>GIT DIFF</span>
                </div>
                <div className={styles.tuiDetailPane}>
                  <div
                    className={clsx(styles.tuiDetailBody, tab === "diff" && styles.tuiDetailBodyTop)}
                    style={{ opacity: visible ? 1 : 0, transition: "opacity 0.25s" }}
                  >
                    {tab === "agent" ? (
                      TUI_AGENT_LINES[sceneIdx].slice(0, visibleLines).map((line, i) =>
                        line.t === "blank" ? (
                          <div key={i}>&nbsp;</div>
                        ) : line.t === "cmd" ? (
                          <div key={i}>
                            <span className={styles.tuiDim}>&gt; </span>
                            <span className={styles.tuiMuted}>{line.text}</span>
                          </div>
                        ) : line.t === "success" ? (
                          <div key={i}>
                            <span className={styles.tuiGreen}>✓ </span>
                            <span className={styles.tuiDim}>{line.text}</span>
                          </div>
                        ) : (
                          <div key={i} className={styles.tuiDim}>{line.text}</div>
                        )
                      )
                    ) : (
                      sceneIdx === 0 ? <>
                        <div className={styles.tuiDim}>src/hooks/useCart.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}const total = cart.total</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}const total = cart?.total ?? 0</span></div>
                        <div className={styles.tuiDim}>&nbsp;</div>
                        <div className={styles.tuiDim}>src/components/Cart.tsx</div>
                        <div><span className={styles.tuiGreen}>+{"  "}if (!items) return null</span></div>
                      </> : sceneIdx === 1 ? <>
                        <div className={styles.tuiDim}>src/auth/token.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}refreshToken()</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}if (!isRefreshing) {"{"}</span></div>
                        <div><span className={styles.tuiGreen}>+{"    "}refreshToken()</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}{"}"}</span></div>
                      </> : <>
                        <div className={styles.tuiDim}>src/utils/format.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ debounce }"} from 'lodash'</span></div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ merge }"} from 'lodash'</span></div>
                        <div className={styles.tuiDim}>&nbsp;</div>
                        <div className={styles.tuiDim}>src/pages/Dashboard.tsx</div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ memo }"} from 'react'</span></div>
                      </>
                    )}
                  </div>
                </div>
              </div>

              {/* Pane 3: Worktrees */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[3]-Worktrees</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <div className={styles.tuiTableHeaderCustom}>
                  <span>CUR</span>
                  <span>BRANCH</span>
                  <span>STATUS</span>
                  <span>AGENT</span>
                </div>
                {TUI_ROWS.map((row, i) => (
                  <div
                    key={row.label}
                    className={clsx(
                      styles.tuiTableRowCustom,
                      i === scene.rowIndex && styles.tuiTableRowSelected,
                    )}
                  >
                    <span className={i === scene.rowIndex ? styles.tuiGreen : undefined}>
                      {i === scene.rowIndex ? "*" : ""}
                    </span>
                    <span className={styles.tuiMuted}>{row.label}</span>
                    <span className={row.status === "dirty" ? styles.tuiRed : styles.tuiGreen}>
                      {row.status}
                    </span>
                    <span className={clsx(
                      row.agent === "ready" && styles.tuiGreen,
                      row.agent === "busy" && styles.tuiYellow,
                      row.agent === "running" && styles.tuiBlue,
                      row.agent === "n/a" && styles.tuiDim
                    )}>
                      {row.agent}
                    </span>
                  </div>
                ))}
              </div>

              <div className={styles.tuiBottomBar}>
                <span className={styles.tuiDim}>└ tab cycle focus │ esc close help │ INFO: ready</span>
                <span className={styles.tuiDim}>
                  {scene.rowIndex + 1} of 4{"  "}─{" "}
                  <span style={{ opacity: visible ? 1 : 0, transition: "opacity 0.25s" }}>{scene.worktree}</span> ↗
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
