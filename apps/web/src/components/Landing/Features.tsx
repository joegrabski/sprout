import React from "react";
import styles from "../../pages/index.module.css";

const FEATURES = [
  {
    n: "01",
    title: "See everything at once",
    description:
      "A TUI showing all your worktrees with live status, active sessions, and git diffs, without leaving the terminal.",
  },
  {
    n: "02",
    title: "Tailored multi-pane sessions",
    description:
      "Per-project multi-pane layouts, auto-running dev servers, and a consistent toolset, scaffolded from one TOML rule.",
  },
  {
    n: "03",
    title: "An AI agent per branch",
    description:
      "Run Codex, Aider, Claude Code, or Gemini inside each worktree. Each agent keeps its own independent context.",
  },
  {
    n: "04",
    title: "Work in parallel, ship faster",
    description:
      "Tackle a hotfix, a feature, and a review at the same time. No stashing. No branch juggling. Every worktree stays ready.",
  },
  {
    n: "05",
    title: "Always a clean repo root",
    description:
      "Worktrees live in a sibling directory. Your main checkout stays clean and easy to navigate.",
  },
  {
    n: "06",
    title: "Feels native to your shell",
    description:
      "Shell hooks for zsh, bash, and fish let you navigate worktrees exactly like any other directory. Preview panes load your full shell profile, so your tools are always on PATH.",
  },
  {
    n: "07",
    title: "Preview any branch, live",
    description:
      "Promote any worktree to a live preview: your whole stack (APIs, web apps, mobile bundler) runs from that branch in a dedicated session. Switch which branch is live with one command.",
  },
  {
    n: "08",
    title: "Configs that follow your tunnels",
    description:
      "Expose a service through a tunnel and Sprout rewrites the URLs into your app config on every switch. No stale tunnel URLs, no hand-editing, no rebuilds.",
  },
];

export function FeaturesSection() {
  return (
    <section className={styles.featuresSection}>
      <div className="container">
        <div className={styles.featuresLayout}>
          <div className={styles.featuresIntro}>
            <h2 className={styles.sectionTitle}>
              Everything your workflow needs
            </h2>
            <p className={styles.featuresIntroSubtitle}>
              Sprout handles the overhead so you stay in flow.
            </p>
          </div>
          <div className={`${styles.featuresGrid} ${styles.voxelSurface}`} data-anim>
            <div className={styles.voxelInner}>
            {FEATURES.map((f) => (
              <div key={f.n} className={styles.featureItem}>
                <span className={styles.featureNum}>{f.n}</span>
                <h3 className={styles.featureTitle}>{f.title}</h3>
                <p className={styles.featureDescription}>{f.description}</p>
              </div>
            ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
