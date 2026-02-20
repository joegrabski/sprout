import React, { useState } from "react";
import Link from "@docusaurus/Link";
import { Check, Copy, Terminal } from "lucide-react";
import styles from "../../pages/index.module.css";

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
  return (
    <div className={styles.installBox}>
      <div className={styles.installLabel}>
        <Terminal size={13} />
        <span>Install via Homebrew</span>
      </div>
      <InstallLine command="brew tap joegrabski/sprout https://github.com/joegrabski/sprout" />
      <InstallLine command="brew install sprout" />
      <div className={styles.installLinks}>
        <Link to="/docs/installation">Other installation methods →</Link>
      </div>
    </div>
  );
}
