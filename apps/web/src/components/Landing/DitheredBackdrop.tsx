import BrowserOnly from "@docusaurus/BrowserOnly";
import clsx from "clsx";
import React from "react";
import styles from "../../pages/index.module.css";
import BranchDither from "./BranchDither";

type DitheredBackdropProps = {
  className?: string;
  variant?: "hero" | "section" | "cta";
  intensity?: number;
  mousePunch?: boolean;
};

// Per-variant settings for the dithered branch field. The hero spans the whole
// page (looser dots); sections/CTA are tighter and dimmer so the growth stays a
// subtle dark texture behind the content.
const FIELD_PARAMS = {
  hero: { cell: 7, dotR: 1.4, intensity: 1.0 },
  section: { cell: 7, dotR: 1.3, intensity: 0.85 },
  cta: { cell: 7, dotR: 1.3, intensity: 0.9 },
} as const;

export function DitheredBackdrop({
  className,
  variant = "section",
  intensity,
  mousePunch = false,
}: DitheredBackdropProps) {
  const params = FIELD_PARAMS[variant];
  return (
    <div className={clsx(styles.ditherCanvas, className)} aria-hidden="true">
      <BrowserOnly>
        {() => (
          <BranchDither
            cell={params.cell}
            dotR={params.dotR}
            intensity={intensity ?? params.intensity}
            mousePunch={mousePunch}
            speed={1}
            style={{ position: "absolute", top: 0, left: 0, width: "100%" }}
          />
        )}
      </BrowserOnly>
    </div>
  );
}
