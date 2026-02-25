import React, { ReactNode } from "react";
import { GitBranch, Terminal, Zap } from "lucide-react";
import { DitheredBackdrop } from "./DitheredBackdrop";
import styles from "../../pages/index.module.css";

type DemoCardProps = {
  icon: any;
  title: string;
  children: ReactNode;
};

function DemoCard({ icon: Icon, title, children }: DemoCardProps) {
  return (
    <div className={`${styles.demoCard} ${styles.voxelSurface}`}>
      <DitheredBackdrop className={styles.ditherEdge} variant="section" />
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
            <Icon size={12} />
            <span>{title}</span>
          </div>
        </div>
        <div className={styles.terminalBody}>{children}</div>
      </div>
    </div>
  );
}

export function DemoSection() {
  return (
    <section className={styles.demoSection}>
      <div className="container">
        <div className={styles.demoHeader} data-anim>
          <div className={styles.demoHeaderLeft}>
            <span className={styles.sectionLabel}>In practice</span>
            <h2 className={styles.sectionTitle}>A few commands.</h2>
          </div>
          <p className={styles.demoHeaderRight}>
            Simple, memorable commands that stay out of your way.
          </p>
        </div>

        <div className={styles.demoGrid} data-anim>
          <DemoCard icon={GitBranch} title="create a worktree">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>
                sprout new feat checkout-redesign
              </span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalSuccess}>✓ branch created</div>
              <div className={styles.terminalSuccess}>
                ✓ tmux session launched
              </div>
              <div className={styles.terminalSuccess}>✓ agent ready</div>
            </div>
          </DemoCard>

          <DemoCard icon={Terminal} title="list all worktrees">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>sprout list</span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalTable}>
                <div>
                  <span className={styles.terminalCurrent}>*</span>{" "}
                  feat/checkout-redesign{" "}
                  <span className={styles.terminalActive}>active</span>
                </div>
                <div>
                  &nbsp; main{" "}
                  <span className={styles.terminalInactive}>clean</span>
                </div>
                <div>
                  &nbsp; fix/login-bug{" "}
                  <span className={styles.terminalInactive}>clean</span>
                </div>
              </div>
            </div>
          </DemoCard>

          <DemoCard icon={Zap} title="switch instantly">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>
                sprout go fix/login-bug
              </span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalPath}>
                ~/projects/myapp.worktrees/fix-login-bug
              </div>
            </div>
          </DemoCard>
        </div>
      </div>
    </section>
  );
}
