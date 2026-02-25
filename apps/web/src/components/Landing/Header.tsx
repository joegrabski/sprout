import React, { useEffect, useMemo, useRef, useState, CSSProperties } from "react";
import Link from "@docusaurus/Link";
import Heading from "@theme/Heading";
import clsx from "clsx";
import { ArrowRight, Bot, Command, Sprout, Terminal, User } from "lucide-react";
import { InstallCommand } from "./Install";
import styles from "../../pages/index.module.css";

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

export function HomepageHeader() {
  return (
    <header className={styles.heroBanner}>
      <GitWorktreeBackground />

      <div className="container">
        <div className={styles.heroContent} data-anim>
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
            dedicated AI agents, so you can work across multiple branches
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
