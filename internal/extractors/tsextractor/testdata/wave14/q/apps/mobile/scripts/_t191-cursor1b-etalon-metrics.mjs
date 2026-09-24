#!/usr/bin/env node
/**
 * Cursor1b verification:
 * 1) Chrome ghost gate (top/bottom) after dilate+6 residual hole
 * 2) PRIMARY fidelity metric: exact-SVG CheckCircle (3482:28947 cleaned+owner glass)
 *    vs illustration-onboarding-no-leak-api.png icon slot (not SVG-vs-PNG).
 *
 * Usage (from apps/mobile):
 *   node scripts/_t191-cursor1b-etalon-metrics.mjs
 */
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { PNG } from 'pngjs';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const pixelmatchMod = require('pixelmatch');
const pixelmatch = typeof pixelmatchMod === 'function' ? pixelmatchMod : pixelmatchMod.default;

const mobileRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const outDir = join(
  mobileRoot,
  'e2e/artifacts/worker-reports/t191-fidelity/GlassIconCheckCircle/cursor1-exact-svg',
);
mkdirSync(outDir, { recursive: true });

// NoLeak icon slot (from derive-icon-slot-vignette TARGETS)
const NOLEAK = {
  etalon: join(mobileRoot, 'e2e/figma-component-baselines/ios/illustration-onboarding-no-leak-api.png'),
  cardOrigin: [61, 65],
  iconSlotInCard: [100, 24],
  iconSize: 40,
  center: [180, 103],
};
const iconAbsLeft = NOLEAK.cardOrigin[0] + NOLEAK.iconSlotInCard[0]; // 161
const iconAbsTop = NOLEAK.cardOrigin[1] + NOLEAK.iconSlotInCard[1]; // 89

const cleanedSvgPath = join(
  mobileRoot,
  'e2e/artifacts/worker-reports/t191-figma-refs/glass-icons/glass-icon-check-circle-3482-28947-cleaned.svg',
);
const cleanedSvg = readFileSync(cleanedSvgPath, 'utf8');

function readPng(path) {
  return PNG.sync.read(readFileSync(path));
}
function writePng(path, png) {
  writeFileSync(path, PNG.sync.write(png));
}
function luma(r, g, b) {
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function crop(png, x0, y0, w, h) {
  const out = new PNG({ width: w, height: h });
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const sx = Math.max(0, Math.min(png.width - 1, x0 + x));
      const sy = Math.max(0, Math.min(png.height - 1, y0 + y));
      const si = (sy * png.width + sx) * 4;
      const di = (y * w + x) * 4;
      out.data[di] = png.data[si];
      out.data[di + 1] = png.data[si + 1];
      out.data[di + 2] = png.data[si + 2];
      out.data[di + 3] = 255;
    }
  }
  return out;
}

function scaleNearest(src, scale) {
  const w = src.width * scale;
  const h = src.height * scale;
  const out = new PNG({ width: w, height: h });
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const sx = Math.min(src.width - 1, Math.floor(x / scale));
      const sy = Math.min(src.height - 1, Math.floor(y / scale));
      const si = (sy * src.width + sx) * 4;
      const di = (y * w + x) * 4;
      out.data[di] = src.data[si];
      out.data[di + 1] = src.data[si + 1];
      out.data[di + 2] = src.data[si + 2];
      out.data[di + 3] = 255;
    }
  }
  return out;
}

function compositeOnCanvas(src, canvasRgb = [13, 14, 15]) {
  const out = new PNG({ width: src.width, height: src.height });
  for (let i = 0; i < src.width * src.height; i++) {
    const o = i * 4;
    const a = (src.data[o + 3] ?? 255) / 255;
    out.data[o] = Math.round(src.data[o] * a + canvasRgb[0] * (1 - a));
    out.data[o + 1] = Math.round(src.data[o + 1] * a + canvasRgb[1] * (1 - a));
    out.data[o + 2] = Math.round(src.data[o + 2] * a + canvasRgb[2] * (1 - a));
    out.data[o + 3] = 255;
  }
  return out;
}

function regionMetrics(a, b, thr = 0.05) {
  const w = a.width;
  const h = a.height;
  const diff = new PNG({ width: w, height: h });
  const mismatched = pixelmatch(a.data, b.data, diff.data, w, h, {
    threshold: thr,
    includeAA: true,
  });
  const total = w * h;
  const cx = w / 2;
  const cy = h / 2;
  let coreMis = 0;
  let coreN = 0;
  let rimMis = 0;
  let rimN = 0;
  let sumAbs = 0;
  let maxAbs = 0;
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = (y * w + x) * 4;
      const d = Math.max(
        Math.abs(a.data[i] - b.data[i]),
        Math.abs(a.data[i + 1] - b.data[i + 1]),
        Math.abs(a.data[i + 2] - b.data[i + 2]),
      );
      sumAbs += d;
      maxAbs = Math.max(maxAbs, d);
      const r = Math.hypot(x + 0.5 - cx, y + 0.5 - cy);
      const isMis = diff.data[i] > 40 || diff.data[i + 1] > 40; // pixelmatch marks red
      if (r <= w * 0.28) {
        coreN++;
        if (isMis) coreMis++;
      } else if (r <= w * 0.48) {
        rimN++;
        if (isMis) rimMis++;
      }
    }
  }
  return {
    thr,
    size: w,
    mismatched,
    total,
    ratio: mismatched / total,
    meanAbsMaxChannel: sumAbs / total,
    maxAbsMaxChannel: maxAbs,
    core: { mismatched: coreMis, total: coreN, ratio: coreN ? coreMis / coreN : 0 },
    rim: { mismatched: rimMis, total: rimN, ratio: rimN ? rimMis / rimN : 0 },
    diff,
  };
}

function hstrip(images, gap = 8) {
  const h = Math.max(...images.map((i) => i.height));
  const w = images.reduce((s, i) => s + i.width, 0) + gap * (images.length - 1);
  const out = new PNG({ width: w, height: h });
  for (let i = 0; i < out.data.length; i += 4) {
    out.data[i] = 13;
    out.data[i + 1] = 14;
    out.data[i + 2] = 15;
    out.data[i + 3] = 255;
  }
  let x0 = 0;
  for (const img of images) {
    for (let y = 0; y < img.height; y++) {
      for (let x = 0; x < img.width; x++) {
        const si = (y * img.width + x) * 4;
        const di = (y * out.width + (x0 + x)) * 4;
        out.data[di] = img.data[si];
        out.data[di + 1] = img.data[si + 1];
        out.data[di + 2] = img.data[si + 2];
        out.data[di + 3] = 255;
      }
    }
    x0 += img.width + gap;
  }
  return out;
}

/** Chrome underlay ghost: top/bottom radial smoothness after dilate+6 hole. */
function chromeTopBottomGate(chromePath, cardOrigin, center, iconR = 20) {
  const chrome = readPng(chromePath);
  const [ox, oy] = cardOrigin;
  const ccx = center[0] - ox;
  const ccy = center[1] - oy;
  const bands = {
    top: { y0: Math.round(ccy - iconR - 8), y1: Math.round(ccy - iconR + 1) },
    bottom: { y0: Math.round(ccy + iconR - 1), y1: Math.round(ccy + iconR + 8) },
  };
  const out = {};
  for (const [name, b] of Object.entries(bands)) {
    let maxStep = 0;
    let green = 0;
    let n = 0;
    const midX = Math.round(ccx);
    const Ls = [];
    for (let y = b.y0; y <= b.y1; y++) {
      if (y < 0 || y >= chrome.height || midX < 0 || midX >= chrome.width) continue;
      const i = (y * chrome.width + midX) * 4;
      const r = chrome.data[i];
      const g = chrome.data[i + 1];
      const bl = chrome.data[i + 2];
      const L = luma(r, g, bl);
      Ls.push(L);
      n++;
      if (g > r + 20 && g > bl + 20 && g > 70) green++;
    }
    for (let k = 1; k < Ls.length; k++) maxStep = Math.max(maxStep, Math.abs(Ls[k] - Ls[k - 1]));
    // 64-ray second-diff in annulus
    let maxSecond = 0;
    for (let i = 0; i < 64; i++) {
      const ang = (i / 64) * Math.PI * 2;
      const samples = [];
      for (let d = iconR - 2; d <= iconR + 8; d++) {
        const x = Math.round(ccx + d * Math.cos(ang));
        const y = Math.round(ccy + d * Math.sin(ang));
        if (x < 0 || y < 0 || x >= chrome.width || y >= chrome.height) continue;
        const idx = (y * chrome.width + x) * 4;
        samples.push(luma(chrome.data[idx], chrome.data[idx + 1], chrome.data[idx + 2]));
      }
      for (let k = 1; k < samples.length - 1; k++) {
        maxSecond = Math.max(maxSecond, Math.abs(samples[k + 1] - 2 * samples[k] + samples[k - 1]));
      }
    }
    out[name] = {
      n,
      midlineMaxStep: maxStep,
      residualGreen: green,
      annulusMaxSecondDiff: maxSecond,
      pass: green === 0 && maxSecond < 25 && maxStep < 25,
      Ls: Ls.map((v) => Number(v.toFixed(1))),
    };
  }
  return {
    top: out.top,
    bottom: out.bottom,
    pass: out.top.pass && out.bottom.pass,
  };
}

async function renderExactSvg(size) {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({
    viewport: { width: size + 16, height: size + 16 },
    deviceScaleFactor: 1,
  });
  const html = `<!doctype html><html><body style="margin:0;background:#0d0e0f">
<div id="host" style="width:${size}px;height:${size}px;line-height:0">
<style>#host > svg { width:100%; height:100%; display:block; }</style>
${cleanedSvg}
</div></body></html>`;
  await page.setContent(html, { waitUntil: 'load' });
  await page.waitForTimeout(80);
  const buf = await page.locator('#host').screenshot({ type: 'png' });
  await browser.close();
  return compositeOnCanvas(PNG.sync.read(buf));
}

async function main() {
  if (!existsSync(NOLEAK.etalon)) throw new Error(`missing etalon ${NOLEAK.etalon}`);
  if (!existsSync(cleanedSvgPath)) throw new Error(`missing cleaned svg ${cleanedSvgPath}`);

  const etalonFull = readPng(NOLEAK.etalon);
  // 40×40 etalon crop at icon slot
  const etalon40 = crop(etalonFull, iconAbsLeft, iconAbsTop, 40, 40);
  writePng(join(outDir, 'etalon-check-40x40-slot.png'), etalon40);
  const etalon120 = scaleNearest(etalon40, 3);
  writePng(join(outDir, 'etalon-check-40x40-slot-dpr3.png'), etalon120);

  // Also pad crop at region center 56×56 for context strip
  const [cx, cy] = NOLEAK.center;
  const etalonSlot56 = crop(etalonFull, Math.round(cx - 28), Math.round(cy - 28), 56, 56);
  writePng(join(outDir, 'etalon-slot-56-dpr1.png'), etalonSlot56);

  const svg40 = await renderExactSvg(40);
  const svg120 = await renderExactSvg(120);
  writePng(join(outDir, 'exact-svg-vs-etalon-40.png'), svg40);
  writePng(join(outDir, 'exact-svg-vs-etalon-dpr3.png'), svg120);

  // Ensure etalon is opaque on canvas for fair compare
  const etalon40c = compositeOnCanvas(etalon40);
  const etalon120c = compositeOnCanvas(etalon120);

  const m40 = regionMetrics(svg40, etalon40c, 0.05);
  const m120 = regionMetrics(svg120, etalon120c, 0.05);
  writePng(join(outDir, 'exact-svg-vs-etalon-40-diff.png'), m40.diff);
  writePng(join(outDir, 'exact-svg-vs-etalon-dpr3-diff.png'), m120.diff);
  writePng(join(outDir, 'side-by-side-svg-etalon-diff-dpr3.png'), hstrip([svg120, etalon120c, m120.diff]));
  writePng(join(outDir, 'side-by-side-svg-etalon-diff-40.png'), hstrip([svg40, etalon40c, m40.diff]));

  // Chrome 1b gates
  const chromeNoleak = join(
    mobileRoot,
    'src/components/figma/cards/assets/pin/illustration-onboarding-no-leak-card-chrome.png',
  );
  const chromeMon = join(
    mobileRoot,
    'src/components/figma/cards/assets/pin/screen-illustration-monitoring-intro-3392-25832-card-chrome.png',
  );
  const chromeGates = {
    noleak: chromeTopBottomGate(chromeNoleak, [61, 65], [180, 103]),
    monitoring: chromeTopBottomGate(chromeMon, [61, 99], [180, 135]),
  };
  for (const [id, path] of [
    ['noleak', chromeNoleak],
    ['monitoring', chromeMon],
  ]) {
    const ch = readPng(path);
    const [ox, oy] = id === 'noleak' ? [61, 65] : [61, 99];
    const [ccx0, ccy0] = id === 'noleak' ? [180, 103] : [180, 135];
    const cropCh = crop(ch, Math.round(ccx0 - ox - 28), Math.round(ccy0 - oy - 28), 56, 56);
    writePng(join(outDir, `${id}-chrome-slot-56-dpr3.png`), scaleNearest(cropCh, 3));
  }

  const prov = JSON.parse(
    readFileSync(join(mobileRoot, 'src/components/figma/cards/assets/pin/PROVENANCE.json'), 'utf8'),
  );
  const shaOf = (rel) =>
    createHash('sha256').update(readFileSync(join(mobileRoot, rel))).digest('hex');

  const metricsPath = join(outDir, 'metrics.json');
  const prev = existsSync(metricsPath) ? JSON.parse(readFileSync(metricsPath, 'utf8')) : {};

  const report = {
    ...prev,
    cursor1b: {
      iconDilatePx: 6,
      residualLumaThr: 2,
      holeNote: 'alpha+dilate6 etalon-space + residual|Δ|>2 adjacent expand; Jacobi inpaint',
      chromeSha: {
        noleak: shaOf('src/components/figma/cards/assets/pin/illustration-onboarding-no-leak-card-chrome.png'),
        monitoring: shaOf(
          'src/components/figma/cards/assets/pin/screen-illustration-monitoring-intro-3392-25832-card-chrome.png',
        ),
        provenanceMatch: {
          noleak:
            prov.assets.find((a) => a.fileName === 'illustration-onboarding-no-leak-card-chrome.png')
              ?.sha256 ===
            shaOf('src/components/figma/cards/assets/pin/illustration-onboarding-no-leak-card-chrome.png'),
          monitoring:
            prov.assets.find(
              (a) => a.fileName === 'screen-illustration-monitoring-intro-3392-25832-card-chrome.png',
            )?.sha256 ===
            shaOf(
              'src/components/figma/cards/assets/pin/screen-illustration-monitoring-intro-3392-25832-card-chrome.png',
            ),
        },
      },
      chromeTopBottomGhost: chromeGates,
      chromeGhostPass: chromeGates.noleak.pass && chromeGates.monitoring.pass,
    },
    // PRIMARY fidelity metric (owner): exact SVG vs Figma API etalon, NOT vs PNG paint.
    exactSvgVsEtalonApi: {
      sourceSvg:
        'e2e/artifacts/worker-reports/t191-figma-refs/glass-icons/glass-icon-check-circle-3482-28947-cleaned.svg',
      etalonApi: 'e2e/figma-component-baselines/ios/illustration-onboarding-no-leak-api.png',
      slot: {
        iconAbsLeft,
        iconAbsTop,
        iconSize: 40,
        regionCenter: NOLEAK.center,
      },
      crop40: {
        thr0_05_includeAA: {
          mismatched: m40.mismatched,
          total: m40.total,
          ratio: Number(m40.ratio.toFixed(6)),
        },
        meanAbsMaxChannel: Number(m40.meanAbsMaxChannel.toFixed(3)),
        maxAbsMaxChannel: m40.maxAbsMaxChannel,
        coreRatio: Number(m40.core.ratio.toFixed(6)),
        rimRatio: Number(m40.rim.ratio.toFixed(6)),
        files: {
          svg: 'exact-svg-vs-etalon-40.png',
          etalon: 'etalon-check-40x40-slot.png',
          diff: 'exact-svg-vs-etalon-40-diff.png',
          strip: 'side-by-side-svg-etalon-diff-40.png',
        },
      },
      dpr3: {
        thr0_05_includeAA: {
          mismatched: m120.mismatched,
          total: m120.total,
          ratio: Number(m120.ratio.toFixed(6)),
        },
        meanAbsMaxChannel: Number(m120.meanAbsMaxChannel.toFixed(3)),
        maxAbsMaxChannel: m120.maxAbsMaxChannel,
        coreRatio: Number(m120.core.ratio.toFixed(6)),
        rimRatio: Number(m120.rim.ratio.toFixed(6)),
        files: {
          svg: 'exact-svg-vs-etalon-dpr3.png',
          etalon: 'etalon-check-40x40-slot-dpr3.png',
          diff: 'exact-svg-vs-etalon-dpr3-diff.png',
          strip: 'side-by-side-svg-etalon-diff-dpr3.png',
        },
      },
      note:
        'Primary fidelity = exact-SVG Chromium render vs -api.png icon crop. SVG-vs-PNG (~0.27) is secondary (our paint vs our raster), not etalon fidelity.',
    },
  };

  // Keep prior svg-vs-png under secondary key if present
  if (prev.metrics && !report.secondarySvgVsPng) {
    report.secondarySvgVsPng = prev.metrics;
  }

  writeFileSync(metricsPath, JSON.stringify(report, null, 2));
  writeFileSync(join(outDir, 'cursor1b-chrome-ghost-gate.json'), JSON.stringify(chromeGates, null, 2));

  console.log(JSON.stringify({
    chromeGhostPass: report.cursor1b.chromeGhostPass,
    chromeGates,
    exactSvgVsEtalon_40: report.exactSvgVsEtalonApi.crop40,
    exactSvgVsEtalon_dpr3: report.exactSvgVsEtalonApi.dpr3,
  }, null, 2));
  console.log('wrote', metricsPath);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
