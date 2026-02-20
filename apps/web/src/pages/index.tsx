import Link from "@docusaurus/Link";
import Heading from "@theme/Heading";
import Layout from "@theme/Layout";
import clsx from "clsx";
import {
  ArrowRight,
  Bot,
  Check,
  Command,
  Copy,
  GitBranch,
  Sprout,
  Terminal,
  User,
  Zap,
} from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import React, { useEffect, useMemo, useRef, useState } from "react";

import styles from "./index.module.css";

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

function InstallCommand() {
  return (
    <div className={styles.installBox}>
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
  );
}

const MAIN_NODE = { x: 490, y: 280 };

const WORKTREE_DEFS = [
  { id: "feat-auth", x: 180, y: 420, label: "feat/auth", color: "#0daf62" },
  { id: "fix-bug", x: 200, y: 280, label: "fix/bug", color: "#06b6d4" },
  { id: "feat-ui", x: 780, y: 280, label: "feat/ui", color: "#8b5cf6" },
  { id: "review-pr", x: 800, y: 420, label: "review/pr", color: "#f59e0b" },
  { id: "chore-deps", x: 180, y: 150, label: "chore/deps", color: "#00d4aa" },
  { id: "docs-api", x: 800, y: 150, label: "docs/api", color: "#38bdf8" },
] as const;

const MAX_COMMIT_DOTS_PER_WORKTREE = 20;
const COMMIT_DOT_LIFETIME_MS = 4200;
const PARALLEL_COMMIT_CADENCE_MS = 720;
const COMMIT_OFFSET_STEP_MS = 140;
const AGENT_BASE_SPEED = 0.00074;
const USER_HOP_INTERVAL_MS = 3200;
const USER_ORBIT_SPEED = 0.00105;
const GRAPH_WIDTH = 980;
const GRAPH_HEIGHT = 560;

type WorktreeDef = (typeof WORKTREE_DEFS)[number];
type ReactKonvaModule = typeof import("react-konva");
type CommitParticle = {
  id: number;
  worktreeId: WorktreeDef["id"];
  createdAt: number;
  angle: number;
  startRadius: number;
  endRadius: number;
  wobble: number;
  originDx: number;
  originDy: number;
};
type RenderParticle = {
  id: string;
  x: number;
  y: number;
  opacity: number;
  radius: number;
  color: string;
};
type RenderAgent = {
  id: string;
  x: number;
  y: number;
  color: string;
  anchorX: number;
  anchorY: number;
};
type GraphTransform = {
  scale: number;
  offsetX: number;
  offsetY: number;
};

function GitWorktreeBackground() {
  const commitIdRef = useRef(0);
  const graphRef = useRef<HTMLDivElement | null>(null);
  const [isClient, setIsClient] = useState(false);
  const [konva, setKonva] = useState<ReactKonvaModule | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [graphSize, setGraphSize] = useState({
    width: GRAPH_WIDTH,
    height: GRAPH_HEIGHT,
  });
  const [commitParticles, setCommitParticles] = useState<CommitParticle[]>([]);
  const worktreeById = useMemo<Record<WorktreeDef["id"], WorktreeDef>>(
    () =>
      WORKTREE_DEFS.reduce(
        (acc, wt) => {
          acc[wt.id] = wt;
          return acc;
        },
        {} as Record<WorktreeDef["id"], WorktreeDef>,
      ),
    [],
  );

  useEffect(() => {
    setIsClient(true);
  }, []);

  useEffect(() => {
    let active = true;
    import("react-konva").then((mod) => {
      if (active) {
        setKonva(mod);
      }
    });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!isClient || !graphRef.current) {
      return;
    }

    const element = graphRef.current;
    let rafId = 0;

    const updateSize = () => {
      cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        const rect = element.getBoundingClientRect();
        const width = Math.max(1, Math.round(rect.width));
        const height = Math.max(1, Math.round(rect.height));
        setGraphSize((prev) =>
          prev.width === width && prev.height === height
            ? prev
            : { width, height },
        );
      });
    };

    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);

    return () => {
      cancelAnimationFrame(rafId);
      observer.disconnect();
    };
  }, [isClient]);

  useEffect(() => {
    let rafId: number;
    const tick = () => {
      setNowMs(Date.now());
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, []);

  useEffect(() => {
    const pendingTimeouts = new Set<number>();

    const appendCommitDot = (worktreeId: WorktreeDef["id"]) => {
      const createdAt = Date.now();
      setCommitParticles((prev) => {
        const alive = prev.filter(
          (p) => createdAt - p.createdAt < COMMIT_DOT_LIFETIME_MS,
        );
        const withNew = [
          ...alive,
          {
            id: commitIdRef.current++,
            worktreeId,
            createdAt,
            angle: Math.random() * Math.PI * 2,
            startRadius: 6 + Math.random() * 10,
            endRadius: 18 + Math.random() * 18,
            wobble: Math.random() * Math.PI * 2,
            originDx:
              Math.cos(Math.random() * Math.PI * 2) * (3 + Math.random() * 10),
            originDy:
              Math.sin(Math.random() * Math.PI * 2) * (2 + Math.random() * 7),
          },
        ];

        const cap = MAX_COMMIT_DOTS_PER_WORKTREE * WORKTREE_DEFS.length;
        return withNew.slice(-cap);
      });
    };

    const spawnParallelCommits = () => {
      WORKTREE_DEFS.forEach((wt, index) => {
        const timeoutId = window.setTimeout(() => {
          pendingTimeouts.delete(timeoutId);
          appendCommitDot(wt.id);
        }, index * COMMIT_OFFSET_STEP_MS);
        pendingTimeouts.add(timeoutId);
      });
    };

    spawnParallelCommits();
    const intervalId = window.setInterval(
      spawnParallelCommits,
      PARALLEL_COMMIT_CADENCE_MS,
    );

    return () => {
      window.clearInterval(intervalId);
      pendingTimeouts.forEach((timeoutId) => window.clearTimeout(timeoutId));
      pendingTimeouts.clear();
    };
  }, []);

  const renderParticles = useMemo<RenderParticle[]>(() => {
    const particles: RenderParticle[] = [];

    commitParticles.forEach((particle) => {
      const wt = worktreeById[particle.worktreeId];
      if (!wt) {
        return;
      }

      const age = nowMs - particle.createdAt;
      if (age < 0 || age > COMMIT_DOT_LIFETIME_MS) {
        return;
      }

      const progress = age / COMMIT_DOT_LIFETIME_MS;
      const radius =
        particle.startRadius +
        (particle.endRadius - particle.startRadius) * progress;
      const swirl =
        Math.sin(progress * Math.PI * 2 + particle.wobble) * 2.2 * progress;
      const angle = particle.angle + progress * 1.5;
      const opacity = (1 - progress) ** 1.35 * 0.92;

      particles.push({
        id: `${particle.worktreeId}-${particle.id}`,
        x: wt.x + particle.originDx + Math.cos(angle) * (radius + swirl),
        y: wt.y + particle.originDy + Math.sin(angle) * (radius * 0.82 + swirl),
        opacity,
        radius: 1.25 + (1 - progress) * 1.2,
        color: wt.color,
      });
    });

    return particles;
  }, [commitParticles, nowMs, worktreeById]);

  const renderAgents = useMemo<RenderAgent[]>(
    () =>
      WORKTREE_DEFS.map((wt, wtIndex) => {
        const phase =
          nowMs * (AGENT_BASE_SPEED + wtIndex * 0.00003) + wtIndex * 1.35;
        const radiusX =
          38 + (wtIndex % 3) * 10 + Math.sin(nowMs * 0.0009 + wtIndex) * 6;
        const radiusY =
          20 + (wtIndex % 2) * 10 + Math.cos(nowMs * 0.00082 + wtIndex) * 6;

        return {
          id: `agent-${wt.id}`,
          x: wt.x + Math.cos(phase) * radiusX,
          y: wt.y + Math.sin(phase * 1.28) * radiusY,
          color: wt.color,
          anchorX: wt.x,
          anchorY: wt.y,
        };
      }),
    [nowMs],
  );

  const renderUser = useMemo(() => {
    const hop = nowMs / USER_HOP_INTERVAL_MS;
    const currentIndex = Math.floor(hop) % WORKTREE_DEFS.length;
    const previousIndex =
      (currentIndex - 1 + WORKTREE_DEFS.length) % WORKTREE_DEFS.length;
    const transition = hop - Math.floor(hop);
    const ease = transition * transition * (3 - 2 * transition);
    const from = WORKTREE_DEFS[previousIndex];
    const to = WORKTREE_DEFS[currentIndex];
    const centerX = from.x + (to.x - from.x) * ease;
    const centerY = from.y + (to.y - from.y) * ease;
    const orbitPhase = nowMs * USER_ORBIT_SPEED;

    return {
      x: centerX + Math.cos(orbitPhase) * 28,
      y: centerY + Math.sin(orbitPhase * 1.22) * 18,
      color: to.color,
      targetX: to.x,
      targetY: to.y,
    };
  }, [nowMs]);

  const graphTransform = useMemo<GraphTransform>(() => {
    const scale = Math.min(
      graphSize.width / GRAPH_WIDTH,
      graphSize.height / GRAPH_HEIGHT,
    );
    const safeScale = Number.isFinite(scale) && scale > 0 ? scale : 1;
    return {
      scale: safeScale,
      offsetX: (graphSize.width - GRAPH_WIDTH * safeScale) / 2,
      offsetY: (graphSize.height - GRAPH_HEIGHT * safeScale) / 2,
    };
  }, [graphSize]);

  const projectPoint = (x: number, y: number) => ({
    left: `${graphTransform.offsetX + x * graphTransform.scale}px`,
    top: `${graphTransform.offsetY + y * graphTransform.scale}px`,
  });

  return (
    <div className={styles.backgroundGraphic}>
      <div ref={graphRef} className={styles.worktreeVisualization}>
        {isClient &&
          konva &&
          (() => {
            const { Stage, Layer, Line, Circle, Rect, Text, Group } = konva;
            return (
              <Stage
                width={graphSize.width}
                height={graphSize.height}
                className={styles.worktreeCanvas}
              >
                <Layer listening={false}>
                  <Group
                    x={graphTransform.offsetX}
                    y={graphTransform.offsetY}
                    scaleX={graphTransform.scale}
                    scaleY={graphTransform.scale}
                  >
                    {WORKTREE_DEFS.map((wt) => (
                      <Line
                        key={`branch-${wt.id}`}
                        points={[MAIN_NODE.x, MAIN_NODE.y, wt.x, wt.y]}
                        stroke={wt.color}
                        strokeWidth={1.6}
                        dash={[7, 6]}
                        opacity={0.55}
                      />
                    ))}

                    <Circle
                      x={MAIN_NODE.x}
                      y={MAIN_NODE.y}
                      radius={16}
                      fill="#10b981"
                      opacity={0.12}
                    />
                    <Circle
                      x={MAIN_NODE.x}
                      y={MAIN_NODE.y}
                      radius={6}
                      fill="#10b981"
                      opacity={0.9}
                    />

                    {WORKTREE_DEFS.map((wt) => {
                      const labelWidth = 24 + wt.label.length * 6.6;
                      return (
                        <Group key={`wt-${wt.id}`} x={wt.x} y={wt.y}>
                          <Circle radius={16} fill={wt.color} opacity={0.12} />
                          <Circle radius={5} fill={wt.color} opacity={0.9} />
                          <Rect
                            x={-labelWidth / 2}
                            y={-10}
                            width={labelWidth}
                            height={20}
                            cornerRadius={10}
                            fill="rgba(8, 14, 11, 0.38)"
                            stroke={`${wt.color}55`}
                            strokeWidth={1}
                          />
                          <Text
                            x={-labelWidth / 2}
                            y={-6}
                            width={labelWidth}
                            height={12}
                            align="center"
                            verticalAlign="middle"
                            text={wt.label}
                            fill={wt.color}
                            fontSize={10.5}
                            fontStyle="bold"
                          />
                        </Group>
                      );
                    })}

                    {renderParticles.map((particle) => (
                      <Circle
                        key={`particle-${particle.id}`}
                        x={particle.x}
                        y={particle.y}
                        radius={particle.radius}
                        fill={particle.color}
                        opacity={particle.opacity}
                      />
                    ))}

                    {renderAgents.map((agent) => (
                      <Line
                        key={`agent-link-${agent.id}`}
                        points={[
                          agent.anchorX,
                          agent.anchorY,
                          agent.x,
                          agent.y,
                        ]}
                        stroke={agent.color}
                        strokeWidth={1.1}
                        dash={[3, 7]}
                        opacity={0.34}
                      />
                    ))}
                    {renderAgents.map((agent) => (
                      <Group key={agent.id} x={agent.x} y={agent.y}>
                        <Circle
                          radius={12.5}
                          fill={agent.color}
                          opacity={0.18}
                        />
                        <Circle radius={8.2} fill={agent.color} opacity={0.9} />
                      </Group>
                    ))}

                    <Line
                      points={[
                        renderUser.targetX,
                        renderUser.targetY,
                        renderUser.x,
                        renderUser.y,
                      ]}
                      stroke={renderUser.color}
                      strokeWidth={1.05}
                      dash={[2, 8]}
                      opacity={0.32}
                    />
                    <Group x={renderUser.x} y={renderUser.y}>
                      <Circle radius={11.2} fill="rgba(229, 231, 235, 0.18)" />
                      <Circle radius={7.4} fill="#e5e7eb" opacity={0.95} />
                    </Group>
                  </Group>
                </Layer>
              </Stage>
            );
          })()}
        <div className={styles.graphIconOverlay}>
          {renderAgents.map((agent) => (
            <div
              key={`agent-icon-${agent.id}`}
              className={styles.agentIcon}
              style={projectPoint(agent.x, agent.y) as CSSProperties}
            >
              <Bot size={12} strokeWidth={2.2} />
            </div>
          ))}
          <div
            className={styles.userIcon}
            style={projectPoint(renderUser.x, renderUser.y) as CSSProperties}
          >
            <User size={11} strokeWidth={2.2} />
          </div>
        </div>
      </div>

      <div className={styles.heroGlow} />
    </div>
  );
}

function HomepageHeader() {
  return (
    <header className={styles.heroBanner}>
      <GitWorktreeBackground />

      <div className="container">
        <div className={styles.heroContent}>
          <div className={styles.badge}>
            <Sprout size={13} />
            <span>Open Source • Built for the Terminal</span>
          </div>

          <Heading as="h1" className={styles.heroTitle}>
            <span className={styles.titleLine}>Split your work.</span>
            <span className={styles.titleLine}>
              <span className={styles.titleGradient}>Not your focus.</span>
            </span>
          </Heading>

          <p className={styles.heroSubtitle}>
            Sprout manages your git worktrees with isolated tmux sessions and
            dedicated AI agents — so you can work across multiple branches
            without ever losing your place.
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

const FEATURES = [
  {
    n: "01",
    title: "See everything at once",
    description:
      "A TUI showing all your worktrees with live status, active sessions, git state, and running agents — without leaving the terminal.",
  },
  {
    n: "02",
    title: "Isolated environments, instantly",
    description:
      "Every worktree gets its own tmux session with Neovim, Lazygit, and your tools pre-configured. Switch contexts in one command.",
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

function FeaturesSection() {
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
          <div className={styles.featuresGrid}>
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
    </section>
  );
}

type DemoCardProps = {
  icon: any;
  title: string;
  children: ReactNode;
};

function DemoCard({ icon: Icon, title, children }: DemoCardProps) {
  return (
    <div className={styles.demoCard}>
      <div className={styles.terminalChrome}>
        <div className={styles.terminalDots}>
          <span
            className={styles.terminalDot}
            style={{ background: "#ff5f57" }}
          />
          <span
            className={styles.terminalDot}
            style={{ background: "#febc2e" }}
          />
          <span
            className={styles.terminalDot}
            style={{ background: "#28c840" }}
          />
        </div>
        <div className={styles.terminalWindowTitle}>
          <Icon size={12} />
          <span>{title}</span>
        </div>
      </div>
      <div className={styles.terminalBody}>{children}</div>
    </div>
  );
}

function DemoSection() {
  return (
    <section className={styles.demoSection}>
      <div className="container">
        <div className={styles.demoHeader}>
          <div className={styles.demoHeaderLeft}>
            <span className={styles.sectionLabel}>In practice</span>
            <h2 className={styles.sectionTitle}>A few commands.</h2>
          </div>
          <p className={styles.demoHeaderRight}>
            Simple, memorable commands that stay out of your way.
          </p>
        </div>

        <div className={styles.demoGrid}>
          <DemoCard icon={GitBranch} title="create a worktree">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>
                sprout new feat checkout-redesign
              </span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalSuccess}>✓ branch created</div>
              <div className={styles.terminalSuccess}>
                ✓ tmux session launched
              </div>
              <div className={styles.terminalSuccess}>✓ agent ready</div>
            </div>
          </DemoCard>

          <DemoCard icon={Terminal} title="list all worktrees">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>sprout list</span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalTable}>
                <div>
                  <span className={styles.terminalCurrent}>*</span>{" "}
                  feat/checkout-redesign{" "}
                  <span className={styles.terminalActive}>active</span>
                </div>
                <div>
                  &nbsp; main{" "}
                  <span className={styles.terminalInactive}>clean</span>
                </div>
                <div>
                  &nbsp; fix/login-bug{" "}
                  <span className={styles.terminalInactive}>clean</span>
                </div>
              </div>
            </div>
          </DemoCard>

          <DemoCard icon={Zap} title="switch instantly">
            <div className={styles.terminalLine}>
              <span className={styles.terminalPrompt}>$</span>
              <span className={styles.terminalCommand}>
                sprout go fix/login-bug
              </span>
            </div>
            <div className={styles.terminalOutput}>
              <div className={styles.terminalPath}>
                ~/projects/myapp.worktrees/fix-login-bug
              </div>
            </div>
          </DemoCard>
        </div>
      </div>
    </section>
  );
}

type AgentLine =
  | { t: "cmd"; text: string }
  | { t: "thought"; text: string }
  | { t: "success"; text: string }
  | { t: "blank" };

const TUI_AGENT_LINES: AgentLine[][] = [
  // feat/checkout
  [
    { t: "cmd",     text: "Reading src/hooks/useCart.ts..." },
    { t: "blank" },
    { t: "thought", text: "The cart total isn't guarded — null on" },
    { t: "thought", text: "first render will throw. Adding optional" },
    { t: "thought", text: "chaining and a zero fallback." },
    { t: "blank" },
    { t: "cmd",     text: "Editing src/hooks/useCart.ts..." },
    { t: "success", text: "Patched — cart?.total ?? 0" },
    { t: "cmd",     text: "Running tests..." },
    { t: "success", text: "12 passed, 0 failed" },
  ],
  // fix/auth-bug
  [
    { t: "cmd",     text: "Reading src/auth/token.ts..." },
    { t: "blank" },
    { t: "thought", text: "refreshToken fires without checking if" },
    { t: "thought", text: "a refresh is already in progress —" },
    { t: "thought", text: "duplicate requests under load. Adding" },
    { t: "thought", text: "an isRefreshing guard." },
    { t: "blank" },
    { t: "cmd",     text: "Editing src/auth/token.ts..." },
    { t: "success", text: "Added guard on line 47" },
    { t: "success", text: "8 passed, 0 failed" },
  ],
  // chore/cleanup
  [
    { t: "cmd",     text: "Scanning for unused imports..." },
    { t: "blank" },
    { t: "thought", text: "Found 3 files with dead imports." },
    { t: "thought", text: "Removing them to keep the bundle lean." },
    { t: "blank" },
    { t: "cmd",     text: "Editing 3 files..." },
    { t: "success", text: "Removed 12 unused imports" },
    { t: "success", text: "0 warnings, 0 errors" },
  ],
];

const TUI_SCENES = [
  {
    worktree: "feat/checkout",
    agentStatus: "running" as const,
    rowIndex: 1,
  },
  {
    worktree: "fix/auth-bug",
    agentStatus: "running" as const,
    rowIndex: 2,
  },
  {
    worktree: "chore/cleanup",
    agentStatus: "running" as const,
    rowIndex: 3,
  },
];

const TUI_ROWS = [
  { label: "main",           status: "dirty", tmux: false, agent: false },
  { label: "feat/checkout",  status: "dirty", tmux: true,  agent: true  },
  { label: "fix/auth-bug",   status: "dirty", tmux: true,  agent: true  },
  { label: "chore/cleanup",  status: "clean", tmux: false, agent: false },
];

// 2 tabs (agent, diff) per scene × 3 scenes = 6 steps
const TUI_TOTAL_STEPS = TUI_SCENES.length * 2;

function TUISection() {
  const [step, setStep] = useState(0);
  const [visible, setVisible] = useState(true);

  const sceneIdx = Math.floor(step / 2);
  const tab = step % 2 === 0 ? "agent" : "diff";
  const scene = TUI_SCENES[sceneIdx];

  const [visibleLines, setVisibleLines] = useState(0);

  // Advance the step every 5.5s
  useEffect(() => {
    const id = setInterval(() => {
      setVisible(false);
      setTimeout(() => {
        setStep((s) => (s + 1) % TUI_TOTAL_STEPS);
        setVisible(true);
      }, 250);
    }, 5500);
    return () => clearInterval(id);
  }, []);

  // Reveal agent lines one by one on each step
  useEffect(() => {
    setVisibleLines(0);
    if (tab !== "agent") return;
    const total = TUI_AGENT_LINES[sceneIdx].length;
    let n = 0;
    const id = setInterval(() => {
      n++;
      setVisibleLines(n);
      if (n >= total) clearInterval(id);
    }, 160);
    return () => clearInterval(id);
  }, [step]);

  return (
    <section className={styles.tuiSection}>
      <div className="container">
        <div className={styles.tuiLayout}>
          <div className={styles.tuiCopy}>
            <span className={styles.sectionLabel}>Interface</span>
            <h2 className={styles.sectionTitle}>
              A TUI built for{" "}
              <span className={styles.titleGradient}>parallel work.</span>
            </h2>
            <p className={styles.tuiDescription}>
              Run <code>sprout ui</code> for a three-pane interface — every
              branch, its tmux session, git state, and running agent, all
              visible at once.
            </p>
            <ul className={styles.tuiPoints}>
              <li>Jump between worktrees with a single keypress</li>
              <li>Watch live agent output and git diffs side by side</li>
              <li>Start, stop, and attach to agents without switching tabs</li>
            </ul>
          </div>

          <div className={styles.tuiFrame}>
            <div className={styles.terminalChrome}>
              <div className={styles.terminalDots}>
                <span
                  className={styles.terminalDot}
                  style={{ background: "#ff5f57" }}
                />
                <span
                  className={styles.terminalDot}
                  style={{ background: "#febc2e" }}
                />
                <span
                  className={styles.terminalDot}
                  style={{ background: "#28c840" }}
                />
              </div>
              <div className={styles.terminalWindowTitle}>
                <Terminal size={12} />
                <span>sprout ui</span>
              </div>
            </div>
            <div className={styles.tuiBody}>
              {/* Pane 1 — Status */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[1]-Status</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <span className={styles.tuiGreen}>✓</span>{" "}
                <span className={styles.tuiMuted}>myapp</span>
                <span className={styles.tuiDim}> → </span>
                <span className={styles.tuiGreen}>main</span>
                <span className={styles.tuiDim}>{"  "}selected: </span>
                <span
                  className={styles.tuiGreen}
                  style={{ transition: "opacity 0.25s", opacity: visible ? 1 : 0 }}
                >
                  {scene.worktree}
                </span>
                <span className={styles.tuiDim}>{"  "}agent: </span>
                <span className={styles.tuiGreen}>running</span>
              </div>

              {/* Pane 2 — Details */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[2]-Details</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <div className={styles.tuiTabs}>
                  <span className={tab === "agent" ? styles.tuiTabActive : styles.tuiTabInactive}>
                    AGENT OUTPUT
                  </span>
                  <span className={styles.tuiDim}> │ </span>
                  <span className={tab === "diff" ? styles.tuiTabActive : styles.tuiTabInactive}>
                    GIT DIFF
                  </span>
                </div>
                <div className={styles.tuiDetailPane}>
                  <div
                    className={clsx(styles.tuiDetailBody, tab === "diff" && styles.tuiDetailBodyTop)}
                    style={{ opacity: visible ? 1 : 0, transition: "opacity 0.25s" }}
                  >
                    {tab === "agent" ? (
                      TUI_AGENT_LINES[sceneIdx].slice(0, visibleLines).map((line, i) =>
                        line.t === "blank" ? (
                          <div key={i}>&nbsp;</div>
                        ) : line.t === "cmd" ? (
                          <div key={i}>
                            <span className={styles.tuiDim}>&gt; </span>
                            <span className={styles.tuiMuted}>{line.text}</span>
                          </div>
                        ) : line.t === "success" ? (
                          <div key={i}>
                            <span className={styles.tuiGreen}>✓ </span>
                            <span className={styles.tuiDim}>{line.text}</span>
                          </div>
                        ) : (
                          <div key={i} className={styles.tuiDim}>{line.text}</div>
                        )
                      )
                    ) : (
                      sceneIdx === 0 ? <>
                        <div className={styles.tuiDim}>src/hooks/useCart.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}const total = cart.total</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}const total = cart?.total ?? 0</span></div>
                        <div className={styles.tuiDim}>&nbsp;</div>
                        <div className={styles.tuiDim}>src/components/Cart.tsx</div>
                        <div><span className={styles.tuiGreen}>+{"  "}if (!items) return null</span></div>
                      </> : sceneIdx === 1 ? <>
                        <div className={styles.tuiDim}>src/auth/token.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}refreshToken()</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}if (!isRefreshing) {"{"}</span></div>
                        <div><span className={styles.tuiGreen}>+{"    "}refreshToken()</span></div>
                        <div><span className={styles.tuiGreen}>+{"  "}{"}"}</span></div>
                      </> : <>
                        <div className={styles.tuiDim}>src/utils/format.ts</div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ debounce }"} from 'lodash'</span></div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ merge }"} from 'lodash'</span></div>
                        <div className={styles.tuiDim}>&nbsp;</div>
                        <div className={styles.tuiDim}>src/pages/Dashboard.tsx</div>
                        <div><span className={styles.tuiRed}>-{"  "}import {"{ memo }"} from 'react'</span></div>
                      </>
                    )}
                  </div>
                </div>
              </div>

              {/* Pane 3 — Worktrees */}
              <div className={styles.tuiPaneBar}>
                <div className={styles.tuiPaneBarLine} />
                <span>[3]-Worktrees</span>
                <div className={styles.tuiPaneBarLine} />
              </div>
              <div className={styles.tuiPaneContent}>
                <div className={styles.tuiTableHeader}>
                  <span>CUR</span>
                  <span>BRANCH</span>
                  <span>STATUS</span>
                  <span>TMUX</span>
                  <span>AGENT</span>
                </div>
                {TUI_ROWS.map((row, i) => (
                  <div
                    key={row.label}
                    className={clsx(
                      styles.tuiTableRow,
                      i === scene.rowIndex && styles.tuiTableRowSelected,
                    )}
                  >
                    <span className={i === scene.rowIndex ? styles.tuiGreen : undefined}>
                      {i === scene.rowIndex ? "*" : ""}
                    </span>
                    <span className={styles.tuiMuted}>{row.label}</span>
                    <span className={row.status === "dirty" ? styles.tuiRed : styles.tuiGreen}>
                      {row.status}
                    </span>
                    <span className={row.tmux ? styles.tuiGreen : styles.tuiDim}>
                      {row.tmux ? "yes" : "no"}
                    </span>
                    <span className={row.agent ? styles.tuiGreen : styles.tuiDim}>
                      {row.agent ? "yes" : "no"}
                    </span>
                  </div>
                ))}
              </div>

              <div className={styles.tuiBottomBar}>
                <span className={styles.tuiDim}>
                  └ tab cycle modal focus │ esc close modal{"  "}INFO: ready
                </span>
                <span className={styles.tuiDim}>
                  {scene.rowIndex + 1} of 4{"  "}─{" "}
                  <span style={{ opacity: visible ? 1 : 0, transition: "opacity 0.25s" }}>
                    {scene.worktree}
                  </span>
                  {" "}↗
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function CTASection() {
  return (
    <section className={styles.ctaSection}>
      <div className="container">
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
    </section>
  );
}

export default function Home(): ReactNode {
  return (
    <Layout
      title="Branch fearlessly. Stay in flow."
      description="Sprout manages your git worktrees with isolated tmux sessions and AI agents — so you can work across multiple branches without losing your place."
    >
      <HomepageHeader />
      <main>
        <FeaturesSection />
        <DemoSection />
        <TUISection />
        <CTASection />
      </main>
    </Layout>
  );
}
