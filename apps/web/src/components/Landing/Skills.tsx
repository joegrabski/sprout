import Link from "@docusaurus/Link";
import { Sparkles } from "lucide-react";
import styles from "../../pages/index.module.css";
import { InstallLine } from "./Install";

export function SkillsSection() {
  return (
    <section id="skills" className={styles.tmuxSection}>
      <div className="container">
        <div className={styles.tmuxLayout} data-anim>
          <div className={styles.tmuxTerminal}>
            <div className={`${styles.tmuxFrame} ${styles.voxelSurface}`}>
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
                    <Sparkles size={12} />
                    <span>claude</span>
                  </div>
                </div>
                <div className={styles.terminalBody}>
                  <InstallLine command="sprout skills install" />
                  <div className={styles.terminalOutput}>
                    <div className={styles.terminalSuccess}>
                      ✓ installed sprout-init to ~/.claude/skills
                    </div>
                    <div className={styles.terminalSuccess}>
                      ✓ installed sprout-issue to ~/.claude/skills
                    </div>
                  </div>
                  <div className={styles.terminalLine}>
                    <span className={styles.terminalPrompt}>&gt;</span>
                    <span className={styles.terminalCommand}>
                      /sprout-issue 123
                    </span>
                  </div>
                  <div className={styles.terminalOutput}>
                    <div className={styles.terminalSuccess}>
                      ✓ worktree feature/123-fix-login ready
                    </div>
                    <div className={styles.terminalPath}>
                      &nbsp;&nbsp;agent is planning, waiting for your review
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div className={styles.tmuxCopy}>
            <h2 className={styles.sectionTitle}>
              Skills for your agent.{" "}
              <span className={styles.titleGradient}>One command.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Sprout ships Claude Code skills that drive sprout for you. Point
              one at a GitHub issue and get a worktree with an agent already
              planning it, or let another generate your whole{" "}
              <code>.sprout.toml</code>.
            </p>
            <ul className={styles.tuiPoints}>
              <li>sprout-issue: issue to worktree to planning agent</li>
              <li>sprout-init: detect your stack and write the config</li>
              <li>
                <Link to="/docs/skills">Skills guide</Link>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}
