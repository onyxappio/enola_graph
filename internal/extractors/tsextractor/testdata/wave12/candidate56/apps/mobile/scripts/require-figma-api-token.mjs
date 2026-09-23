import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const FIGMA_API_TOKEN_ENV_NAMES = [
  'FIGMA_SCREEN_API_TOKENS',
  'FIGMA_API_TOKENS',
  'FIGMA_TOKENS',
  'FIGMA_TOKEN',
  'FIGMA_API_TOKEN',
];
const FIGMA_API_NUMBERED_TOKEN_ENV_PATTERN = /^(FIGMA_SCREEN_API_TOKEN|FIGMA_API_TOKEN|FIGMA_TOKEN)_?(\d+)$/;

function loadLocalEnvToken() {
  const envPath = join(process.cwd(), '.env.local');
  if (existsSync(envPath)) {
    for (const line of readFileSync(envPath, 'utf8').split('\n')) {
      const match = line.match(/^\s*((?:FIGMA_SCREEN_API_TOKENS|FIGMA_API_TOKENS|FIGMA_TOKENS|FIGMA_TOKEN|FIGMA_API_TOKEN)|(?:FIGMA_SCREEN_API_TOKEN|FIGMA_API_TOKEN|FIGMA_TOKEN)_?\d+)=(.*)\s*$/);
      if (!match || process.env[match[1]]) continue;
      process.env[match[1]] = stripEnvQuotes(match[2]);
    }
  }

  if (!process.env.FIGMA_TOKEN && !process.env.FIGMA_API_TOKEN) {
    const firstPooledToken = readFigmaApiTokens()[0];
    if (firstPooledToken) process.env.FIGMA_API_TOKEN = firstPooledToken;
  }
}

loadLocalEnvToken();

const hasFigmaToken = readFigmaApiTokens().length > 0;

if (!hasFigmaToken) {
  console.error('FIGMA_TOKEN, FIGMA_API_TOKEN, FIGMA_API_TOKENS, FIGMA_SCREEN_API_TOKENS, or FIGMA_TOKENS is required for visual:figma-api:test. Set it in the shell or apps/mobile/.env.local.');
  process.exit(1);
}

function readFigmaApiTokens() {
  const tokens = [];
  const seen = new Set();

  for (const envName of [...FIGMA_API_TOKEN_ENV_NAMES, ...readNumberedFigmaApiTokenEnvNames()]) {
    const raw = process.env[envName];
    if (!raw) continue;
    for (const token of splitTokenList(raw)) {
      if (seen.has(token)) continue;
      tokens.push(token);
      seen.add(token);
    }
  }

  return tokens;
}

function readNumberedFigmaApiTokenEnvNames() {
  const prefixRank = {
    FIGMA_SCREEN_API_TOKEN: 0,
    FIGMA_API_TOKEN: 1,
    FIGMA_TOKEN: 2,
  };

  return Object.keys(process.env)
    .filter((envName) => FIGMA_API_NUMBERED_TOKEN_ENV_PATTERN.test(envName))
    .sort((left, right) => {
      const leftMatch = left.match(FIGMA_API_NUMBERED_TOKEN_ENV_PATTERN);
      const rightMatch = right.match(FIGMA_API_NUMBERED_TOKEN_ENV_PATTERN);
      if (!leftMatch || !rightMatch) return left.localeCompare(right);
      return prefixRank[leftMatch[1]] - prefixRank[rightMatch[1]]
        || Number(leftMatch[2]) - Number(rightMatch[2])
        || left.localeCompare(right);
    });
}

function splitTokenList(raw) {
  return stripEnvQuotes(raw)
    .split(/[,\s]+/)
    .map((token) => stripEnvQuotes(token).trim())
    .filter(Boolean);
}

function stripEnvQuotes(value) {
  return value.trim().replace(/^['"]|['"]$/g, '');
}
