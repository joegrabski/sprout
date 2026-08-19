import React from "react";
import BrowserOnly from "@docusaurus/BrowserOnly";
import Link from "@docusaurus/Link";
import Heading from "@theme/Heading";
import clsx from "clsx";
import { ArrowRight, Command } from "lucide-react";
import { InstallCommand } from "./Install";
import Mascot from "./Mascot";
import styles from "../../pages/index.module.css";

export function HomepageHeader() {
  return (
    <header id="hero" className={styles.heroBanner}>
      <div className="container">
        <div className={styles.heroContent} data-anim>
          <div className={styles.heroMascot} role="img" aria-label="Sprout">
            <BrowserOnly>{() => <Mascot />}</BrowserOnly>
          </div>

          <Heading as="h1" className={styles.heroTitle}>
            <span className={styles.titleLine}>Split your work.</span>
            <span className={styles.titleLine}>
              <span className={styles.titleGradient}>Not your focus.</span>
              <span className={styles.titleCursor} aria-hidden="true" />
            </span>
          </Heading>

          <p className={styles.heroSubtitle}>
            Every branch gets its own worktree, tmux session, and AI agent, so
            you can work in parallel without ever losing your place.
          </p>

          <InstallCommand />

          <div className={styles.heroButtons}>
            <Link
              className={clsx("button button--lg", styles.primaryButton)}
              to="/docs/intro"
            >
              <span>Get started</span>
              <ArrowRight size={17} />
            </Link>
            <Link
              className={clsx("button button--lg", styles.secondaryButton)}
              to="/docs/intro"
            >
              <Command size={16} />
              <span>Read the docs</span>
            </Link>
          </div>

          <p className={styles.heroMeta}>
            git 2.5+ &nbsp;·&nbsp; tmux &nbsp;·&nbsp; macos &amp; linux
            &nbsp;·&nbsp; mit
          </p>
        </div>
      </div>
    </header>
  );
}
