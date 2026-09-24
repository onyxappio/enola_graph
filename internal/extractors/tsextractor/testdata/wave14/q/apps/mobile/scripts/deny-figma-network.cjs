'use strict';

const http = require('node:http');
const https = require('node:https');

const INSTALL_KEY = Symbol.for('onyx.mobile.denyFigmaNetwork.installed');
const FIGMA_API_HOST = 'api.figma.com';

if (!globalThis[INSTALL_KEY]) {
  globalThis[INSTALL_KEY] = true;

  const originalFetch = globalThis.fetch;
  if (typeof originalFetch === 'function') {
    globalThis.fetch = function denyFigmaFetch(input, init) {
      assertFigmaApiIsNotRequested(input, init);
      return originalFetch.call(this, input, init);
    };
  }

  patchRequest(http, 'request');
  patchRequest(http, 'get');
  patchRequest(https, 'request');
  patchRequest(https, 'get');
}

function patchRequest(module, method) {
  const original = module[method];
  module[method] = function denyFigmaRequest(input, options, ...rest) {
    assertFigmaApiIsNotRequested(input, options);
    return original.call(this, input, options, ...rest);
  };
}

function assertFigmaApiIsNotRequested(...inputs) {
  for (const input of inputs) {
    const hostname = hostnameFrom(input);
    if (hostname === FIGMA_API_HOST) {
      const error = new Error(
        `[figma-network-deny] blocked network request to ${FIGMA_API_HOST}; use committed pinned artifacts`,
      );
      error.code = 'FIGMA_NETWORK_DENIED';
      throw error;
    }
  }
}

function hostnameFrom(input) {
  if (!input) return null;
  if (input instanceof URL) return normalizeHostname(input.hostname);
  if (typeof input === 'string') {
    try {
      return normalizeHostname(new URL(input).hostname);
    } catch {
      return null;
    }
  }
  if (typeof input !== 'object') return null;

  if (typeof input.url === 'string') {
    const hostname = hostnameFrom(input.url);
    if (hostname) return hostname;
  }
  if (typeof input.hostname === 'string') return normalizeHostname(input.hostname);
  if (typeof input.host === 'string') return normalizeHostname(input.host);
  return null;
}

function normalizeHostname(value) {
  return value.toLowerCase().replace(/^\[/, '').replace(/\]$/, '').split(':')[0];
}
