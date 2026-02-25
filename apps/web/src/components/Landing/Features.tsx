import React from "react";
import { DitheredBackdrop } from "./DitheredBackdrop";
import styles from "../../pages/index.module.css";

const FEATURES = [
  {
    n: "01",
    title: "See everything at once",
    description:
      "A TUI showing all your worktrees with live status, active sessions, git state, and running agents, without leaving the terminal.",
  },
  {
    n: "02",
    title: "Tailored multi-pane sessions",
    description:
      "Every worktree gets its own tmux session. Configure project-specific multi-pane layouts, auto-running dev servers, and consistent toolsets in one TOML rule.",
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
      "Shell hooks for zsh, bash, and fish let you navigate worktrees exactly like any other directory.",
  },
];

export function FeaturesSection() {
  return (
    <section className={styles.featuresSection}>
      <div className="container">
        <div className={styles.featuresLayout}>
          <div className={styles.featuresIntro}>
            <span className={styles.sectionLabel}>Features</span>
            <h2 className={styles.sectionTitle}>
              Everything your workflow needs
            </h2>
            <p className={styles.featuresIntroSubtitle}>
              Sprout handles the overhead so you stay in flow.
            </p>
          </div>
          <div className={`${styles.featuresGrid} ${styles.voxelSurface}`} data-anim>
            <DitheredBackdrop className={styles.ditherEdge} variant="section" />
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
