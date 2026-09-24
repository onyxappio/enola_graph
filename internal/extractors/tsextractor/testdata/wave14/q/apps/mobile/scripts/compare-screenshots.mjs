import { existsSync, mkdirSync, readdirSync, readFileSync, copyFileSync, writeFileSync } from 'node:fs';
import { basename, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';
import pixelmatch from 'pixelmatch';
import { PNG } from 'pngjs';

import {
  CANONICAL_NATIVE_SCREENSHOT_COUNT,
  collectExpectedScreenshotIdsFromMaestro,
  visualCompletenessFailures,
} from './visualScreenshotCompleteness.mjs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const mobileRoot = resolve(__dirname, '..');
const currentDir = join(mobileRoot, 'e2e/artifacts/current/ios');
const baselineDir = join(mobileRoot, 'e2e/baselines/ios');
const diffDir = join(mobileRoot, 'e2e/artifacts/diff/ios');
const update = process.argv.includes('--update');
const maxDiffRatio = Number(process.env.MOBILE_VISUAL_MAX_DIFF_RATIO ?? '0.01');
const threshold = Number(process.env.MOBILE_VISUAL_PIXEL_THRESHOLD ?? '0.12');

mkdirSync(currentDir, { recursive: true });
mkdirSync(baselineDir, { recursive: true });
mkdirSync(diffDir, { recursive: true });

const currentFiles = readdirSync(currentDir)
  .filter((file) => file.endsWith('.png'))
  .sort();
const baselineFiles = readdirSync(baselineDir)
  .filter((file) => file.endsWith('.png'))
  .sort();
const expectedIds = collectExpectedScreenshotIdsFromMaestro();
const currentIds = currentFiles.map((file) => file.replace(/\.png$/i, ''));
const baselineIds = baselineFiles.map((file) => file.replace(/\.png$/i, ''));

if (currentFiles.length === 0) {
  console.error(`No screenshots found in ${currentDir}. Run pnpm mobile:visual:capture first.`);
  process.exit(1);
}

if (update) {
  for (const file of currentFiles) {
    copyFileSync(join(currentDir, file), join(baselineDir, file));
    console.log(`Updated baseline ${file}`);
  }
  process.exit(0);
}

const failures = [];
const report = [];

// Three-way completeness: Maestro expected ↔ current ↔ baseline.
// Removing a state from both current and baseline must still fail.
failures.push(...visualCompletenessFailures({
  expectedIds,
  currentIds,
  baselineIds,
  canonicalCount: CANONICAL_NATIVE_SCREENSHOT_COUNT,
}));

const expectedSet = new Set(expectedIds.map((id) => `${id}.png`));
const currentSet = new Set(currentFiles);
const baselineSet = new Set(baselineFiles);
const comparableFiles = currentFiles.filter(
  (file) => baselineSet.has(file) && expectedSet.has(file),
);

for (const file of comparableFiles) {
  const currentPath = join(currentDir, file);
  const baselinePath = join(baselineDir, file);
  const diffPath = join(diffDir, file);

  const current = PNG.sync.read(readFileSync(currentPath));
  const baseline = PNG.sync.read(readFileSync(baselinePath));

  if (current.width !== baseline.width || current.height !== baseline.height) {
    failures.push(`${file}: size mismatch current=${current.width}x${current.height}, baseline=${baseline.width}x${baseline.height}`);
    continue;
  }

  const diff = new PNG({ width: current.width, height: current.height });
  const diffPixels = pixelmatch(current.data, baseline.data, diff.data, current.width, current.height, {
    threshold,
    includeAA: false,
  });
  const diffRatio = diffPixels / (current.width * current.height);

  writeFileSync(diffPath, PNG.sync.write(diff));
  report.push({ file, diffPixels, diffRatio });

  if (diffRatio > maxDiffRatio) {
    failures.push(`${file}: ${(diffRatio * 100).toFixed(3)}% changed (${diffPixels} px)`);
  }
}

for (const entry of report) {
  console.log(`${basename(entry.file)}: ${(entry.diffRatio * 100).toFixed(3)}% (${entry.diffPixels} px)`);
}

console.log(
  `\nVisual completeness: expected=${expectedIds.length} current=${currentFiles.length} baseline=${baselineFiles.length} comparable=${comparableFiles.length}`,
);

if (failures.length > 0) {
  console.error('\nMobile visual regression failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  console.error('\nIf the new screenshots are intentional, run pnpm mobile:visual:update.');
  process.exit(1);
}

if (
  currentFiles.length !== baselineFiles.length
  || currentFiles.length !== expectedIds.length
  || expectedIds.length !== CANONICAL_NATIVE_SCREENSHOT_COUNT
) {
  console.error(
    `\nMobile visual completeness failed: expected (${expectedIds.length}) current (${currentFiles.length}) baseline (${baselineFiles.length}) canonical (${CANONICAL_NATIVE_SCREENSHOT_COUNT}).`,
  );
  process.exit(1);
}

console.log(`\nMobile visual regression passed (${CANONICAL_NATIVE_SCREENSHOT_COUNT}/${CANONICAL_NATIVE_SCREENSHOT_COUNT}).`);
