import { useEffect, useRef } from "react";
import styles from "./AsciiFlowField.module.css";
import { BAYER, buildPalette, N, paintDots, TINTS } from "./dither";

// A dark, dithered "growing branches" backdrop sized to the FULL page (absolute,
// not fixed — it scrolls with the content; only the visible band is redrawn each
// frame). Trees are rooted just outside the page edges and grow inward, so
// branches enter from the borders rather than appearing mid-screen. Each tree
// grows on its own cycle (curved branches rasterized into a density grid),
// quantized through a 4x4 Bayer matrix into small dark-green dither dots. The
// cursor disturbs nearby dots — shoving them, dusting in fresh ones, shedding a
// few that tumble away.
//
// Decorative: aria-hidden, pointer-inert, paused on hidden tabs, single static
// frame under reduced motion.

const BRICK = 0.34;
const SPAWN_DECAY = 0.76;
const SPAWN_GAIN = 0.5;
const SPAWN_MAX = 0.7;
const SPAWN_EPS = 0.02;
const PUSH_GAIN = 1.7;
const PUSH_MAX = 1.6;
const PUSH_DECAY = 0.8;
const PUSH_EPS = 0.05;
const FRAG_SPREAD = 0.9;
const FRAG_SPEED = 0.9;
const FRAG_DRAG = 0.9;
const FRAG_GRAV = 0.02;
const FRAG_FADE = 0.87;
const FRAG_CHANCE = 0.5;
const FRAG_CAP = 260;

export default function BranchDither({
  className = "",
  cell = 7,
  dotR = 1.4,
  fps = 22,
  speed = 1,
  mousePunch = false,
  radius = 170,
  intensity = 1,
  style,
}) {
  const wrapRef = useRef(null);
  const canvasRef = useRef(null);
  const rafRef = useRef(0);

  useEffect(() => {
    const wrap = wrapRef.current;
    const canvas = canvasRef.current;
    if (!wrap || !canvas) return undefined;
    const ctx = canvas.getContext("2d");
    if (!ctx) return undefined;

    const PAL = buildPalette(intensity);

    const reduced =
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const useMouse = mousePunch && !reduced;

    let cols = 0;
    let rows = 0; // full-document rows
    let viewRows = 0; // viewport rows (tree scale)
    let wCss = 0;
    let hCss = 0;
    let v0 = 0;
    let v1 = 0;
    let dens = new Float32Array(0);
    let tintGrid = new Uint8Array(0);
    let isBrick = new Uint8Array(0);
    let spawn = new Float32Array(0);
    let pushX = new Float32Array(0);
    let pushY = new Float32Array(0);
    let segs = [];
    let treePeriod = [];
    let treeOffset = [];
    const spawnCells = new Set();
    const pushCells = new Set();
    let particles = [];
    let running = false;
    let lastTime = 0;
    let clock = 0;
    const idleInterval = 1000 / fps;
    const activeInterval = 1000 / 45;
    const brush = Math.max(1, Math.round(radius / 90));
    const m = {
      cx: 0,
      cy: 0,
      lastCx: 0,
      lastCy: 0,
      active: false,
      lastMove: -1e6,
    };

    const buildTree = (tree, tint, rx, ry, ang, len, depth, t0, rng) => {
      const dur = 0.05 + depth * 0.028;
      const x1 = rx + Math.cos(ang) * len;
      const y1 = ry + Math.sin(ang) * len;
      const nx = -(y1 - ry);
      const ny = x1 - rx;
      const nl = Math.hypot(nx, ny) || 1;
      const bow = (rng() - 0.5) * len * 0.6;
      segs.push({
        tree,
        tint,
        x0: rx,
        y0: ry,
        cx: (rx + x1) / 2 + (nx / nl) * bow,
        cy: (ry + y1) / 2 + (ny / nl) * bow,
        x1,
        y1,
        minY: Math.min(ry, y1) - 3,
        maxY: Math.max(ry, y1) + 3,
        brush: depth >= 3 ? 1.6 : depth >= 1 ? 1.1 : 0.8,
        amp: 0.72 + depth * 0.12,
        t0,
        t1: Math.min(t0 + dur, 1),
      });
      if (depth <= 0) return;
      const kids = rng() < 0.22 ? 3 : 2;
      for (let i = 0; i < kids; i++) {
        const spread = 0.55 + rng() * 0.55;
        const side = kids === 3 && i === 1 ? 0 : i === 0 ? -1 : 1;
        const a = ang + side * spread + (rng() - 0.5) * 0.3;
        const l = len * (0.58 + rng() * 0.16);
        buildTree(tree, tint, x1, y1, a, l, depth - 1, t0 + dur * 0.72, rng);
      }
    };

    const buildForest = () => {
      segs = [];
      treePeriod = [];
      treeOffset = [];
      let seed = (cols * 2654435761) ^ (rows * 40503);
      const rng = () => {
        seed = (seed * 1103515245 + 12345) & 0x7fffffff;
        return seed / 0x7fffffff;
      };
      const size = Math.max(10, viewRows / 5.0);
      const spacing = size * 1.7;
      const off = size * 0.15;
      let t = 0;
      const addRoot = (rx, ry, ang, chance) => {
        if (rng() > chance) return;
        treePeriod[t] = 16 + rng() * 22;
        treeOffset[t] = rng() * treePeriod[t];
        const tint = (rng() * TINTS.length) | 0;
        const a = ang + (rng() - 0.5) * 0.5;
        buildTree(
          t,
          tint,
          rx,
          ry,
          a,
          size * (0.9 + rng() * 0.5),
          3 + (rng() < 0.4 ? 1 : 0),
          rng() * 0.4,
          rng,
        );
        t++;
      };
      // Top & bottom edges of the page.
      const nx = Math.max(1, Math.round(cols / spacing));
      for (let i = 0; i < nx; i++) {
        addRoot(
          (i + 0.2 + rng() * 0.6) * (cols / nx),
          rows + off,
          -Math.PI / 2,
          0.9,
        );
        addRoot((i + 0.2 + rng() * 0.6) * (cols / nx), -off, Math.PI / 2, 0.7);
      }
      // Left & right edges, all the way down the page.
      const ny = Math.max(1, Math.round(rows / spacing));
      for (let i = 0; i < ny; i++) {
        addRoot(-off, (i + 0.2 + rng() * 0.6) * (rows / ny), 0, 0.8);
        addRoot(
          cols + off,
          (i + 0.2 + rng() * 0.6) * (rows / ny),
          Math.PI,
          0.8,
        );
      }
    };

    // Rasterize a curved branch up to growth fraction pEnd, culled to the band.
    const rasterSeg = (s, pEnd, amp, tint) => {
      const chord = Math.hypot(s.x1 - s.x0, s.y1 - s.y0);
      const n = Math.max(2, Math.ceil(chord * pEnd) + 1);
      const br = s.brush;
      const R = Math.ceil(br);
      for (let k = 0; k <= n; k++) {
        const t = (pEnd * k) / n;
        const it = 1 - t;
        const px = it * it * s.x0 + 2 * it * t * s.cx + t * t * s.x1;
        const py = it * it * s.y0 + 2 * it * t * s.cy + t * t * s.y1;
        const cyr = Math.round(py);
        if (cyr < v0 - R || cyr > v1 + R) continue;
        const cxr = Math.round(px);
        for (let by = -R; by <= R; by++) {
          for (let bx = -R; bx <= R; bx++) {
            const gx = cxr + bx;
            const gy = cyr + by;
            if (gx < 0 || gy < v0 || gx >= cols || gy >= v1) continue;
            const dd = Math.hypot(bx, by) / (br + 0.5);
            if (dd >= 1) continue;
            const i = gy * cols + gx;
            let v = dens[i] + amp * (1 - dd);
            if (v > 1.5) v = 1.5;
            dens[i] = v;
            tintGrid[i] = tint;
          }
        }
      }
    };

    const R = brush + 1;
    const strike = (px, py, amt, dirx, diry) => {
      const cgx = Math.floor(px / cell);
      const cgy = Math.floor(py / cell);
      for (let by = -R; by <= R; by += 1) {
        for (let bx = -R; bx <= R; bx += 1) {
          const gx = cgx + bx;
          const gy = cgy + by;
          if (gx < 0 || gy < 0 || gx >= cols || gy >= rows) continue;
          const i = gy * cols + gx;
          if (!isBrick[i]) continue;
          const along = bx * dirx + by * diry;
          const perp = -bx * diry + by * dirx;
          const dd = Math.sqrt(
            (along / (R + 1)) ** 2 * 0.45 + (perp / R) ** 2 * 1.7,
          );
          if (dd >= 1) continue;
          const fall = 1 - dd;
          const shove = amt * fall * PUSH_GAIN * cell;
          let nx = pushX[i] + dirx * shove;
          let ny = pushY[i] + diry * shove;
          const cap = PUSH_MAX * cell;
          nx = nx > cap ? cap : nx < -cap ? -cap : nx;
          ny = ny > cap ? cap : ny < -cap ? -cap : ny;
          pushX[i] = nx;
          pushY[i] = ny;
          pushCells.add(i);
          const sv = spawn[i] + SPAWN_GAIN * (0.6 + fall);
          spawn[i] = sv > SPAWN_MAX ? SPAWN_MAX : sv;
          spawnCells.add(i);
          if (
            particles.length < FRAG_CAP &&
            Math.random() < FRAG_CHANCE * (0.5 + fall)
          ) {
            const a =
              Math.atan2(diry, dirx) + (Math.random() - 0.5) * FRAG_SPREAD;
            const spd =
              FRAG_SPEED * (0.5 + Math.random() * 0.9) * (0.6 + amt * fall);
            let lvl = (dens[i] * N) | 0;
            lvl = lvl < 0 ? 0 : lvl > N - 1 ? N - 1 : lvl;
            particles.push({
              x: gx + 0.5,
              y: gy + 0.5,
              vx: Math.cos(a) * spd,
              vy: Math.sin(a) * spd,
              life: 1,
              lvl,
              tint: tintGrid[i],
            });
          }
        }
      }
    };

    const stepParticles = () => {
      if (!particles.length) return;
      const next = [];
      for (const p of particles) {
        p.vx *= FRAG_DRAG;
        p.vy = p.vy * FRAG_DRAG + FRAG_GRAV;
        p.x += p.vx;
        p.y += p.vy;
        p.life *= FRAG_FADE;
        if (
          p.life > 0.08 &&
          p.x > -3 &&
          p.y > -3 &&
          p.x < cols + 3 &&
          p.y < rows + 3
        )
          next.push(p);
      }
      particles = next;
    };

    const applyPunch = () => {
      const dmx = m.cx - m.lastCx;
      const dmy = m.cy - m.lastCy;
      m.lastCx = m.cx;
      m.lastCy = m.cy;
      const s = Math.sqrt(dmx * dmx + dmy * dmy);
      if (s < 0.5) return;
      const dirx = dmx / s;
      const diry = dmy / s;
      const amt = Math.min(0.6, 0.12 + s * 0.013);
      const steps = Math.min(48, Math.max(1, Math.floor(s / (cell * 0.7))));
      const ox = m.lastCx - dmx;
      const oy = m.lastCy - dmy;
      for (let k = 0; k <= steps; k += 1)
        strike(ox + (dmx * k) / steps, oy + (dmy * k) / steps, amt, dirx, diry);
    };

    const draw = () => {
      const sy = window.scrollY || window.pageYOffset || 0;
      const vh = window.innerHeight || hCss;
      v0 = Math.max(0, Math.floor(sy / cell) - 4);
      v1 = Math.min(rows, Math.ceil((sy + vh) / cell) + 4);

      for (let gy = v0; gy < v1; gy++) {
        const base = gy * cols;
        for (let gx = 0; gx < cols; gx++) dens[base + gx] = 0;
      }
      const ct = clock * speed;
      const bandTop = v0 - 4; // cell units, matching seg.minY/maxY
      const bandBot = v1 + 4;
      for (let s = 0; s < segs.length; s++) {
        const seg = segs[s];
        if (seg.maxY < bandTop || seg.minY > bandBot) continue;
        const per = treePeriod[seg.tree];
        const local = ((((ct + treeOffset[seg.tree]) % per) + per) % per) / per;
        const env = Math.min(1, local / 0.06) * Math.min(1, (1 - local) / 0.16);
        if (env <= 0) continue;
        let p = (local - seg.t0) / (seg.t1 - seg.t0);
        if (p <= 0) continue;
        p = p > 1 ? 1 : p * p * (3 - 2 * p);
        rasterSeg(seg, p, seg.amp * env, seg.tint);
      }
      for (let gy = v0; gy < v1; gy++) {
        const base = gy * cols;
        for (let gx = 0; gx < cols; gx++)
          isBrick[base + gx] = dens[base + gx] >= BRICK ? 1 : 0;
      }

      if (useMouse) {
        if (m.active) applyPunch();
        stepParticles();
        if (spawnCells.size) {
          for (const i of spawnCells) {
            const v = spawn[i] * SPAWN_DECAY;
            if (v < SPAWN_EPS) {
              spawn[i] = 0;
              spawnCells.delete(i);
            } else spawn[i] = v;
          }
        }
        if (pushCells.size) {
          for (const i of pushCells) {
            const nx = pushX[i] * PUSH_DECAY;
            const ny = pushY[i] * PUSH_DECAY;
            if (Math.abs(nx) < PUSH_EPS && Math.abs(ny) < PUSH_EPS) {
              pushX[i] = 0;
              pushY[i] = 0;
              pushCells.delete(i);
            } else {
              pushX[i] = nx;
              pushY[i] = ny;
            }
          }
        }
      }

      ctx.clearRect(0, v0 * cell, wCss, (v1 - v0) * cell);
      const d2 = dotR * 2;
      paintDots(ctx, {
        cols,
        v0,
        v1,
        cell,
        dotR,
        dens,
        tintGrid,
        PAL,
        spawn,
        pushX,
        pushY,
      });
      if (particles.length) {
        for (const p of particles) {
          const gx = p.x | 0;
          const gy = p.y | 0;
          if (gx < 0 || gy < v0 || gx >= cols || gy >= v1) continue;
          if (p.life < BAYER[(gy & 3) * 4 + (gx & 3)]) continue;
          ctx.fillStyle = PAL[p.tint][p.lvl];
          ctx.fillRect(p.x * cell - dotR, p.y * cell - dotR, d2, d2);
        }
      }
    };

    const layout = () => {
      const dpr = 1; // full-page canvas — keep memory in check; dots are chunky
      wCss = Math.max(
        1,
        Math.floor(window.innerWidth || wrap.getBoundingClientRect().width),
      );
      const docH = Math.max(
        window.innerHeight || 0,
        document.documentElement.scrollHeight,
        document.body ? document.body.scrollHeight : 0,
      );
      hCss = docH;
      wrap.style.height = `${docH}px`;
      cols = Math.max(1, Math.ceil(wCss / cell));
      rows = Math.max(1, Math.ceil(hCss / cell));
      viewRows = Math.max(1, Math.ceil((window.innerHeight || hCss) / cell));
      canvas.width = Math.floor(wCss * dpr);
      canvas.height = Math.floor(hCss * dpr);
      canvas.style.width = `${wCss}px`;
      canvas.style.height = `${hCss}px`;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const count = cols * rows;
      dens = new Float32Array(count);
      tintGrid = new Uint8Array(count);
      isBrick = new Uint8Array(count);
      spawn = new Float32Array(count);
      pushX = new Float32Array(count);
      pushY = new Float32Array(count);
      spawnCells.clear();
      pushCells.clear();
      particles = [];
      buildForest();
      draw();
    };

    const tick = (time) => {
      const dt = lastTime ? Math.min((time - lastTime) / 1000, 0.05) : 0.016;
      const movingRecently = useMouse && time - m.lastMove < 200;
      const active =
        useMouse &&
        (movingRecently ||
          spawnCells.size ||
          pushCells.size ||
          particles.length);
      const interval = active ? activeInterval : idleInterval;
      if (time - lastTime >= interval) {
        clock += dt * (0.6 + 1.0 * (0.5 + 0.5 * Math.sin(clock * 0.6)) ** 6.0);
        lastTime = time;
        draw();
      }
      if (running) rafRef.current = requestAnimationFrame(tick);
    };

    const start = () => {
      if (!running && !reduced && !document.hidden) {
        running = true;
        rafRef.current = requestAnimationFrame(tick);
      }
    };
    const stop = () => {
      running = false;
      cancelAnimationFrame(rafRef.current);
    };

    let lastKey = "";
    const maybeLayout = () => {
      const docH = Math.max(
        window.innerHeight || 0,
        document.documentElement.scrollHeight,
        document.body ? document.body.scrollHeight : 0,
      );
      const key = `${window.innerWidth}x${Math.round(docH / 60)}`;
      if (key === lastKey) return;
      lastKey = key;
      layout();
    };
    lastKey = `${window.innerWidth}x${Math.round(
      Math.max(window.innerHeight || 0, document.documentElement.scrollHeight) /
        60,
    )}`;
    layout();
    const settle = setTimeout(() => {
      lastKey = "";
      maybeLayout();
    }, 700);

    let rw = 0;
    const onResize = () => {
      window.clearTimeout(rw);
      rw = window.setTimeout(maybeLayout, 150);
    };
    window.addEventListener("resize", onResize);
    const ro = new ResizeObserver(() => onResize());
    if (document.body) ro.observe(document.body);

    const onMove = (e) => {
      const rect = canvas.getBoundingClientRect();
      m.cx = e.clientX - rect.left;
      m.cy = e.clientY - rect.top; // canvas spans the doc → client − rect.top = doc y
      m.active = true;
      m.lastMove = e.timeStamp || performance.now();
    };
    const onLeave = () => {
      m.active = false;
    };
    const onVis = () => {
      if (document.hidden) stop();
      else start();
    };
    if (useMouse) {
      window.addEventListener("pointermove", onMove, { passive: true });
      window.addEventListener("blur", onLeave);
      document.addEventListener("mouseleave", onLeave);
    }
    document.addEventListener("visibilitychange", onVis);
    start();

    return () => {
      stop();
      clearTimeout(settle);
      window.clearTimeout(rw);
      window.removeEventListener("resize", onResize);
      ro.disconnect();
      document.removeEventListener("visibilitychange", onVis);
      if (useMouse) {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("blur", onLeave);
        document.removeEventListener("mouseleave", onLeave);
      }
    };
  }, [cell, dotR, fps, speed, mousePunch, radius, intensity]);

  return (
    <div
      ref={wrapRef}
      aria-hidden="true"
      className={`${styles.field} ${className}`.trim()}
      style={style}
    >
      <canvas ref={canvasRef} className={styles.canvas} />
    </div>
  );
}
