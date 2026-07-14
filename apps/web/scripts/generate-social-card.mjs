// Regenerate: node apps/web/scripts/generate-social-card.mjs && rsvg-convert social.svg -w 1200 -h 630 -o apps/web/static/img/social-card.png
import { writeFileSync } from "node:fs";
import { branchField, markDots } from "./brand-lib.mjs";

const W = 1200, H = 630, cx = W / 2;

const field = branchField({
  W, H, cell: 7, dotR: 1.6, intensity: 0.9, treeDiv: 24, seed: 0x2b17,
  mask: (x, y) => {
    const up = Math.min(1, Math.max(0, (y - H * 0.5) / (H * 0.42))); // lower half only
    const cxd = Math.abs(x - cx) / (W * 0.5);
    const centre = 0.5 + 0.5 * Math.min(1, cxd / 0.4);
    return up * centre;
  },
});

// Stacked lockup: mark on top, wordmark, tagline, domain.
const markS = 6.6, markCenterY = 196;
const mtx = cx - 12 * markS, mty = markCenterY - 12.83 * markS;

const svg = `<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg" font-family="SF Mono, ui-monospace, Menlo, monospace">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="${H}" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#0c1712"/><stop offset="1" stop-color="#080d0a"/>
    </linearGradient>
    <radialGradient id="halo" cx="50%" cy="34%" r="62%">
      <stop offset="0" stop-color="#1f7a52" stop-opacity="0.5"/>
      <stop offset="0.55" stop-color="#124d34" stop-opacity="0.15"/>
      <stop offset="1" stop-color="#0b2a1c" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="leaf" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#48f0a0"/><stop offset="1" stop-color="#12b268"/>
    </linearGradient>
    <linearGradient id="word" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#f3fff9"/><stop offset="1" stop-color="#c6f4da"/>
    </linearGradient>
  </defs>
  <rect width="${W}" height="${H}" fill="url(#bg)"/>
  <rect width="${W}" height="${H}" fill="url(#halo)"/>
  <g shape-rendering="crispEdges">${field}</g>
  <g transform="translate(${mtx.toFixed(1)} ${mty.toFixed(1)}) scale(${markS})">${markDots()}</g>
  <text x="${cx}" y="388" text-anchor="middle" font-size="132" font-weight="800" letter-spacing="-3" fill="url(#word)">sprout</text>
  <text x="${cx}" y="448" text-anchor="middle" font-size="26" font-weight="500" letter-spacing="6" fill="#6fdca6" fill-opacity="0.92">GIT WORKTREES · WITHOUT THE CONTEXT SWITCHING</text>
  <text x="${cx}" y="566" text-anchor="middle" font-size="24" font-weight="600" letter-spacing="3" fill="#3f7d61">sprout.dev</text>
</svg>
`;
writeFileSync("social.svg", svg);
console.log("wrote social.svg");