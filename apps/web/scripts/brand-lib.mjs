// Shared pieces for the banner + social card: the branch-dither field (ported
// from the site's BranchDither.js) and the hand-made sprout mark.
import { readFileSync } from "node:fs";

const BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5].map((v) => (v + 0.5) / 16);
const LEVEL_ALPHA = [0.2, 0.32, 0.46, 0.6];
const N = LEVEL_ALPHA.length;
const TINTS = [
  [26, 84, 54], [32, 96, 60], [20, 70, 46], [36, 104, 64], [28, 88, 56],
];

function hash2(ix, iy) {
  let h = Math.imul(ix | 0, 374761393) + Math.imul(iy | 0, 668265263);
  h = Math.imul(h ^ (h >>> 13), 1274126177);
  h ^= h >>> 16;
  return (h >>> 0) / 4294967296;
}

// Render a dithered branch field for a WxH canvas. Trees are rooted along the
// bottom and grown fully; density is Bayer-dithered into dots. `mask(x,y)`
// returns a 0..1 multiplier on each dot's alpha (fade behind the lockup / top).
// Kept sparse + dim so it reads as texture, not noise.
export function branchField({ W, H, cell = 6, dotR = 1.4, intensity = 0.9, treeDiv = 30, seed = 0x51ed, mask }) {
  const cols = Math.ceil(W / cell);
  const rows = Math.ceil(H / cell);
  const dens = new Float32Array(cols * rows);
  const tint = new Uint8Array(cols * rows);
  const segs = [];

  let s = (cols * 2654435761) ^ (rows * 40503) ^ seed;
  const rng = () => { s = (s * 1103515245 + 12345) & 0x7fffffff; return s / 0x7fffffff; };

  const buildTree = (t, rx, ry, ang, len, depth) => {
    const x1 = rx + Math.cos(ang) * len, y1 = ry + Math.sin(ang) * len;
    const nx = -(y1 - ry), ny = x1 - rx, nl = Math.hypot(nx, ny) || 1;
    const bow = (rng() - 0.5) * len * 0.6;
    segs.push({
      t, x0: rx, y0: ry,
      cx: (rx + x1) / 2 + (nx / nl) * bow, cy: (ry + y1) / 2 + (ny / nl) * bow,
      x1, y1, brush: depth >= 3 ? 1.6 : depth >= 1 ? 1.1 : 0.75, amp: 0.62 + depth * 0.12,
    });
    if (depth <= 0) return;
    const kids = rng() < 0.22 ? 3 : 2;
    for (let i = 0; i < kids; i++) {
      const spread = 0.55 + rng() * 0.55;
      const side = kids === 3 && i === 1 ? 0 : i === 0 ? -1 : 1;
      const a = ang + side * spread + (rng() - 0.5) * 0.3;
      buildTree(t, x1, y1, a, len * (0.6 + rng() * 0.16), depth - 1);
    }
  };

  const unit = rows / 4.2;
  const trees = Math.max(3, Math.round(cols / treeDiv));
  for (let i = 0; i < trees; i++) {
    const rx = ((i + 0.5) / trees) * cols + (rng() - 0.5) * (cols / trees) * 0.6;
    const ry = rows * (0.99 + rng() * 0.14);
    const ang = -Math.PI / 2 + (rng() - 0.5) * 0.45;
    buildTree(i % TINTS.length, rx, ry, ang, unit * (0.9 + rng() * 0.5), 4 + (rng() < 0.4 ? 1 : 0));
  }

  for (const seg of segs) {
    const chord = Math.hypot(seg.x1 - seg.x0, seg.y1 - seg.y0);
    const n = Math.max(2, Math.ceil(chord) + 1);
    const br = seg.brush, R = Math.ceil(br);
    for (let k = 0; k <= n; k++) {
      const tt = k / n, it = 1 - tt;
      const px = it * it * seg.x0 + 2 * it * tt * seg.cx + tt * tt * seg.x1;
      const py = it * it * seg.y0 + 2 * it * tt * seg.cy + tt * tt * seg.y1;
      const cxr = Math.round(px), cyr = Math.round(py);
      for (let by = -R; by <= R; by++) for (let bx = -R; bx <= R; bx++) {
        const gx = cxr + bx, gy = cyr + by;
        if (gx < 0 || gy < 0 || gx >= cols || gy >= rows) continue;
        const dd = Math.hypot(bx, by) / (br + 0.5);
        if (dd >= 1) continue;
        const i = gy * cols + gx;
        let v = dens[i] + seg.amp * (1 - dd);
        if (v > 1.5) v = 1.5;
        dens[i] = v; tint[i] = seg.t;
      }
    }
  }

  let out = "";
  const d2 = (dotR * 2).toFixed(1);
  for (let gy = 0; gy < rows; gy++) {
    for (let gx = 0; gx < cols; gx++) {
      const i = gy * cols + gx, d = dens[i];
      if (d <= 0.02 || d < BAYER[(gy & 3) * 4 + (gx & 3)]) continue;
      let lvl = (d * N) | 0; if (lvl > N - 1) lvl = N - 1;
      const px = gx * cell + cell / 2 - dotR, py = gy * cell + cell / 2 - dotR;
      let a = LEVEL_ALPHA[lvl] * intensity;
      if (mask) a *= mask(px + dotR, py + dotR);
      if (a < 0.015) continue;
      const [r, g, b] = TINTS[tint[i]];
      out += `<rect x="${px.toFixed(1)}" y="${py.toFixed(1)}" width="${d2}" height="${d2}" fill="rgb(${r},${g},${b})" fill-opacity="${a.toFixed(3)}"/>`;
    }
  }
  return out;
}

// Hand-made dithered sprout mark (24u space) as <rect> dots with the given fill.
export function markDots(fill = "url(#leaf)") {
  return readFileSync(new URL("./brand-mark.txt", import.meta.url), "utf8")
    .trim().split("\n").map((ln) => {
      const [x, y] = ln.trim().split(/\s+/);
      return `<rect x="${x}" y="${y}" width="1.28" height="1.28" rx="0.32" fill="${fill}"/>`;
    }).join("");
}
