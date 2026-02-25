import React from "react";
import Link from "@docusaurus/Link";
import clsx from "clsx";
import { ArrowRight, GitBranch } from "lucide-react";
import { DitheredBackdrop } from "./DitheredBackdrop";
import styles from "../../pages/index.module.css";

export function CTASection() {
  return (
    <section className={styles.ctaSection}>
      <div className="container">
        <div className={`${styles.ctaShell} ${styles.voxelSurface}`} data-anim>
          <DitheredBackdrop className={styles.ditherEdge} variant="cta" />
          <div className={styles.voxelInner}>
            <div className={styles.ctaDivider}>
              <div className={styles.ctaDiamond} />
            </div>
            <h2 className={styles.ctaTitle}>Stop stashing. Start sprouting.</h2>
            <p className={styles.ctaSubtitle}>
              brew tap joegrabski/sprout &amp;&amp; brew install sprout
            </p>
            <div className={styles.ctaButtons}>
              <Link
                className={clsx("button button--lg", styles.primaryButton)}
                to="/docs/intro"
              >
                <span>Get started</span>
                <ArrowRight size={17} />
              </Link>
              <Link
                className={clsx("button button--lg", styles.secondaryButton)}
                to="https://github.com/joegrabski/sprout"
              >
                <GitBranch size={17} />
                <span>View on GitHub</span>
              </Link>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
