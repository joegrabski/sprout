// Sprout README banner — built on the site's *new* dither effect (BranchDither.js):
// recursive organic trees rasterised into a density grid, then quantised through a
// 4x4 Bayer matrix into small dark-green dither dots. Here the forest is rooted
// along the bottom edge and grown fully (static frame), with the sprout mark +
// wordmark composited on top. Deterministic (seeded LCG, no Math.random).
import { writeFileSync } from "node:fs";

const W = 1280, H = 360;
const cell = 6;      // dot spacing (px)
const dotR = 1.5;    // dot half-size (px)
const INTENSITY = 1.25;

// --- ported verbatim from BranchDither.js ---
const BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5].map((v) => (v + 0.5) / 16);
const LEVEL_ALPHA = [0.2, 0.32, 0.46, 0.6];
const N = LEVEL_ALPHA.length;
const TINTS = [
  [22, 74, 48], [28, 86, 54], [17, 63, 42], [32, 92, 58], [24, 79, 50],
];
const BRICK = 0.34;

function hash2(ix, iy) {
  let h = Math.imul(ix | 0, 374761393) + Math.imul(iy | 0, 668265263);
  h = Math.imul(h ^ (h >>> 13), 1274126177);
  h ^= h >>> 16;
  return (h >>> 0) / 4294967296;
}

const cols = Math.ceil(W / cell);
const rows = Math.ceil(H / cell);
const dens = new Float32Array(cols * rows);
const tintGrid = new Uint8Array(cols * rows);
const segs = [];

// buildTree — recursive curved segments in cell coords (ported).
function buildTree(tint, rx, ry, ang, len, depth, rng) {
  const x1 = rx + Math.cos(ang) * len;
  const y1 = ry + Math.sin(ang) * len;
  const nx = -(y1 - ry), ny = x1 - rx;
  const nl = Math.hypot(nx, ny) || 1;
  const bow = (rng() - 0.5) * len * 0.6;
  segs.push({
    tint,
    x0: rx, y0: ry,
    cx: (rx + x1) / 2 + (nx / nl) * bow,
    cy: (ry + y1) / 2 + (ny / nl) * bow,
    x1, y1,
    brush: depth >= 3 ? 1.7 : depth >= 1 ? 1.15 : 0.8,
    amp: 0.7 + depth * 0.13,
  });
  if (depth <= 0) return;
  const kids = rng() < 0.25 ? 3 : 2;
  for (let i = 0; i < kids; i++) {
    const spread = 0.5 + rng() * 0.6;
    const side = kids === 3 && i === 1 ? 0 : i === 0 ? -1 : 1;
    const a = ang + side * spread + (rng() - 0.5) * 0.3;
    const l = len * (0.6 + rng() * 0.18);
    buildTree(tint, x1, y1, a, l, depth - 1, rng);
  }
}

// Forest rooted along the bottom edge, grown upward — a lush sprout silhouette.
function buildForest() {
  let seed = (cols * 2654435761) ^ (rows * 40503) ^ 0x51ed;
  const rng = () => { seed = (seed * 1103515245 + 12345) & 0x7fffffff; return seed / 0x7fffffff; };
  const unit = rows / 4.6;
  const trees = Math.round(cols / 17);
  for (let i = 0; i < trees; i++) {
    const rx = ((i + 0.5) / trees) * cols + (rng() - 0.5) * (cols / trees) * 0.7;
    const ry = rows * (0.98 + rng() * 0.12);            // root at/just below the bottom
    const ang = -Math.PI / 2 + (rng() - 0.5) * 0.5;     // grow up
    const len = unit * (0.9 + rng() * 0.55);
    const depth = 4 + (rng() < 0.5 ? 1 : 0);
    const tint = (rng() * TINTS.length) | 0;
    buildTree(tint, rx, ry, ang, len, depth, rng);
  }
}

// rasterSeg at full growth (pEnd=1) — stamp density + tint (ported).
function rasterSeg(s) {
  const amp = s.amp;
  const chord = Math.hypot(s.x1 - s.x0, s.y1 - s.y0);
  const n = Math.max(2, Math.ceil(chord) + 1);
  const br = s.brush;
  const R = Math.ceil(br);
  for (let k = 0; k <= n; k++) {
    const t = k / n, it = 1 - t;
    const px = it * it * s.x0 + 2 * it * t * s.cx + t * t * s.x1;
    const py = it * it * s.y0 + 2 * it * t * s.cy + t * t * s.y1;
    const cxr = Math.round(px), cyr = Math.round(py);
    for (let by = -R; by <= R; by++) {
      for (let bx = -R; bx <= R; bx++) {
        const gx = cxr + bx, gy = cyr + by;
        if (gx < 0 || gy < 0 || gx >= cols || gy >= rows) continue;
        const dd = Math.hypot(bx, by) / (br + 0.5);
        if (dd >= 1) continue;
        const i = gy * cols + gx;
        let v = dens[i] + amp * (1 - dd);
        if (v > 1.5) v = 1.5;
        dens[i] = v;
        tintGrid[i] = s.tint;
      }
    }
  }
}

buildForest();
for (const s of segs) rasterSeg(s);

// Legibility: fade dots inside a central ellipse so the wordmark stays clean.
const cx = W * 0.5, cyc = H * 0.46, maskRx = 430, maskRy = 118;
function clearFactor(px, py) {
  const dx = (px - cx) / maskRx, dy = (py - cyc) / maskRy;
  const d = Math.sqrt(dx * dx + dy * dy);
  if (d >= 1) return 1;
  return Math.max(0, (d - 0.32) / 0.68);
}

// Bayer-dither the density grid into dots.
let dots = "";
const d2 = (dotR * 2).toFixed(1);
for (let gy = 0; gy < rows; gy++) {
  for (let gx = 0; gx < cols; gx++) {
    const i = gy * cols + gx;
    const d = dens[i];
    if (d <= 0.02) continue;
    if (d < BAYER[(gy & 3) * 4 + (gx & 3)]) continue;
    let lvl = (d * N) | 0;
    if (lvl > N - 1) lvl = N - 1;
    const px = gx * cell + cell / 2 - dotR;
    const py = gy * cell + cell / 2 - dotR;
    const [r, g, b] = TINTS[tintGrid[i]];
    const a = LEVEL_ALPHA[lvl] * INTENSITY * clearFactor(px, py);
    if (a < 0.02) continue;
    dots += `<rect x="${px.toFixed(1)}" y="${py.toFixed(1)}" width="${d2}" height="${d2}" fill="rgb(${r},${g},${b})" fill-opacity="${a.toFixed(3)}"/>\n`;
  }
}

// --- lockup: the hand-authored dithered sprout mark left of the wordmark ---
// Dots are the same layout as logo.svg (24u space), centred at ~(12, 12.8).
const MARK_DOTS = `
  <rect x="3.06" y="3.89" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="4.72" y="3.89" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="18.00" y="3.89" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="19.66" y="3.89" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="3.06" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="4.72" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="6.38" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="16.34" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="18.00" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="19.66" y="5.55" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="4.72" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="6.38" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="8.04" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="14.68" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="16.34" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="18.00" y="7.21" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="6.38" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="8.04" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="9.70" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="13.02" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="14.68" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="16.34" y="8.87" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="8.04" y="10.53" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="9.70" y="10.53" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="10.53" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="13.02" y="10.53" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="14.68" y="10.53" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="9.70" y="12.19" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="12.19" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="13.02" y="12.19" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="13.85" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="15.51" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="17.17" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="9.70" y="18.83" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="18.83" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="13.02" y="18.83" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="9.70" y="20.49" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="11.36" y="20.49" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
  <rect x="13.02" y="20.49" width="1.28" height="1.28" rx="0.32" fill="url(#leaf)"/>
`;
const yc = 150, markS = 4.0, mCx = 498;
const mtx = mCx - 12 * markS, mty = yc - 12.8 * markS;
const wordX = 560, wordBaseline = yc + 30;
const mark = `
<g transform="translate(${mtx.toFixed(1)} ${mty.toFixed(1)}) scale(${markS})">${MARK_DOTS}</g>`;

const svg = `<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg" font-family="SF Mono, ui-monospace, Menlo, monospace">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="${H}" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#0a0f0b"/>
      <stop offset="1" stop-color="#070b08"/>
    </linearGradient>
    <radialGradient id="glow" cx="50%" cy="44%" r="60%">
      <stop offset="0" stop-color="#0c2418" stop-opacity="0.8"/>
      <stop offset="1" stop-color="#0c2418" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="leaf" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#3aec96"/>
      <stop offset="1" stop-color="#0fae61"/>
    </linearGradient>
    <linearGradient id="word" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#f4fff9"/>
      <stop offset="1" stop-color="#bff3d6"/>
    </linearGradient>
  </defs>
  <rect width="${W}" height="${H}" fill="url(#bg)"/>
  <ellipse cx="${cx}" cy="${H * 0.44}" rx="520" ry="210" fill="url(#glow)"/>
  <g shape-rendering="crispEdges">
${dots}  </g>
  ${mark}
  <text x="${wordX}" y="${wordBaseline}" font-size="84" font-weight="800" letter-spacing="-1.5" fill="url(#word)">sprout</text>
  <text x="${cx}" y="238" text-anchor="middle" font-size="19" font-weight="500" letter-spacing="4.4" fill="#5fd39b" fill-opacity="0.92">GIT WORKTREES · WITHOUT THE CONTEXT SWITCHING</text>
</svg>
`;

writeFileSync("banner.svg", svg);
console.log(`wrote banner.svg — ${segs.length} segments, ${dots.split("\n").length - 1} dots, ${cols}x${rows} grid`);
