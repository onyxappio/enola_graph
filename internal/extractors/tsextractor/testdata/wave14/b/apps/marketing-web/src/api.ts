import type {
  PaymentCheckoutSessionCreateRequest,
  PaymentCheckoutSessionCreateResponse,
  PaymentOfferQueryRequest,
  PaymentOfferQueryResponse,
  PaymentOrderStatusRequest,
  PaymentOrderStatusResponse,
  PaymentBillingClaimRequest,
  PaymentBillingClaimResponse,
  PaymentBillingClaimStatusRequest,
  PaymentBillingClaimStatusResponse,
} from '@onyx/contracts';

export type SubjectType = 'email' | 'password' | 'phone';

export type ScanAttribution = {
  fSessionId?: string;
  surface?: string;
  [key: string]: string | undefined;
};

/**
 * Optional audit join context for server-emitted scan_requested events.
 * When landings start sending `tracking.testRunId`, it MUST match the opaque
 * non-PII grammar ^[A-Za-z0-9_-]{8,64}$ (UUIDs / probe-style ids; never emails,
 * dotted hostnames, or free-text). Enforced by packages/contracts marketingScanTestRunIdSchema.
 */
export type ScanTrackingContext = {
  testRunId?: string;
};

export type ScanRequest = {
  subjectType: SubjectType;
  subject: string;
  source: 'marketing-funnel';
  attribution?: ScanAttribution;
  /** Additive; see ScanTrackingContext grammar for testRunId. */
  tracking?: ScanTrackingContext;
};

export type ScanCreateResponse = {
  scanRunId: string;
  state: 'queued';
  subjectType: SubjectType;
};

export type ProviderCoverage = {
  total: number;
  completed: string[];
  failed: string[];
  skipped: string[];
  pending: string[];
  coverageStatus: 'pending' | 'complete' | 'partial' | 'failed';
};

export type ScanStatusResponse = {
  scanRunId: string;
  subjectType: SubjectType;
  source: string;
  state: 'requested' | 'queued' | 'running' | 'completed' | 'completed_partial' | 'failed' | 'expired';
  providerCoverage: ProviderCoverage | null;
  report: {
    exposureSummary: {
      totalIncidents: number;
      totalExposedFields: number;
      hasPasswordLeak: boolean;
      hasAddressLeak: boolean;
      hasPhoneLeak: boolean;
    };
  } | null;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
};

export type PaymentFormUxEvent = 'success' | 'fail' | 'error' | 'verify';

export type ClaimTokenLocation = {
  hash: string;
  search?: string;
};

export type ClaimLinkState = {
  claimToken: string;
  email?: string;
  emailLocked: boolean;
};

export type BillingClaimClientResult =
  | {
    status: 'success';
    httpStatus: number;
    body: Extract<PaymentBillingClaimResponse, { status: 'claimed' }>;
  }
  | {
    status: 'claimed_existing_account';
    httpStatus: number;
    body: Extract<PaymentBillingClaimResponse, { status: 'claimed_existing_account' }>;
  }
  | {
    status: 'invalid_or_expired';
    httpStatus: number;
    reason?: string;
  }
  | {
    status: 'existing_account';
    httpStatus: number;
    reason?: string;
  }
  | {
    status: 'already_claimed';
    httpStatus: number;
    reason?: string;
  }
  | {
    status: 'queued';
    httpStatus: number;
    reason?: string;
    body: unknown;
  }
  | {
    status: 'retryable_error';
    httpStatus: number;
    reason?: string;
  };

export const onyxAppStoreUrl = 'https://apps.apple.com/app/onyx/id6785895194';

export function resolveMarketingApiBaseUrl(env: ImportMetaEnv = import.meta.env): string {
  return (env.VITE_MARKETING_API_BASE_URL || 'https://marketing-api-sandbox.onyxapp.io').replace(/\/$/, '');
}

export function resolvePaymentApiBaseUrl(env: ImportMetaEnv = import.meta.env, hostname = readRuntimeHostname()): string {
  const configuredBaseUrl = normalizeConfiguredBaseUrl(env.VITE_PAYMENT_API_BASE_URL);
  return configuredBaseUrl ?? resolveRuntimeApiBaseUrl('payment-api', hostname);
}

export function resolveProductApiBaseUrl(env: ImportMetaEnv = import.meta.env, hostname = readRuntimeHostname()): string {
  const configuredBaseUrl = normalizeConfiguredBaseUrl(env.VITE_PRODUCT_API_BASE_URL);
  return configuredBaseUrl ?? resolveRuntimeApiBaseUrl('product-api', hostname);
}

function normalizeConfiguredBaseUrl(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed.replace(/\/+$/, '') : undefined;
}

function readRuntimeHostname(): string | undefined {
  if (typeof window === 'undefined') return undefined;
  return window.location.hostname;
}

function resolveRuntimeApiBaseUrl(serviceName: 'payment-api' | 'product-api', hostname: string | undefined): string {
  const normalizedHostname = hostname?.trim().toLowerCase() ?? '';

  if (
    !normalizedHostname
    || normalizedHostname === 'localhost'
    || normalizedHostname === '127.0.0.1'
    || normalizedHostname === '0.0.0.0'
    || normalizedHostname === '::1'
    || normalizedHostname.endsWith('.localhost')
  ) {
    return `https://${serviceName}-test.onyxapp.io`;
  }

  if (
    normalizedHostname === 'claim.onyxapp.io'
    || normalizedHostname === 'onyxapp.io'
    || normalizedHostname === 'www.onyxapp.io'
    || normalizedHostname === 'marketing-web.onyxapp.io'
    || normalizedHostname === 'marketing-web-prod.onyxapp.io'
  ) {
    return `https://${serviceName}.onyxapp.io`;
  }

  const marketingWebEnvironment = normalizedHostname.match(/^marketing-web-(.+)\.onyxapp\.io$/)?.[1];
  if (marketingWebEnvironment) {
    return `https://${serviceName}-${marketingWebEnvironment}.onyxapp.io`;
  }

  const claimEnvironment = normalizedHostname.match(/^claim-(.+)\.onyxapp\.io$/)?.[1];
  if (claimEnvironment) {
    return `https://${serviceName}-${claimEnvironment}.onyxapp.io`;
  }

  return `https://${serviceName}-test.onyxapp.io`;
}

export function readClaimTokenFromLocation(location: ClaimTokenLocation): string | undefined {
  return readClaimLinkFromLocation(location)?.claimToken;
}

export function readClaimTokenFromHash(hash: string): string | undefined {
  return readClaimLinkFromHash(hash)?.claimToken;
}

export function readClaimLinkFromLocation(location: ClaimTokenLocation): ClaimLinkState | undefined {
  return readClaimLinkFromHash(location.hash);
}

export function readClaimLinkFromHash(hash: string): ClaimLinkState | undefined {
  const withoutHash = hash.startsWith('#') ? hash.slice(1) : hash;
  const withoutRouteSlash = withoutHash.startsWith('/') ? withoutHash.slice(1) : withoutHash;
  const trimmed = withoutRouteSlash.trim();
  if (!trimmed) return undefined;

  const tokenParamLink = readTokenParamClaimLink(trimmed);
  if (tokenParamLink) return tokenParamLink;

  try {
    const decoded = decodeURIComponent(trimmed).trim();
    return decoded ? { claimToken: decoded, emailLocked: false } : undefined;
  } catch {
    return { claimToken: trimmed, emailLocked: false };
  }
}

function readTokenParamClaimLink(fragment: string): ClaimLinkState | undefined {
  const params = new URLSearchParams(fragment);
  const claimToken = params.get('token')?.trim();
  if (!claimToken) return undefined;

  const email = params.get('email')?.trim() || undefined;
  return {
    claimToken,
    email,
    emailLocked: Boolean(email),
  };
}

/**
 * POST /v1/scans payload builder entrypoint.
 * If attaching tracking.testRunId for audit joins, keep it opaque non-PII:
 * ^[A-Za-z0-9_-]{8,64}$ — same grammar as marketingScanTestRunIdSchema (contracts).
 */
export async function requestScan(baseUrl: string, request: ScanRequest): Promise<ScanCreateResponse> {
  const response = await fetch(`${baseUrl}/v1/scans`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(`scan_request_failed:${response.status}`);
  }

  return response.json() as Promise<ScanCreateResponse>;
}

export async function getScanStatus(baseUrl: string, scanRunId: string): Promise<ScanStatusResponse> {
  const response = await fetch(`${baseUrl}/v1/scans/${encodeURIComponent(scanRunId)}`);

  if (!response.ok) {
    throw new Error(`scan_status_failed:${response.status}`);
  }

  return response.json() as Promise<ScanStatusResponse>;
}

export async function queryPaymentOffers(baseUrl: string, request: PaymentOfferQueryRequest): Promise<PaymentOfferQueryResponse> {
  return postPaymentJson(baseUrl, '/v1/payment/f/offers/query', request, 'payment_offer_query_failed');
}

export async function createPaymentCheckoutSession(
  baseUrl: string,
  idempotencyKey: string,
  request: PaymentCheckoutSessionCreateRequest,
): Promise<PaymentCheckoutSessionCreateResponse> {
  return postPaymentJson(baseUrl, '/v1/payment/f/checkout-sessions/create', request, 'payment_checkout_create_failed', {
    'Idempotency-Key': idempotencyKey,
  });
}

export async function getPaymentOrderStatus(baseUrl: string, request: PaymentOrderStatusRequest): Promise<PaymentOrderStatusResponse> {
  return postPaymentJson(baseUrl, '/v1/payment/f/orders/status', request, 'payment_order_status_failed');
}

export async function preflightBillingClaim(
  baseUrl: string,
  request: PaymentBillingClaimStatusRequest,
): Promise<PaymentBillingClaimStatusResponse> {
  const response = await fetch(`${baseUrl}/v1/billing/claim/status`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ claim_token: request.claim_token }),
  });
  const body = await readJsonBody(response);
  const status = readStringProperty(body, 'status');

  if (response.ok && isBillingClaimStatus(status)) {
    return { status };
  }

  throw new Error(`billing_claim_status_failed:${response.status}`);
}

export async function claimBillingSubscription(
  baseUrl: string,
  request: PaymentBillingClaimRequest,
): Promise<BillingClaimClientResult> {
  const response = await fetch(`${baseUrl}/v1/billing/claim`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      ...request,
      email: request.email.trim(),
    }),
  });
  const body = await readJsonBody(response);
  const reason = readStringProperty(body, 'reason') ?? readStringProperty(body, 'error');

  if (response.status === 404 || reason === 'invalid_claim_token' || reason === 'claim_expired') {
    return { status: 'invalid_or_expired', httpStatus: response.status, reason };
  }

  if (reason === 'account_already_exists') {
    return { status: 'existing_account', httpStatus: response.status, reason };
  }

  if (reason === 'claim_already_used') {
    return { status: 'already_claimed', httpStatus: response.status, reason };
  }

  if (response.status === 202) {
    return { status: 'queued', httpStatus: response.status, reason, body };
  }

  if (response.ok && isClaimedExistingBillingResponse(body)) {
    return { status: 'claimed_existing_account', httpStatus: response.status, body };
  }

  if (response.ok && isClaimedBillingResponse(body)) {
    return { status: 'success', httpStatus: response.status, body };
  }

  if (response.ok) {
    return { status: 'queued', httpStatus: response.status, reason, body };
  }

  return { status: 'retryable_error', httpStatus: response.status, reason };
}

export function describeScanResult(status: ScanStatusResponse): string {
  if (status.state === 'requested' || status.state === 'queued' || status.state === 'running') {
    return 'Scan is running through the provider workers.';
  }

  if (status.state === 'failed') {
    return 'The scan failed safely. Try again in a moment.';
  }

  const summary = status.report?.exposureSummary;
  if (!summary) {
    return 'Scan completed. No report is available yet.';
  }

  const findings =
    summary.totalIncidents === 0
      ? 'No exposures were found for this subject.'
      : `${summary.totalIncidents.toLocaleString()} exposure${summary.totalIncidents === 1 ? '' : 's'} found${
          summary.hasPasswordLeak ? ', including a leaked password' : ''
        }.`;

  if (status.state === 'completed_partial') {
    return `${findings} Some providers were unavailable — results may be incomplete.`;
  }

  return findings;
}

export function describePaymentFormUxEvent(event: PaymentFormUxEvent): string {
  if (event === 'success') {
    return 'Payment form completed. Onyx is waiting for backend confirmation from Solidgate before account access is prepared.';
  }
  if (event === 'verify') {
    return 'Additional verification is in progress. Keep this page open while Onyx waits for backend confirmation.';
  }
  if (event === 'fail') {
    return 'Payment was not completed. You can retry with the same offer or choose another payment method.';
  }
  return 'The payment form reported an error. No access was granted; please retry or contact support.';
}

export function describePaymentOrderStatus(status: PaymentOrderStatusResponse): string {
  if (status.access.status === 'active') {
    return 'Backend confirmation is complete and access is active.';
  }

  if (status.payment.status === 'paid_reserved' && status.access.status === 'awaiting_account_claim') {
    return 'Payment is confirmed. Check your email for the secure account claim link.';
  }

  if (status.payment.status === 'failed') {
    return 'Payment failed. No access was granted; please retry the payment.';
  }

  if (status.next_action === 'contact_support') {
    return 'Payment needs manual review. No access was granted automatically.';
  }

  return 'Onyx is waiting for backend payment confirmation. Frontend payment events are UX-only and do not grant access.';
}

async function postPaymentJson<T>(
  baseUrl: string,
  path: string,
  body: unknown,
  errorPrefix: string,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw new Error(`${errorPrefix}:${response.status}`);
  }

  return response.json() as Promise<T>;
}
async function readJsonBody(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return undefined;
  }
}

function readStringProperty(value: unknown, property: string): string | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const propertyValue = (value as Record<string, unknown>)[property];
  return typeof propertyValue === 'string' && propertyValue.trim() ? propertyValue : undefined;
}

function isBillingClaimStatus(value: string | undefined): value is PaymentBillingClaimStatusResponse['status'] {
  return value === 'claimable' || value === 'already_claimed' || value === 'invalid_or_expired';
}

function isClaimedBillingResponse(value: unknown): value is Extract<PaymentBillingClaimResponse, { status: 'claimed' }> {
  return Boolean(value && typeof value === 'object' && (value as Record<string, unknown>).status === 'claimed');
}

function isClaimedExistingBillingResponse(value: unknown): value is Extract<PaymentBillingClaimResponse, { status: 'claimed_existing_account' }> {
  return Boolean(value && typeof value === 'object' && (value as Record<string, unknown>).status === 'claimed_existing_account');
}
