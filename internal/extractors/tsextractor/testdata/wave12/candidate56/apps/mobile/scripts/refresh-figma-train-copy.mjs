#!/usr/bin/env node
// Manual refresh ritual for `packages/contracts/src/train/fixtures/figmaTrainCopy.baseline.json`
// (T-370 doc↔Figma copy parity guardrail). Rewrites the committed fixture
// from the live Figma file so the next release/structure wave can re-run
// the parity invariants against current Figma copy.
//
// CI never calls this — it hits the Figma REST API and requires a token.
// Run it by hand before releases or Figma structure waves:
//   FIGMA_TOKEN=... node apps/mobile/scripts/refresh-figma-train-copy.mjs
//
// Output shape per frame: { frameId, frameName, texts: [{ nodeId, text, bold, path, inst }] }
// bold = array of [start, end) index ranges whose fontWeight >= 600 relative to the string.
import https from 'node:https';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const defaultOutPath = path.resolve(here, '../../../packages/contracts/src/train/fixtures/figmaTrainCopy.baseline.json');

const isOffline = process.env.FIGMA_OFFLINE === '1' || process.env.GATES_OFFLINE === '1';
const token = process.env.FIGMA_TOKEN || process.env.FIGMA_API_TOKEN;

if (isOffline) {
  console.log('[refresh-figma-train-copy] FIGMA_OFFLINE/GATES_OFFLINE=1 — skipping live Figma refresh (fixture left as-is).');
  process.exit(0);
}
if (!token) {
  console.log('[refresh-figma-train-copy] No FIGMA_TOKEN/FIGMA_API_TOKEN set — skipping live Figma refresh (fixture left as-is).');
  console.log('[refresh-figma-train-copy] Set FIGMA_TOKEN (or FIGMA_API_TOKEN) to a Figma personal access token to refresh.');
  process.exit(0);
}

const fileKey = 'z8ueGL08v3ahQ11hOA4501';
const sections = [
  ['C1/L1', '5071:5283'], ['C1/L2', '5001:5844'], ['C1/L3', '5001:5908'], ['C1/L4', '5001:5972'], ['C1/Quiz', '5001:5791'],
  ['C2/L1', '5081:6766'], ['C2/L2', '5082:7385'], ['C2/L3', '5083:8010'], ['C2/L4', '5084:8626'], ['C2/Quiz', '5093:9239'],
];

function get(reqPath) {
  return new Promise((resolve, reject) => {
    https.get({ host: 'api.figma.com', path: reqPath, headers: { 'X-Figma-Token': token } }, (res) => {
      let d = '';
      res.on('data', (c) => (d += c));
      res.on('end', () => { try { resolve(JSON.parse(d)); } catch { reject(new Error('bad json: ' + d.slice(0, 200))); } });
    }).on('error', reject);
  });
}

function boldRanges(n) {
  const base = n.style || {};
  const chars = n.characters || '';
  const overrides = n.characterStyleOverrides || [];
  const table = n.styleOverrideTable || {};
  const ranges = [];
  let cur = null;
  for (let i = 0; i < chars.length; i++) {
    const o = overrides[i] || 0;
    const st = o === 0 ? base : { ...base, ...(table[o] || {}) };
    const bold = (st.fontWeight || 400) >= 600;
    if (bold) {
      if (cur && cur[1] === i) cur[1] = i + 1;
      else { cur = [i, i + 1]; ranges.push(cur); }
    }
  }
  return ranges;
}

function walk(n, path, out, insideInstance) {
  if (n.visible === false) return;
  const here = path ? path + '/' + n.name : n.name;
  if (n.type === 'TEXT') {
    out.push({ nodeId: n.id, text: n.characters || '', bold: boldRanges(n), path: here, inst: insideInstance });
    return;
  }
  const nowInside = insideInstance || n.type === 'INSTANCE';
  (n.children || []).forEach((c) => walk(c, here, out, nowInside));
}

async function main() {
  const frames = [];
  for (const [label, id] of sections) {
    const j = await get(`/v1/files/${fileKey}/nodes?ids=${encodeURIComponent(id)}`);
    const node = j.nodes?.[id]?.document;
    if (!node) throw new Error(`section ${label} ${id} missing: ${JSON.stringify(j).slice(0, 200)}`);
    for (const frame of node.children || []) {
      if (frame.visible === false) continue;
      const texts = [];
      walk(frame, '', texts, false);
      frames.push({
        section: label,
        frameId: frame.id,
        frameName: frame.name,
        texts: texts.filter((t) => !/_StatusBar-time$/.test(t.path)),
      });
    }
  }

  const outPath = process.argv[2] || defaultOutPath;
  fs.writeFileSync(outPath, JSON.stringify({
    generatedBy: 'T-370 refresh (manual, apps/mobile/scripts/refresh-figma-train-copy.mjs)',
    fileKey,
    frames,
  }, null, 2) + '\n');
  console.log('written', outPath, frames.length, 'frames,', frames.reduce((a, f) => a + f.texts.length, 0), 'text nodes');
}

main().catch((err) => {
  console.error('[refresh-figma-train-copy] failed:', err);
  process.exitCode = 1;
});
