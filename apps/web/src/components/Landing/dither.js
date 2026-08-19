// Shared dot-dither engine — the same Bayer-quantized stipple used by the
// page's BranchDither backdrop, factored out so any other canvas (the
// mascot included) paints with the identical palette and threshold matrix
// instead of a one-off look-alike.

export const BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5].map(
  (v) => (v + 0.5) / 16,
);

export const LEVEL_ALPHA = [0.2, 0.32, 0.46, 0.6];
export const N = LEVEL_ALPHA.length;

// Subtle green tints only — no teal/lime drift.
export const TINTS = [
  [22, 74, 48],
  [28, 86, 54],
  [17, 63, 42],
  [32, 92, 58],
  [24, 79, 50],
];

export function buildPalette(
  intensity = 1,
  tints = TINTS,
  levels = LEVEL_ALPHA,
) {
  return tints.map(([r, g, b]) =>
    levels.map((a) => `rgba(${r}, ${g}, ${b}, ${(a * intensity).toFixed(3)})`),
  );
}

// Given a density grid (0..~1.5 per cell) and a matching tint index per
// cell, quantize through the 4x4 Bayer matrix into small dots — the exact
// technique BranchDither uses for its growing-branch field.
export function paintDots(
  ctx,
  {
    cols,
    v0,
    v1,
    cell,
    dotR,
    dens,
    tintGrid,
    PAL,
    spawn = null,
    pushX = null,
    pushY = null,
  },
) {
  const d2 = dotR * 2;
  for (let gy = v0; gy < v1; gy++) {
    const yb = (gy & 3) * 4;
    const cyc = gy * cell + cell / 2 - dotR;
    for (let gx = 0; gx < cols; gx++) {
      const i = gy * cols + gx;
      const d = dens[i] + (spawn ? spawn[i] : 0);
      if (d <= 0.02) continue;
      if (d < BAYER[yb + (gx & 3)]) continue;
      let lvl = (d * N) | 0;
      if (lvl > N - 1) lvl = N - 1;
      ctx.fillStyle = PAL[tintGrid[i]][lvl];
      const px = pushX ? pushX[i] : 0;
      const py = pushY ? pushY[i] : 0;
      ctx.fillRect(gx * cell + cell / 2 - dotR + px, cyc + py, d2, d2);
    }
  }
}
