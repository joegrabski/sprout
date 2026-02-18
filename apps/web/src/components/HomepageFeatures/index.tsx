import Heading from "@theme/Heading";
import clsx from "clsx";
import type { ReactNode } from "react";
import styles from "./styles.module.css";

type FeatureItem = {
  title: string;
  icon: string;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: "Interactive TUI",
    icon: "🖥️",
    description: (
      <>
        Beautiful terminal interface for managing all your worktrees. See
        status, active sessions, and running agents at a glance.
      </>
    ),
  },
  {
    title: "Tmux Integration",
    icon: "🪟",
    description: (
      <>
        Automatic session management with Neovim and Lazygit. Each worktree gets
        its own isolated development environment.
      </>
    ),
  },
  {
    title: "AI Agent Support",
    icon: "🤖",
    description: (
      <>
        Built-in integration with Codex, Aider, Claude Code, and Gemini. Each
        worktree can have its own AI coding assistant.
      </>
    ),
  },
  {
    title: "Zero Context Switching",
    icon: "⚡",
    description: (
      <>
        Work on multiple branches simultaneously without losing your flow. No
        more stashing or worrying about uncommitted changes.
      </>
    ),
  },
  {
    title: "Smart Organization",
    icon: "📁",
    description: (
      <>
        Automatic worktree directory management with intuitive branch naming.
        Keeps your repository root clean and worktrees easy to find.
      </>
    ),
  },
  {
    title: "Shell Integration",
    icon: "🐚",
    description: (
      <>
        Seamless directory switching via shell hooks. Navigate between worktrees
        effortlessly from your terminal.
      </>
    ),
  },
];

function Feature({ title, icon, description }: FeatureItem) {
  return (
    <div className={clsx("col col--4")}>
      <div className="text--center">
        <div className={styles.featureIcon}>{icon}</div>
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
