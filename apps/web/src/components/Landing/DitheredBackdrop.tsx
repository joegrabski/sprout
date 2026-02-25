import BrowserOnly from "@docusaurus/BrowserOnly";
import { Dithering } from "@paper-design/shaders-react";
import clsx from "clsx";
import React, { useEffect, useMemo, useRef, useState } from "react";
import styles from "../../pages/index.module.css";

type DitheredBackdropProps = {
  className?: string;
  variant?: "hero" | "section" | "cta";
  intensity?: number;
};

const LIGHT_PALETTES = {
  hero: [
    [0.88, 0.96, 0.92],
    [0.78, 0.9, 0.86],
    [0.86, 0.98, 0.94],
  ],
  section: [
    [0.9, 0.97, 0.94],
    [0.8, 0.92, 0.88],
    [0.88, 0.99, 0.95],
  ],
  cta: [
    [0.86, 0.95, 0.92],
    [0.76, 0.88, 0.85],
    [0.86, 0.98, 0.94],
  ],
} as const;

const DARK_PALETTES = {
  hero: [
    [0.05, 0.11, 0.09],
    [0.07, 0.17, 0.13],
    [0.1, 0.24, 0.18],
  ],
  section: [
    [0.06, 0.13, 0.1],
    [0.08, 0.19, 0.14],
    [0.12, 0.26, 0.2],
  ],
  cta: [
    [0.07, 0.16, 0.12],
    [0.09, 0.22, 0.16],
    [0.13, 0.29, 0.21],
  ],
} as const;

export function DitheredBackdrop({
  className,
  variant = "section",
}: DitheredBackdropProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [theme, setTheme] = useState<"light" | "dark">("light");
  const [size, setSize] = useState({ width: 0, height: 0 });
  const palette = useMemo(() => {
    const source = theme === "dark" ? DARK_PALETTES : LIGHT_PALETTES;
    return source[variant];
  }, [theme, variant]);

  useEffect(() => {
    const root = document.documentElement;
    const update = () => {
      setTheme(root.getAttribute("data-theme") === "dark" ? "dark" : "light");
    };
    update();
    const observer = new MutationObserver(update);
    observer.observe(root, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const update = () => {
      const rect = el.getBoundingClientRect();
      setSize({
        width: Math.max(1, Math.round(rect.width)),
        height: Math.max(1, Math.round(rect.height)),
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={containerRef}
      className={clsx(styles.ditherCanvas, className)}
      aria-hidden="true"
    >
      <BrowserOnly>
        {() =>
          size.width > 0 && size.height > 0 ? (
            <Dithering
              width={size.width}
              height={size.height}
              colorBack={`#${palette[0]
                .map((c) =>
                  Math.round(c * 255)
                    .toString(16)
                    .padStart(2, "0"),
                )
                .join("")}`}
              colorFront={`#${palette[2]
                .map((c) =>
                  Math.round(c * 255)
                    .toString(16)
                    .padStart(2, "0"),
                )
                .join("")}`}
              shape="warp"
              type="4x4"
              size={4}
              speed={0.05}
              scale={3}
            />
          ) : null
        }
      </BrowserOnly>
    </div>
  );
}
