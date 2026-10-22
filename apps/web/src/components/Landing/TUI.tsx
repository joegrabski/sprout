import clsx from "clsx";
import { Terminal } from "lucide-react";
import type React from "react";
import { useEffect, useState } from "react";
import type { Seg } from "../../../scripts/tui-demo/ansi-to-html";
import styles from "../../pages/index.module.css";
import { TUI_FRAMES } from "./tuiFrames.generated";

// Real frames of `sprout` running against a demo repo. Regenerate them with
// apps/web/scripts/tui-demo/capture.sh after changing the TUI.
const SCENES = [
  { label: "Worktrees & changes", title: "sprout ui — acme-api" },
  { label: "Live agents", title: "sprout ui — agent tab" },
  { label: "Start from an issue or PR", title: "sprout ui — new worktree" },
];

function segStyle(seg: Seg): React.CSSProperties {
  return {
    color: seg.fg,
    background: seg.bg,
    fontWeight: seg.bold ? 700 : undefined,
    opacity: seg.faint ? 0.6 : undefined,
  };
}

export function TUISection() {
  const [scene, setScene] = useState(0);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (paused) return;
    const id = setInterval(
      () => setScene((s) => (s + 1) % TUI_FRAMES.length),
      6500,
    );
    return () => clearInterval(id);
  }, [paused]);

  return (
    <section className={styles.tuiSection}>
      <div className="container">
        <div className={styles.tuiStack} data-anim>
          <div className={styles.tuiCopy}>
            <h2 className={styles.sectionTitle}>
              A TUI built for{" "}
              <span className={styles.titleGradient}>parallel work.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Run <code>sprout</code> to see every worktree at once, with its
              branch, pull request, CI result, tmux session and what its agent
              is doing, then act on it without leaving.
            </p>
            <ul className={styles.tuiPoints}>
              <li>
                Spot the agent that needs input among a dozen that are busy
              </li>
              <li>
                Review, stage, commit or discard changes right next to the diff
              </li>
              <li>
                Watch an agent live, then attach to tmux only when it matters
              </li>
              <li>
                Start a worktree from a branch, an issue or a pull request
              </li>
            </ul>
          </div>

          <div className={`${styles.tuiFrame} ${styles.voxelSurface}`}>
            <div className={styles.voxelInner}>
              <div className={styles.terminalChrome}>
                <div className={styles.terminalDots}>
                  <span
                    className={styles.terminalDot}
                    style={{ background: "#ff5f57" }}
                  />
                  <span
                    className={styles.terminalDot}
                    style={{ background: "#febc2e" }}
                  />
                  <span
                    className={styles.terminalDot}
                    style={{ background: "#28c840" }}
                  />
                </div>
                <div className={styles.terminalWindowTitle}>
                  <Terminal size={12} />
                  <span>{SCENES[scene]?.title}</span>
                </div>
              </div>
              <div className={styles.tuiScreenWrap}>
                <pre
                  className={styles.tuiScreen}
                  role="img"
                  aria-label={`Screenshot of the sprout TUI: ${SCENES[scene]?.label}`}
                >
                  {TUI_FRAMES[scene].map((line, row) => (
                    // Rows are positional: a frame never reorders.
                    // biome-ignore lint/suspicious/noArrayIndexKey: static screenshot rows
                    <div key={row}>
                      {line.length === 0
                        ? " "
                        : line.map((seg, col) => (
                            // biome-ignore lint/suspicious/noArrayIndexKey: static screenshot cells
                            <span key={col} style={segStyle(seg)}>
                              {seg.t}
                            </span>
                          ))}
                    </div>
                  ))}
                </pre>
              </div>
            </div>
          </div>

          <div className={styles.tuiScenes}>
            {SCENES.map((s, i) => (
              <button
                key={s.label}
                type="button"
                aria-pressed={i === scene}
                className={clsx(
                  styles.tuiSceneButton,
                  i === scene && styles.tuiSceneButtonActive,
                )}
                onClick={() => {
                  setScene(i);
                  setPaused(true);
                }}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
