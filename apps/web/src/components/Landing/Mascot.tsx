import type React from "react";
import { useEffect, useRef } from "react";
import fieldStyles from "./AsciiFlowField.module.css";
import { buildPalette, paintDots } from "./dither";

// The mascot, painted by the exact same dot-dither engine as the page
// backdrop (BranchDither/dither.js) instead of a static SVG: every frame we
// rasterize the body/stem/leaves/eyes into a density grid and quantize it
// through the shared Bayer matrix into small squares — the same paintDots()
// call the background uses, and the cursor shoves/dusts its dots with the
// backdrop's exact punch mechanics.
//
// Personality runs on one repeating timeline (idle glance → the signature
// "split": two worktree nodes branch out on thin git-branch lines, hold,
// merge back — the product pitch as body language → curious tilt → sleepy
// droop → wake-up bounce), with two live behaviors on top: the eyes track
// the cursor when it's nearby, and a hard shove startles it (eyes pop wide,
// quick flinch squash).
//
// Always drawn at native (hero) resolution and only ever shrunk for display
// (see `displayWidth`) — the navbar/logo usage is a smaller *view* of the
// same drawing, not a re-tuned copy.
//
// Decorative: aria-hidden, single static frame under reduced motion, paused
// on hidden tabs.

const SCALE = 5.8;
const CELL = 3.2;
const DOT_R = 0.95;
const W = 144; // px, ≈ 24.8 model units wide
const H = 142; // px, ≈ 23.3 model units tall

const TINT_BODY = 0;
const TINT_ACCENT = 1;
const TINT_SHADOW = 2;
const TINT_EYE = 3;
const MASCOT_TINTS = [
  [34, 212, 122], // --sprout-green
  [52, 226, 136], // --sprout-green-vivid — stem/leaves
  [10, 40, 26], // shadow, near-background
  [150, 255, 200], // eyes — a clear glint against the body
];
const MASCOT_LEVELS = [0.32, 0.48, 0.66, 0.85];

const BODY = { cx: 12, cy: 14.3, rx: 6.6, ry: 6.1 };
const EYES = { cy: 13.6, dx: 2.5, rx: 1.2, ry: 1.45 };
const STEM = {
  p0: [11.6, 8.6],
  c1: [11.1, 6.4],
  c2: [11.9, 4.6],
  p1: [10.9, 2.6],
};
const LEAF_BIG = { cx: 8.6, cy: 7.1, rx: 2.7, ry: 1.1, rot: -0.4 };
const LEAF_SMALL = { cx: 13.1, cy: 4.1, rx: 2.1, ry: 0.85, rot: 0.5 };
const SHADOW = { cx: 12, cy: 21.3, rx: 6.3, ry: 1.4 };

// The signature move's branch lines (model space, same shapes as the old
// SVG): two worktree nodes that grow out of the body, hold, and merge back.
const BRANCH_L = {
  p0: [6.0, 13.0],
  c1: [4.0, 12.2],
  c2: [2.4, 11.3],
  p1: [1.6, 10.4],
};
const BRANCH_R = {
  p0: [18.1, 15.4],
  c1: [20.2, 16.3],
  c2: [21.7, 17.5],
  p1: [22.6, 18.6],
};

// One repeating personality timeline, in seconds.
const CYCLE = 12;

// Cursor-shove constants, identical to BranchDither's so the mascot's dots
// react to the mouse with exactly the same weight and spring as the
// backdrop's (push is expressed in cell units, gain/caps/decay match, and
// the strike dusts extra density into struck cells the same way).
const BRUSH_R = 3; // cells, same as the backdrop's brush at its default radius
const PUSH_GAIN = 1.7;
const PUSH_MAX = 1.6;
const PUSH_DECAY = 0.8;
const PUSH_EPS = 0.05;
const SPAWN_GAIN = 0.5;
const SPAWN_MAX = 0.7;
const SPAWN_DECAY = 0.76;
const SPAWN_EPS = 0.02;

// Cheap per-cell hash, used to grain the body's fill so the Bayer pattern
// doesn't read as one uniform mechanical checkerboard — the same irregular
// texture the background gets for free from its varying branch density.
function hashNoise(x, y, seed) {
  const v = Math.sin(x * 12.9898 + y * 78.233 + seed * 37.719) * 43758.5453;
  return v - Math.floor(v);
}

// A few sine harmonics around the body's angle give it a slowly-drifting
// organic wobble instead of a perfect ellipse — echoing the hand-drawn blob
// the mascot used to be, rather than a rigid vector shape.
function bodyWobble(theta, t) {
  return (
    1 +
    0.06 * Math.sin(3 * theta + t * 0.3) +
    0.04 * Math.sin(5 * theta - t * 0.22 + 1.3) +
    0.03 * Math.sin(7 * theta + t * 0.17 + 2.6)
  );
}

const clamp01 = (v) => (v < 0 ? 0 : v > 1 ? 1 : v);
const smooth = (v) => v * v * (3 - 2 * v);
// 0→1 progress through the window [a, b] on the timeline, smoothstepped.
const phase = (t, a, b) => clamp01((t - a) / (b - a));
// Rises over [a, b], holds at 1, falls back over [c, d].
const pulse = (t, a, b, c, d) =>
  smooth(phase(t, a, b)) * (1 - smooth(phase(t, c, d)));

function cubicPoint(p0, c1, c2, p1, t) {
  const it = 1 - t;
  const x =
    it * it * it * p0[0] +
    3 * it * it * t * c1[0] +
    3 * it * t * t * c2[0] +
    t * t * t * p1[0];
  const y =
    it * it * it * p0[1] +
    3 * it * it * t * c1[1] +
    3 * it * t * t * c2[1] +
    t * t * t * p1[1];
  return [x, y];
}

type MascotProps = {
  className?: string;
  style?: React.CSSProperties;
  /** Final on-screen width in px; the drawing itself always renders at native (W×H) resolution and is only scaled down for display. */
  displayWidth?: number;
  /** Whether the cursor disturbs the dots, like it does on the page backdrop. */
  interactive?: boolean;
};

export default function Mascot({
  className = "",
  style,
  displayWidth = W,
  interactive = true,
}: MascotProps) {
  const wrapRef = useRef(null);
  const canvasRef = useRef(null);
  const rafRef = useRef(0);

  useEffect(() => {
    const wrap = wrapRef.current;
    const canvas = canvasRef.current;
    if (!wrap || !canvas) return undefined;
    const ctx = canvas.getContext("2d");
    if (!ctx) return undefined;

    const PAL = buildPalette(1, MASCOT_TINTS, MASCOT_LEVELS);

    const reduced =
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const useMouse = interactive && !reduced;

    const dpr = Math.min(2, window.devicePixelRatio || 1);
    canvas.width = Math.round(W * dpr);
    canvas.height = Math.round(H * dpr);
    canvas.style.width = `${displayWidth}px`;
    canvas.style.height = `${displayWidth * (H / W)}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    const cols = Math.ceil(W / CELL);
    const rows = Math.ceil(H / CELL);
    const dens = new Float32Array(cols * rows);
    const tintGrid = new Uint8Array(cols * rows);
    const pushX = new Float32Array(cols * rows);
    const pushY = new Float32Array(cols * rows);
    const spawn = new Float32Array(cols * rows);
    const activeCells = new Set<number>();

    // Model space (the old SVG's 0-24 viewBox) -> canvas pixels, through a
    // per-frame pose: a head tilt about the body center and a squash/stretch
    // anchored at the ground, so the whole character bends together.
    const OX = (W - 24 * SCALE) / 2;
    const OY = 7;
    const pose = { rot: 0, sx: 1, sy: 1 };
    const toPx = (mx, my) => {
      const dx = mx - BODY.cx;
      const dy = my - BODY.cy;
      const c = Math.cos(pose.rot);
      const s = Math.sin(pose.rot);
      let x = BODY.cx + dx * c - dy * s;
      let y = BODY.cy + dx * s + dy * c;
      x = BODY.cx + (x - BODY.cx) * pose.sx;
      y = 20.4 + (y - 20.4) * pose.sy;
      return [OX + x * SCALE, OY + y * SCALE];
    };

    const stampEllipse = (
      cx,
      cy,
      rx,
      ry,
      rot,
      density,
      tint,
      opts: { wobbleT?: number; grain?: number } = {},
    ) => {
      const wobbleT = opts.wobbleT;
      const grain = opts.grain || 0;
      const [pcx, pcy] = toPx(cx, cy);
      const prx = rx * SCALE * pose.sx;
      const pry = ry * SCALE * pose.sy;
      // Scan bounds use the larger radius: under a pose tilt the ellipse's
      // axes no longer align with the grid.
      const pm = Math.max(prx, pry) * 1.2 + 2;
      const gx0 = Math.max(0, Math.floor((pcx - pm) / CELL));
      const gx1 = Math.min(cols - 1, Math.ceil((pcx + pm) / CELL));
      const gy0 = Math.max(0, Math.floor((pcy - pm) / CELL));
      const gy1 = Math.min(rows - 1, Math.ceil((pcy + pm) / CELL));
      const c = Math.cos(rot + pose.rot);
      const s = Math.sin(rot + pose.rot);
      for (let gy = gy0; gy <= gy1; gy++) {
        for (let gx = gx0; gx <= gx1; gx++) {
          const x = gx * CELL + CELL / 2;
          const y = gy * CELL + CELL / 2;
          const dx = x - pcx;
          const dy = y - pcy;
          const ux = dx * c + dy * s;
          const uy = -dx * s + dy * c;
          let r = Math.sqrt((ux / prx) ** 2 + (uy / pry) ** 2);
          if (wobbleT !== undefined)
            r /= bodyWobble(Math.atan2(uy, ux), wobbleT);
          if (r > 1.15) continue;
          const edge = 1 - Math.max(0, (r - 0.85) / 0.3);
          let d = density * Math.min(1, Math.max(0, edge));
          if (grain > 0) {
            const n = hashNoise(gx, gy, tint * 13 + 7);
            d = Math.min(1, Math.max(0, d * (1 - grain + grain * 2 * n)));
          }
          const i = gy * cols + gx;
          if (d <= dens[i]) continue;
          dens[i] = d;
          tintGrid[i] = tint;
        }
      }
    };

    const stampCurve = (p0, c1, c2, p1, brushModel, density, tint) => {
      const steps = 20;
      for (let i = 0; i <= steps; i++) {
        const [mx, my] = cubicPoint(p0, c1, c2, p1, i / steps);
        stampEllipse(mx, my, brushModel, brushModel, 0, density, tint);
      }
    };

    // Cursor interaction — the same shove technique BranchDither uses
    // (strike/decay), just scoped to this small grid and without spawning
    // any new dots: the cursor only displaces dots that are already there.
    const pointer = {
      x: -999,
      y: -999,
      lastX: -999,
      lastY: -999,
      active: false,
      lastMove: -1e6,
    };

    // BranchDither's strike, verbatim (minus the tumbling particles):
    // anisotropic brush stretched along the movement direction, shove
    // scaled by cell size, only cells that currently hold a dot get moved,
    // and struck cells pick up a decaying spawn boost that dusts nearby
    // sub-threshold cells up over the Bayer threshold — the same fresh-dot
    // shimmer the backdrop gets under the cursor.
    const strike = (px, py, amt, dirx, diry) => {
      const R = BRUSH_R;
      const cgx = Math.floor(px / CELL);
      const cgy = Math.floor(py / CELL);
      for (let by = -R; by <= R; by += 1) {
        for (let bx = -R; bx <= R; bx += 1) {
          const gx = cgx + bx;
          const gy = cgy + by;
          if (gx < 0 || gy < 0 || gx >= cols || gy >= rows) continue;
          const i = gy * cols + gx;
          if (dens[i] <= 0.02) continue;
          const along = bx * dirx + by * diry;
          const perp = -bx * diry + by * dirx;
          const dd = Math.sqrt(
            (along / (R + 1)) ** 2 * 0.45 + (perp / R) ** 2 * 1.7,
          );
          if (dd >= 1) continue;
          const fall = 1 - dd;
          const shove = amt * fall * PUSH_GAIN * CELL;
          let nx = pushX[i] + dirx * shove;
          let ny = pushY[i] + diry * shove;
          const cap = PUSH_MAX * CELL;
          nx = nx > cap ? cap : nx < -cap ? -cap : nx;
          ny = ny > cap ? cap : ny < -cap ? -cap : ny;
          pushX[i] = nx;
          pushY[i] = ny;
          const sv = spawn[i] + SPAWN_GAIN * (0.6 + fall);
          spawn[i] = sv > SPAWN_MAX ? SPAWN_MAX : sv;
          activeCells.add(i);
        }
      }
    };

    // Interpolate strikes along the pointer's path since the last frame —
    // same as the backdrop — so a fast swipe rakes through every dot it
    // crosses instead of landing one strike at the endpoint.
    const applyPunch = () => {
      const dmx = pointer.x - pointer.lastX;
      const dmy = pointer.y - pointer.lastY;
      pointer.lastX = pointer.x;
      pointer.lastY = pointer.y;
      const s = Math.hypot(dmx, dmy);
      if (s < 0.5) return;
      const dirx = dmx / s;
      const diry = dmy / s;
      const amt = Math.min(0.6, 0.12 + s * 0.013);
      const steps = Math.min(48, Math.max(1, Math.floor(s / (CELL * 0.7))));
      const ox = pointer.x - dmx;
      const oy = pointer.y - dmy;
      for (let k = 0; k <= steps; k += 1)
        strike(ox + (dmx * k) / steps, oy + (dmy * k) / steps, amt, dirx, diry);
      // A hard swipe across the body startles it (with a refractory gap so
      // continuous scrubbing doesn't lock the face wide-eyed).
      const onBody =
        pointer.x > -8 &&
        pointer.x < W + 8 &&
        pointer.y > -8 &&
        pointer.y < H + 8;
      if (onBody && amt > 0.22 && clock - startleAt > 1.4) startleAt = clock;
    };

    const decayInteraction = () => {
      if (!activeCells.size) return;
      for (const i of activeCells) {
        let px = pushX[i] * PUSH_DECAY;
        let py = pushY[i] * PUSH_DECAY;
        let sv = spawn[i] * SPAWN_DECAY;
        if (Math.abs(px) < PUSH_EPS) px = 0;
        if (Math.abs(py) < PUSH_EPS) py = 0;
        if (sv < SPAWN_EPS) sv = 0;
        pushX[i] = px;
        pushY[i] = py;
        spawn[i] = sv;
        if (px === 0 && py === 0 && sv === 0) activeCells.delete(i);
      }
    };

    let running = false;
    let clock = 0;
    let lastTime = 0;
    const idleInterval = 1000 / 30;
    const activeInterval = 1000 / 45;
    let blinkAt = 3.2 + Math.random() * 2;
    // Eye gaze (model units, spring-smoothed) and the last startle time.
    let gazeX = 0;
    let gazeY = 0;
    let startleAt = -1e6;

    // A partially-grown branch line with a bright worktree node on its tip
    // (the node pops in once the line is most of the way out).
    const drawBranch = (b, p) => {
      if (p <= 0.02) return;
      const steps = 14;
      const m = Math.max(1, Math.round(steps * p));
      for (let i = 0; i <= m; i++) {
        const [mx, my] = cubicPoint(b.p0, b.c1, b.c2, b.p1, i / steps);
        stampEllipse(mx, my, 0.3, 0.3, 0, 0.8, TINT_ACCENT);
      }
      const nodeK = smooth(phase(p, 0.55, 0.9));
      if (nodeK > 0.05) {
        const [nx, ny] = cubicPoint(b.p0, b.c1, b.c2, b.p1, m / steps);
        stampEllipse(nx, ny, 0.62 * nodeK, 0.62 * nodeK, 0, 0.95, TINT_EYE);
      }
    };

    const draw = (t) => {
      dens.fill(0);
      const breathe = Math.sin(t * 0.7) * 0.045;
      const bob = Math.sin(t * 0.7) * 1.1;
      const sway = Math.sin(t * 0.55) * 0.35;
      const cyc = t % CYCLE;

      // ── Personality timeline ──
      let poseRot = 0;
      let sx = 1;
      let sy = 1;
      let eyeSX = 1;
      let eyeSY = 1;
      let droopRad = 0;
      let idleTX = 0;
      let idleTY = 0;

      // The split: worktree nodes branch out, hold, merge back — and the
      // eyes follow their own branches out and back.
      const pL = Math.min(
        smooth(phase(cyc, 3.0, 3.7)),
        1 - smooth(phase(cyc, 5.1, 5.8)),
      );
      const pR = Math.min(
        smooth(phase(cyc, 3.3, 4.0)),
        1 - smooth(phase(cyc, 5.3, 6.0)),
      );
      const lookL = pulse(cyc, 3.0, 3.4, 4.1, 4.5);
      const lookR = pulse(cyc, 4.3, 4.7, 5.5, 5.9);
      idleTX += -0.55 * lookL + 0.55 * lookR;
      idleTY += 0.15 * (lookL + lookR);
      eyeSY *= 1 + 0.08 * Math.max(pL, pR);

      // A brief idle glance to the side early in the cycle.
      idleTX += 0.35 * pulse(cyc, 1.3, 1.6, 2.1, 2.4);

      // Curious: head tilt, eyes widen, gaze lifts.
      const cur = pulse(cyc, 6.8, 7.15, 7.7, 8.05);
      poseRot += ((-9 * Math.PI) / 180) * cur;
      eyeSX *= 1 + 0.12 * cur;
      eyeSY *= 1 + 0.16 * cur;
      idleTY += -0.2 * cur;

      // Sleepy: stem droops, eyes half-lid, gaze sinks, slight slump.
      const slp = pulse(cyc, 8.8, 9.3, 9.9, 10.3);
      droopRad += ((-16 * Math.PI) / 180) * slp;
      eyeSY *= 1 - 0.62 * slp;
      idleTY += 0.3 * slp;
      sy *= 1 - 0.03 * slp;

      // Wake-up bounce: squash, then rebound stretch.
      const bk = phase(cyc, 10.6, 11.5);
      if (bk > 0 && bk < 1) {
        const squash = Math.sin(Math.PI * clamp01(bk / 0.45));
        const stretch = Math.sin(Math.PI * clamp01((bk - 0.45) / 0.55));
        sy *= 1 - 0.13 * squash + 0.09 * stretch;
        sx *= 1 + 0.11 * squash - 0.06 * stretch;
      }

      // Startled by a hard shove: eyes pop wide, quick flinch squash.
      const st = t - startleAt;
      const startled = st >= 0 && st < 0.6;
      if (startled) {
        const k = 1 - st / 0.6;
        eyeSX *= 1 + 0.18 * k;
        eyeSY *= 1 + 0.5 * k;
        const flinch = Math.sin(Math.PI * clamp01(st / 0.22));
        sy *= 1 - 0.08 * flinch;
        sx *= 1 + 0.06 * flinch;
      }

      // ── Gaze: the cursor wins its attention when nearby; otherwise the
      // timeline's glances play out. Spring-smoothed so the eyes glide.
      let gtX = idleTX;
      let gtY = idleTY;
      if (useMouse && pointer.active) {
        const bex = OX + BODY.cx * SCALE;
        const bey = OY + EYES.cy * SCALE;
        const gdx = pointer.x - bex;
        const gdy = pointer.y - bey;
        const gd = Math.hypot(gdx, gdy);
        if (gd > 1 && gd < 2.2 * W) {
          const m = Math.min(1, gd / 40);
          gtX = (gdx / gd) * 0.6 * m;
          gtY = (gdy / gd) * 0.45 * m;
        }
      }
      gazeX += (gtX - gazeX) * 0.14;
      gazeY += (gtY - gazeY) * 0.14;

      // ── Stamping ──
      // Ground shadow first, pose-neutral (the ground doesn't tilt), but it
      // widens when the body squashes.
      pose.rot = 0;
      pose.sx = 1;
      pose.sy = 1;
      const shadowStretch =
        (1 + Math.sin(t * 0.7 + 0.3) * 0.05) * (1 + (1 - sy) * 1.2);
      stampEllipse(
        SHADOW.cx,
        SHADOW.cy - bob * 0.15,
        SHADOW.rx * shadowStretch,
        SHADOW.ry,
        0,
        0.55,
        TINT_SHADOW,
      );

      // Everything else bends together through the pose.
      pose.rot = poseRot;
      pose.sx = sx;
      pose.sy = sy;

      // The split branches (drawn under the body so they read as growing
      // out of it).
      drawBranch(BRANCH_L, pL);
      drawBranch(BRANCH_R, pR);

      // Stem, swaying from its base pivot (plus the sleepy droop).
      const swayRad = (Math.PI / 180) * sway * 14 + droopRad;
      const pivot = STEM.p0;
      const rotPt = (p) => {
        const dx = p[0] - pivot[0];
        const dy = p[1] - pivot[1];
        const c = Math.cos(swayRad);
        const s = Math.sin(swayRad);
        return [pivot[0] + dx * c - dy * s, pivot[1] + dx * s + dy * c];
      };
      stampCurve(
        STEM.p0,
        rotPt(STEM.c1),
        rotPt(STEM.c2),
        rotPt(STEM.p1),
        0.42,
        0.85,
        TINT_ACCENT,
      );

      // Leaves, carried along with the stem sway.
      const leafSway = swayRad * 0.6;
      stampEllipse(
        LEAF_BIG.cx,
        LEAF_BIG.cy,
        LEAF_BIG.rx,
        LEAF_BIG.ry,
        LEAF_BIG.rot + leafSway,
        0.9,
        TINT_ACCENT,
        { grain: 0.25 },
      );
      stampEllipse(
        LEAF_SMALL.cx,
        LEAF_SMALL.cy,
        LEAF_SMALL.rx,
        LEAF_SMALL.ry,
        LEAF_SMALL.rot + leafSway,
        0.9,
        TINT_ACCENT,
        { grain: 0.25 },
      );

      // Body, gently breathing, its silhouette wobbling like a hand-drawn
      // blob rather than a rigid ellipse, and its fill grained so it isn't
      // one uniform Bayer checkerboard.
      stampEllipse(
        BODY.cx,
        BODY.cy + bob * 0.4,
        BODY.rx * (1 + breathe),
        BODY.ry * (1 - breathe * 0.6),
        0,
        0.62,
        TINT_BODY,
        { wobbleT: t, grain: 0.4 },
      );

      // Eyes: gaze offset + expression scaling + blink. Blinks are held off
      // while startled (wide eyes don't blink), and occasionally come as a
      // quick double-blink.
      const sinceBlink = t - blinkAt;
      let blinkK = 1;
      if (startled) {
        if (sinceBlink >= 0) blinkAt = t + 1.2 + Math.random() * 2;
      } else if (sinceBlink >= 0 && sinceBlink < 0.16) {
        const p = sinceBlink / 0.16;
        blinkK = p < 0.5 ? 1 - p * 1.8 : (p - 0.5) * 1.8;
      } else if (sinceBlink >= 0.16) {
        blinkAt = t + (Math.random() < 0.18 ? 0.3 : 2.6 + Math.random() * 2.8);
      }
      const eyeCy = EYES.cy + bob * 0.4 + gazeY;
      const eyeRx = EYES.rx * eyeSX;
      const eyeRy = Math.max(0.12, EYES.ry * eyeSY * blinkK);
      stampEllipse(
        BODY.cx - EYES.dx + gazeX,
        eyeCy,
        eyeRx,
        eyeRy,
        0,
        0.95,
        TINT_EYE,
      );
      stampEllipse(
        BODY.cx + EYES.dx + gazeX,
        eyeCy,
        eyeRx,
        eyeRy,
        0,
        0.95,
        TINT_EYE,
      );

      if (useMouse) {
        if (pointer.active) applyPunch();
        decayInteraction();
      }

      ctx.clearRect(0, 0, W, H);
      paintDots(ctx, {
        cols,
        v0: 0,
        v1: rows,
        cell: CELL,
        dotR: DOT_R,
        dens,
        tintGrid,
        PAL,
        spawn,
        pushX,
        pushY,
      });
    };

    const tick = (time) => {
      const movingRecently = useMouse && time - pointer.lastMove < 200;
      const active = useMouse && (movingRecently || activeCells.size);
      const interval = active ? activeInterval : idleInterval;
      if (time - lastTime >= interval) {
        const dt = lastTime ? Math.min((time - lastTime) / 1000, 0.1) : 0.016;
        clock += dt;
        lastTime = time;
        draw(clock);
      }
      if (running) rafRef.current = requestAnimationFrame(tick);
    };

    const start = () => {
      if (!running && !document.hidden) {
        running = true;
        rafRef.current = requestAnimationFrame(tick);
      }
    };
    const stop = () => {
      running = false;
      cancelAnimationFrame(rafRef.current);
    };

    if (reduced) {
      draw(0.6);
    } else {
      start();
    }

    const onVis = () => {
      if (reduced) return;
      if (document.hidden) stop();
      else start();
    };
    document.addEventListener("visibilitychange", onVis);

    const onMove = (e) => {
      const rect = canvas.getBoundingClientRect();
      const x = (e.clientX - rect.left) * (W / rect.width);
      const y = (e.clientY - rect.top) * (H / rect.height);
      if (!pointer.active) {
        pointer.lastX = x;
        pointer.lastY = y;
      }
      pointer.x = x;
      pointer.y = y;
      pointer.active = true;
      pointer.lastMove = e.timeStamp || performance.now();
    };
    const onLeave = () => {
      pointer.active = false;
    };
    if (useMouse) {
      window.addEventListener("pointermove", onMove, { passive: true });
      window.addEventListener("blur", onLeave);
      document.addEventListener("mouseleave", onLeave);
    }

    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVis);
      if (useMouse) {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("blur", onLeave);
        document.removeEventListener("mouseleave", onLeave);
      }
    };
  }, [displayWidth, interactive]);

  const dispH = Math.round(displayWidth * (H / W));

  return (
    <div
      ref={wrapRef}
      aria-hidden="true"
      className={`${fieldStyles.field} ${className}`.trim()}
      style={{
        position: "relative",
        width: displayWidth,
        height: dispH,
        ...style,
      }}
    >
      <canvas ref={canvasRef} className={fieldStyles.canvas} />
    </div>
  );
}
