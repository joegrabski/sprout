import BrowserOnly from "@docusaurus/BrowserOnly";
import Link from "@docusaurus/Link";
import { PulsingBorder } from "@paper-design/shaders-react";
import { Check, Copy, Terminal } from "lucide-react";
import React, { useEffect, useRef, useState } from "react";
import styles from "../../pages/index.module.css";
import { DitheredBackdrop } from "./DitheredBackdrop";

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      onClick={handleCopy}
      className={styles.copyButton}
      title="Copy to clipboard"
    >
      {copied ? <Check size={13} /> : <Copy size={13} />}
    </button>
  );
}

function InstallLine({ command }: { command: string }) {
  return (
    <div className={styles.installLine}>
      <span className={styles.commandPrompt}>$</span>
      <span className={styles.command}>{command}</span>
      <CopyButton text={command} />
    </div>
  );
}

export function InstallCommand() {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

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
      className={`${styles.installBox} ${styles.voxelSurface}`}
    >
      <BrowserOnly>
        {() =>
          size.width > 0 && size.height > 0 ? (
            <div className={styles.installShader} aria-hidden="true">
              <PulsingBorder
                width={size.width}
                height={size.height}
                colors={["#22d47a", "#0daf62", "#0a5a34cc"]}
                colorBack="#00000000"
                roundness={0.1}
                thickness={0.03}
                softness={1}
                aspectRatio="auto"
                intensity={0.2}
                bloom={0.2}
                spots={3}
                spotSize={0.5}
                pulse={0.1}
                smoke={0}
                smokeSize={0}
                speed={0.8}
                scale={0.95}
                marginLeft={0}
                marginRight={0}
                marginTop={0}
                marginBottom={0}
              />
            </div>
          ) : null
        }
      </BrowserOnly>
      <DitheredBackdrop className={styles.ditherEdge} variant="hero" />
      <div className={styles.voxelInner}>
        <div className={styles.installLabel}>
          <Terminal size={13} />
          <span>Install via Homebrew</span>
        </div>
        <InstallLine command="brew tap joegrabski/sprout" />
        <InstallLine command="brew install sprout" />
        <div className={styles.installLinks}>
          <Link to="/docs/installation">Other installation methods →</Link>
        </div>
      </div>
    </div>
  );
}
