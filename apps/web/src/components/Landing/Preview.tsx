import React, { useEffect, useState } from "react";
import { MonitorSmartphone } from "lucide-react";
import styles from "../../pages/index.module.css";

const PREVIEW_SCENES = [
  { branch: "feat/checkout-redesign", url: "https://a1b2c3.ngrok.app" },
  { branch: "fix/login-bug", url: "https://d4e5f6.ngrok.app" },
  { branch: "chore/upgrade-deps", url: "https://7a8b9c.ngrok.app" },
];

export function PreviewSection() {
  const [step, setStep] = useState(0);
  const [visible, setVisible] = useState(true);

  const scene = PREVIEW_SCENES[step];

  useEffect(() => {
    const id = setInterval(() => {
      setVisible(false);
      setTimeout(() => {
        setStep((s) => (s + 1) % PREVIEW_SCENES.length);
        setVisible(true);
      }, 300);
    }, 4500);
    return () => clearInterval(id);
  }, []);

  const fade = {
    opacity: visible ? 1 : 0,
    transition: "opacity 0.3s ease",
  } as const;

  return (
    <section id="preview" className={styles.tmuxSection}>
      <div className="container">
        <div className={styles.tmuxLayout} data-anim>
          <div className={styles.tmuxTerminal}>
            <div className={`${styles.tmuxFrame} ${styles.voxelSurface}`}>
              <div
                className={styles.voxelInner}
                style={{ flex: 1, display: "flex", flexDirection: "column" }}
              >
                <div className={styles.terminalChrome}>
                  <div className={styles.terminalDots}>
                    <span className={styles.terminalDot} style={{ background: "#ff5f57" }} />
                    <span className={styles.terminalDot} style={{ background: "#febc2e" }} />
                    <span className={styles.terminalDot} style={{ background: "#28c840" }} />
                  </div>
                  <div className={styles.terminalWindowTitle}>
                    <MonitorSmartphone size={12} />
                    <span>sprout preview</span>
                  </div>
                </div>
                <div className={styles.terminalBody}>
                  <div className={styles.terminalLine}>
                    <span className={styles.terminalPrompt}>$</span>
                    <span className={styles.terminalCommand}>
                      sprout preview{" "}
                      <span style={fade}>{scene.branch}</span>
                    </span>
                  </div>
                  <div className={styles.terminalOutput}>
                    <div className={styles.terminalSuccess} style={fade}>
                      ✓ Preview now running from {scene.branch}
                    </div>
                    <div className={styles.terminalPath} style={fade}>
                      &nbsp;&nbsp;api → {scene.url}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div className={styles.tmuxCopy}>
            <h2 className={styles.sectionTitle}>
              Preview any branch.{" "}
              <span className={styles.titleGradient}>Configs follow.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Promote any worktree to a live preview and Sprout runs your whole
              stack from it. Expose a service through ngrok and Sprout rewrites
              the URL into your app config whenever it changes — no editing, no
              rebuilds.
            </p>
            <div className={`${styles.tmuxConfigBox} ${styles.voxelSurface}`}>
              <div className={styles.voxelInner}>
                <div className={styles.tmuxConfigHeader}>.sprout.toml</div>
                <pre className={styles.tmuxConfigCode}>
                  {`[[preview_sync]]
file = "{worktree}/web/config.json"
reload_windows = ["web"]

  [[preview_sync.set]]
  path = "api.baseUrl"
  tunnel = "api"`}
                </pre>
              </div>
            </div>
            <ul className={styles.tuiPoints}>
              <li>Run your real stack against any agent's branch</li>
              <li>Tunnel URLs synced into your config automatically</li>
              <li>Switch previews with one command, no rebuilds</li>
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}
