import React, { useEffect, useState } from "react";
import clsx from "clsx";
import { Terminal } from "lucide-react";
import styles from "../../pages/index.module.css";

const TUI_SCENES = [
  { worktree: "feat/checkout", rowIndex: 1 },
  { worktree: "fix/auth-bug", rowIndex: 2 },
  { worktree: "chore/cleanup", rowIndex: 3 },
];

const TUI_ROWS = [
  { label: "main", status: "dirty", tmux: "yes" },
  { label: "feat/checkout", status: "dirty", tmux: "yes" },
  { label: "fix/auth-bug", status: "dirty", tmux: "yes" },
  { label: "chore/cleanup", status: "clean", tmux: "no" },
];

export function TUISection() {
  const [step, setStep] = useState(0);
  const [visible, setVisible] = useState(true);

  const scene = TUI_SCENES[step];

  useEffect(() => {
    const id = setInterval(() => {
      setVisible(false);
      setTimeout(() => {
        setStep((s) => (s + 1) % TUI_SCENES.length);
        setVisible(true);
      }, 250);
    }, 5500);
    return () => clearInterval(id);
  }, []);

  return (
    <section className={styles.tuiSection}>
      <div className="container">
        <div className={styles.tuiLayout} data-anim>
          <div className={styles.tuiCopy}>
            <h2 className={styles.sectionTitle}>
              A TUI built for <span className={styles.titleGradient}>parallel work.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Run <code>sprout ui</code> for a three-pane interface: every branch,
              its tmux session, and the current git diff, all visible at once.
            </p>
            <ul className={styles.tuiPoints}>
              <li>Jump between worktrees with a single keypress</li>
              <li>Scan dirty branches and active tmux sessions at a glance</li>
              <li>Review diffs in place, then attach to tmux for the full session</li>
            </ul>
          </div>

          <div className={`${styles.tuiFrame} ${styles.voxelSurface}`}>
            <div className={styles.voxelInner}>
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
                <div className={styles.tuiPaneBar}>
                  <div className={styles.tuiPaneBarLine} />
                  <span>[1]-Status</span>
                  <div className={styles.tuiPaneBarLine} />
                </div>
                <div className={styles.tuiPaneContent}>
                  <span className={styles.tuiGreen}>✓</span> <span className={styles.tuiMuted}>myapp</span>
                  <span className={styles.tuiDim}> -&gt; </span>
                  <span className={styles.tuiGreen}>main</span>
                  <span className={styles.tuiDim}>{"  "}selected: </span>
                  <span className={styles.tuiGreen} style={{ transition: "opacity 0.25s", opacity: visible ? 1 : 0 }}>
                    {scene.worktree}
                  </span>
                </div>

                <div className={styles.tuiPaneBar}>
                  <div className={styles.tuiPaneBarLine} />
                  <span>[2]-Details</span>
                  <div className={styles.tuiPaneBarLine} />
                </div>
                <div className={styles.tuiPaneContent}>
                  <div className={styles.tuiTabs}>
                    <span className={styles.tuiTabActive}>GIT DIFF</span>
                  </div>
                  <div className={styles.tuiDetailPane}>
                    <div
                      className={clsx(styles.tuiDetailBody, styles.tuiDetailBodyTop)}
                      style={{ opacity: visible ? 1 : 0, transition: "opacity 0.25s" }}
                    >
                      {step === 0 ? (
                        <>
                          <div className={styles.tuiDim}>src/hooks/useCart.ts</div>
                          <div><span className={styles.tuiRed}>-{"  "}const total = cart.total</span></div>
                          <div><span className={styles.tuiGreen}>+{"  "}const total = cart?.total ?? 0</span></div>
                          <div className={styles.tuiDim}>&nbsp;</div>
                          <div className={styles.tuiDim}>src/components/Cart.tsx</div>
                          <div><span className={styles.tuiGreen}>+{"  "}if (!items) return null</span></div>
                        </>
                      ) : step === 1 ? (
                        <>
                          <div className={styles.tuiDim}>src/auth/token.ts</div>
                          <div><span className={styles.tuiRed}>-{"  "}refreshToken()</span></div>
                          <div><span className={styles.tuiGreen}>+{"  "}if (!isRefreshing) {"{"}</span></div>
                          <div><span className={styles.tuiGreen}>+{"    "}refreshToken()</span></div>
                          <div><span className={styles.tuiGreen}>+{"  "}{"}"}</span></div>
                        </>
                      ) : (
                        <>
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
                    <span>TMUX</span>
                  </div>
                  {TUI_ROWS.map((row, i) => (
                    <div
                      key={row.label}
                      className={clsx(
                        styles.tuiTableRowCustom,
                        i === scene.rowIndex && styles.tuiTableRowSelected,
                      )}
                    >
                      <span className={i === 0 ? styles.tuiGreen : undefined}>
                        {i === 0 ? "*" : ""}
                      </span>
                      <span className={styles.tuiMuted}>{row.label}</span>
                      <span className={row.status === "dirty" ? styles.tuiRed : styles.tuiGreen}>
                        {row.status}
                      </span>
                      <span className={row.tmux === "yes" ? styles.tuiGreen : styles.tuiDim}>
                        {row.tmux}
                      </span>
                    </div>
                  ))}
                </div>

                <div className={styles.tuiBottomBar}>
                  <span className={styles.tuiDim}>
                    ╰─ j/k move │ p preview │ enter attach │ tab pane │ ? help   INFO: ready
                  </span>
                  <span className={styles.tuiDim}>─ sprout ╯</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
