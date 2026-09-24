import type { PaymentOfferQueryResponse, PaymentOrderStatusResponse } from '@onyx/contracts';
import {
  claimBillingSubscription,
  createPaymentCheckoutSession,
  describePaymentFormUxEvent,
  describePaymentOrderStatus,
  describeScanResult,
  getPaymentOrderStatus,
  getScanStatus,
  onyxAppStoreUrl,
  preflightBillingClaim,
  queryPaymentOffers,
  readClaimLinkFromLocation,
  requestScan,
  resolveMarketingApiBaseUrl,
  resolvePaymentApiBaseUrl,
  resolveProductApiBaseUrl,
  type ClaimLinkState,
  type ScanStatusResponse,
  type SubjectType,
} from './api';
import { mountDemoEnvironmentBanner } from './demoEnvironment';
import { mountSolidgatePaymentForm } from './paymentForm';
import './styles.css';

const marketingApiBaseUrl = resolveMarketingApiBaseUrl();
const paymentApiBaseUrl = resolvePaymentApiBaseUrl();
const productApiBaseUrl = resolveProductApiBaseUrl();

const app = document.querySelector<HTMLDivElement>('#app');

if (!app) {
  throw new Error('app_mount_missing');
}

const appRoot = app;
mountDemoEnvironmentBanner(appRoot, window.location.hostname);

if (isClaimPath(window.location.pathname)) {
  document.body.classList.add('claim-page');
  mountClaimPage();
} else {
document.body.classList.remove('claim-page');
appRoot.innerHTML = `
  <main class="shell">
    <section class="hero" aria-labelledby="hero-title">
      <div class="eyebrow"><span></span> Live breach intelligence</div>
      <div class="hero-grid">
        <div class="hero-copy">
          <p class="kicker">Onyx Preview Scan</p>
          <h1 id="hero-title">See if your digital trail is already exposed.</h1>
          <p class="lede">Run a privacy-safe preview check against the live Onyx backend. The result returns a scan ID, moves through Cloud SQL, Pub/Sub, workers, HIBP, ClickHouse, and ClickStack.</p>
          <div class="proof-row" aria-label="Verified backend path">
            <span>Cloudflare edge</span>
            <span>Cloud SQL</span>
            <span>HIBP worker</span>
            <span>ClickHouse analytics</span>
          </div>
        </div>
        <form class="scan-card" id="scan-form">
          <div class="card-topline">
            <span>Preview check</span>
            <code>POST /v1/scans</code>
          </div>
          <label>
            What should Onyx check?
            <select name="subjectType" id="subject-type">
              <option value="email">Email address</option>
              <option value="password">Password</option>
              <option value="phone">Phone number</option>
            </select>
          </label>
          <label>
            Value to scan
            <input name="subject" id="subject" autocomplete="off" placeholder="name@example.com" required />
          </label>
          <p class="privacy-note">We only show sanitized provider summaries. The raw value is never sent to telemetry or analytics.</p>
          <button type="submit">Run breach preview</button>
          <div class="status-panel" id="status-panel" aria-live="polite">
            <span class="status-dot idle"></span>
            <div>
              <strong>Ready</strong>
              <p>Submit a value to create a durable scan request.</p>
            </div>
          </div>
        </form>
      </div>
    </section>
    <section class="pipeline" aria-label="Live scan pipeline">
      <article><b>01</b><span>Validate</span><p>Shared Zod contracts keep request shape consistent.</p></article>
      <article><b>02</b><span>Persist</span><p>Cloud SQL keeps scan state queryable by scan ID.</p></article>
      <article><b>03</b><span>Process</span><p>Pub/Sub pushes work to the HIBP worker service.</p></article>
      <article><b>04</b><span>Prove</span><p>ClickHouse and ClickStack receive PII-safe events.</p></article>
    </section>
    <section class="checkout" aria-labelledby="checkout-title">
      <div class="checkout-copy">
        <p class="kicker">Onyx Family</p>
        <h2 id="checkout-title">Subscribe with backend-confirmed access.</h2>
        <p>Provider checkout runs in the browser, but Onyx only prepares account claim after verified webhook confirmation.</p>
      </div>
      <form class="checkout-card" id="checkout-form">
        <div class="card-topline">
          <span>Secure checkout</span>
          <code>POST /v1/payment/f</code>
        </div>
        <div class="offer-panel" id="payment-offer" aria-live="polite">
          <strong>Loading offer…</strong>
          <p>Querying the Payment API for the current backend-approved offer.</p>
        </div>
        <label>
          Account email
          <input name="email" id="checkout-email" type="email" autocomplete="email" placeholder="name@example.com" required />
        </label>
        <button type="submit">Create secure checkout</button>
        <div class="status-panel" id="payment-status-panel" aria-live="polite">
          <span class="status-dot idle"></span>
          <div>
            <strong>Waiting for offer</strong>
            <p>Checkout cannot start until the backend returns an offer selection token.</p>
          </div>
        </div>
        <div class="payment-form-shell" id="solid-payment-form-container" aria-label="Payment form"></div>
      </form>
    </section>
  </main>
`;
}

function isClaimPath(pathname: string) {
  const normalized = pathname.replace(/\/+$/, '') || '/';
  return normalized === '/claim';
}

function mountClaimPage() {
  const claimLink = readClaimLinkFromLocation(window.location);
  if (claimLink || window.location.hash) {
    window.history.replaceState(null, '', window.location.pathname);
  }
  appRoot.className = 'claim-app';

  if (!claimLink) {
    renderInvalidClaimTerminal();
    return;
  }

  renderClaimPreflightStatus();
  void preflightAndRenderClaimPage(claimLink);
}

async function preflightAndRenderClaimPage(claimLink: ClaimLinkState) {
  try {
    const status = await preflightBillingClaim(productApiBaseUrl, { claim_token: claimLink.claimToken });

    if (status.status === 'already_claimed') {
      renderAlreadyClaimedTerminal();
      return;
    }

    if (status.status === 'invalid_or_expired') {
      renderInvalidClaimTerminal();
      return;
    }
  } catch {
    renderClaimForm(claimLink);
    return;
  }

  renderClaimForm(claimLink);
}

function renderClaimPreflightStatus() {
  appRoot.innerHTML = `
    <main class="claim-shell">
      <header class="claim-header" aria-label="Onyx">
        <a class="claim-wordmark" href="/" aria-label="Onyx home">ONYX</a>
      </header>
      <section class="claim-card claim-result" aria-live="polite">
        <span class="status-dot running"></span>
        <p class="kicker">Onyx account claim</p>
        <h1>Checking claim link</h1>
        <p class="claim-copy">Confirming the secure claim token before account setup.</p>
      </section>
    </main>
  `;
}

function renderClaimForm(claimLink: ClaimLinkState) {
  appRoot.innerHTML = `
    <main class="claim-shell">
      <header class="claim-header" aria-label="Onyx">
        <a class="claim-wordmark" href="/" aria-label="Onyx home">ONYX</a>
      </header>
      <section class="claim-card" aria-labelledby="claim-form-title">
        <div class="claim-confirmed-badge">
          <span class="status-dot done"></span>
          <span>Payment confirmed</span>
        </div>
        <p class="kicker">Onyx account claim</p>
        <h1 id="claim-form-title">Create your account</h1>
        <p class="claim-copy" id="claim-form-copy">${
          claimLink.emailLocked
            ? 'Your paid subscription is ready. Set a password for the checkout email below to activate Onyx in one step.'
            : 'Your paid subscription is ready. Enter the checkout email and set a password to activate Onyx in one step.'
        }</p>
        <form class="claim-form" id="claim-form">
          <label>
            Email
            <input name="email" id="claim-email" type="email" autocomplete="email" placeholder="name@example.com" required />
            <span class="claim-field-note" id="claim-email-note">${
              claimLink.emailLocked
                ? 'Locked to the email that received this claim link.'
                : 'Use the email address from checkout.'
            }</span>
          </label>
          <label>
            Password
            <input name="password" id="claim-password" type="password" autocomplete="new-password" minlength="8" required />
          </label>
          <label id="claim-confirm-label">
            Confirm password
            <input name="confirmPassword" id="claim-confirm-password" type="password" autocomplete="new-password" minlength="8" required />
          </label>
          <button type="submit" id="claim-submit">Activate subscription</button>
          <div class="status-panel" id="claim-status-panel" aria-live="polite">
            <span class="status-dot idle"></span>
            <div>
              <strong>Ready</strong>
              <p>Your claim token and receipt email were removed from the address bar and are kept only for this activation step.</p>
            </div>
          </div>
        </form>
      </section>
    </main>
  `;

  const form = document.querySelector<HTMLFormElement>('#claim-form');
  const emailInput = document.querySelector<HTMLInputElement>('#claim-email');
  const passwordInput = document.querySelector<HTMLInputElement>('#claim-password');
  const confirmPasswordInput = document.querySelector<HTMLInputElement>('#claim-confirm-password');
  const submitButton = document.querySelector<HTMLButtonElement>('#claim-submit');
  const claimStatusPanel = document.querySelector<HTMLDivElement>('#claim-status-panel');

  if (claimLink.email) {
    emailInput?.setAttribute('value', claimLink.email);
    if (emailInput) {
      emailInput.value = claimLink.email;
      emailInput.readOnly = true;
    }
  }

  function setClaimDisabled(disabled: boolean) {
    emailInput?.toggleAttribute('disabled', disabled);
    passwordInput?.toggleAttribute('disabled', disabled);
    confirmPasswordInput?.toggleAttribute('disabled', disabled);
    submitButton?.toggleAttribute('disabled', disabled);
  }

  function renderClaimStatus(kind: 'idle' | 'running' | 'done' | 'error', title: string, body: string) {
    if (!claimStatusPanel) return;
    claimStatusPanel.innerHTML = `
      <span class="status-dot ${kind}"></span>
      <div>
        <strong></strong>
        <p></p>
      </div>
    `;
    const titleElement = claimStatusPanel.querySelector('strong');
    const bodyElement = claimStatusPanel.querySelector('p');
    if (titleElement) titleElement.textContent = title;
    if (bodyElement) bodyElement.textContent = body;
  }

  form?.addEventListener('submit', async (event) => {
    event.preventDefault();

    const email = claimLink.email ?? emailInput?.value.trim() ?? '';
    const password = passwordInput?.value ?? '';
    const confirmPassword = confirmPasswordInput?.value ?? '';

    if (!email || !password) {
      renderClaimStatus('error', 'Missing account details', 'Enter the checkout email and account password.');
      return;
    }

    if (password !== confirmPassword) {
      renderClaimStatus('error', 'Passwords do not match', 'Confirm the password before creating the account.');
      return;
    }

    try {
      setClaimDisabled(true);
      renderClaimStatus(
        'running',
        'Activating subscription',
        'Creating the account and binding the paid order through the Product API...',
      );

      const claim = await claimBillingSubscription(productApiBaseUrl, {
        claim_token: claimLink.claimToken,
        email,
        password,
      });

      if (claim.status === 'success') {
        renderClaimTerminal(
          'done',
          'Subscription active',
          'Your paid Onyx subscription is active and your account session has been created. Install or open the iOS app to continue.',
          true,
        );
        return;
      }

      if (claim.status === 'claimed_existing_account') {
        renderClaimTerminal(
          'done',
          'Subscription active',
          'Your subscription is now active on your existing account — sign in with your usual password.',
          true,
        );
        return;
      }

      if (claim.status === 'invalid_or_expired') {
        renderInvalidClaimTerminal();
        return;
      }

      if (claim.status === 'already_claimed') {
        renderAlreadyClaimedTerminal();
        return;
      }

      if (claim.status === 'existing_account') {
        renderClaimTerminal(
          'error',
          'Account already exists',
          'We could not safely finish this claim because the account state changed. Retry the same claim link, or contact hello@mail.onyxapp.io.',
        );
        return;
      }

      if (claim.status === 'queued') {
        renderClaimStatus('running', 'Activation pending', 'Onyx is still reconciling this paid order. Try the same claim link again in a moment.');
        return;
      }

      renderClaimStatus('error', 'Activation delayed', claim.reason ?? 'The Product API could not finish the claim. Try again in a moment.');
    } catch (error) {
      const message = error instanceof Error ? error.message : 'unknown_error';
      renderClaimStatus('error', 'Account setup failed', message);
    } finally {
      if (form.isConnected) setClaimDisabled(false);
    }
  });
}

function renderInvalidClaimTerminal() {
  renderClaimTerminal(
    'error',
    'Claim link invalid or expired',
    'Use the latest claim email from Onyx or contact support if your payment was already confirmed.',
  );
}

function renderAlreadyClaimedTerminal() {
  renderClaimTerminal(
    'done',
    'Your subscription is already active',
    'This paid order is already attached to an Onyx account. Open the app and sign in with the email that claimed it.',
    true,
  );
}

function renderClaimTerminal(kind: 'done' | 'error', title: string, body: string, includeAppStoreLink = false) {
  appRoot.innerHTML = `
    <main class="claim-shell">
      <header class="claim-header" aria-label="Onyx">
        <a class="claim-wordmark" href="/" aria-label="Onyx home">ONYX</a>
      </header>
      <section class="claim-card claim-result" aria-live="polite">
        <span class="status-dot ${kind}"></span>
        <p class="kicker">Onyx account claim</p>
        <h1>${title}</h1>
        <p class="claim-copy">${body}</p>
        ${includeAppStoreLink ? `<a class="app-store-link" href="${onyxAppStoreUrl}" target="_blank" rel="noreferrer">Open Onyx in the App Store</a>` : ''}
      </section>
    </main>
  `;
}

type StripeEmbeddedCheckoutInstance = {
  mount(container: HTMLElement): void;
  destroy?(): void;
};

type StripeEmbeddedCheckoutOptions = {
  clientSecret?: string;
  fetchClientSecret?: () => Promise<string>;
  onComplete(): void;
};

type StripeJs = {
  createEmbeddedCheckoutPage?: (options: StripeEmbeddedCheckoutOptions) => Promise<StripeEmbeddedCheckoutInstance>;
  initEmbeddedCheckout?: (options: StripeEmbeddedCheckoutOptions) => Promise<StripeEmbeddedCheckoutInstance>;
};

type StripeJsFactory = (publishableKey: string) => StripeJs | undefined;

declare global {
  interface Window {
    Stripe?: StripeJsFactory;
  }
}

if (!isClaimPath(window.location.pathname)) {
const form = document.querySelector<HTMLFormElement>('#scan-form');
const subjectType = document.querySelector<HTMLSelectElement>('#subject-type');
const subject = document.querySelector<HTMLInputElement>('#subject');
const statusPanel = document.querySelector<HTMLDivElement>('#status-panel');
const checkoutForm = document.querySelector<HTMLFormElement>('#checkout-form');
const checkoutEmail = document.querySelector<HTMLInputElement>('#checkout-email');
const paymentOfferPanel = document.querySelector<HTMLDivElement>('#payment-offer');
const paymentStatusPanel = document.querySelector<HTMLDivElement>('#payment-status-panel');

const placeholders: Record<SubjectType, string> = {
  email: 'name@example.com',
  password: 'Try a known weak password',
  phone: '+15551234567',
};

const fSessionId = `f_session_${crypto.randomUUID()}`;
let offerResponse: PaymentOfferQueryResponse | undefined;
let activeMountedPaymentForm: { destroy?: () => void } | undefined;
let activeCheckoutAttempt: { signature: string; idempotencyKey: string } | undefined;
let isCheckoutSubmitting = false;

const stripeEmbeddedCheckoutScriptUrl = 'https://js.stripe.com/basil/stripe.js';
let stripeEmbeddedCheckoutScriptPromise: Promise<void> | undefined;

subjectType?.addEventListener('change', () => {
  const selected = subjectType.value as SubjectType;
  if (subject) subject.placeholder = placeholders[selected];
});

function renderStatus(kind: 'idle' | 'running' | 'done' | 'error', title: string, body: string) {
  renderPanel(statusPanel, kind, title, body);
}

function renderPaymentStatus(kind: 'idle' | 'running' | 'done' | 'error', title: string, body: string) {
  renderPanel(paymentStatusPanel, kind, title, body);
}

function renderPanel(panel: HTMLDivElement | null, kind: 'idle' | 'running' | 'done' | 'error', title: string, body: string) {
  if (!panel) return;
  panel.innerHTML = `
    <span class="status-dot ${kind}"></span>
    <div>
      <strong></strong>
      <p></p>
    </div>
  `;
  const titleElement = panel.querySelector('strong');
  const bodyElement = panel.querySelector('p');
  if (titleElement) titleElement.textContent = title;
  if (bodyElement) bodyElement.textContent = body;
}

function renderOffer(offer: PaymentOfferQueryResponse['offers'][number]) {
  if (!paymentOfferPanel) return;
  const amount = new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: offer.currency,
  }).format(offer.amount_minor / 100);
  paymentOfferPanel.innerHTML = '<strong></strong><p></p>';
  const title = paymentOfferPanel.querySelector('strong');
  const body = paymentOfferPanel.querySelector('p');
  if (title) title.textContent = offer.display_name;
  if (body) body.textContent = `${amount} / ${offer.billing_period}. Price and provider IDs are controlled by the backend.`;
}

async function pollScan(scanRunId: string): Promise<ScanStatusResponse> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const status = await getScanStatus(marketingApiBaseUrl, scanRunId);
    if (
      status.state === 'completed' ||
      status.state === 'completed_partial' ||
      status.state === 'failed' ||
      status.state === 'expired'
    ) {
      return status;
    }
    await delay(1800);
  }

  return getScanStatus(marketingApiBaseUrl, scanRunId);
}

async function pollPaymentStatus(paymentOrderId: string): Promise<PaymentOrderStatusResponse> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const status = await getPaymentOrderStatus(paymentApiBaseUrl, {
      f_session_id: fSessionId,
      payment_order_id: paymentOrderId,
    });
    if (status.payment.status === 'paid_reserved' || status.payment.status === 'failed' || status.access.status === 'active') {
      return status;
    }
    await delay(1800);
  }

  return getPaymentOrderStatus(paymentApiBaseUrl, {
    f_session_id: fSessionId,
    payment_order_id: paymentOrderId,
  });
}

async function loadPaymentOffer() {
  try {
    renderPaymentStatus('running', 'Loading checkout offer', 'Requesting backend-approved pricing and offer token…');
    offerResponse = await queryPaymentOffers(paymentApiBaseUrl, { f_session_id: fSessionId });
    renderOffer(offerResponse.offers[0]);
    renderPaymentStatus('idle', 'Offer ready', 'Enter your email to create a backend-signed Payment Form session.');
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown_error';
    renderPaymentStatus('error', 'Offer unavailable', message);
  }
}

function setCheckoutDisabled(disabled: boolean) {
  const submitButton = checkoutForm?.querySelector<HTMLButtonElement>('button[type="submit"]');
  submitButton?.toggleAttribute('disabled', disabled);
}

function getCheckoutIdempotencyKey(email: string, offerSelectionToken: string): string {
  const signature = `${offerSelectionToken}:${email.trim().toLowerCase()}`;
  if (activeCheckoutAttempt?.signature !== signature) {
    activeCheckoutAttempt = {
      signature,
      idempotencyKey: `checkout_${crypto.randomUUID()}`,
    };
  }
  return activeCheckoutAttempt.idempotencyKey;
}

function validateStripeHostedCheckoutUrl(checkoutUrl: string) {
  const parsedCheckoutUrl = new URL(checkoutUrl);
  if (parsedCheckoutUrl.protocol !== 'https:' || parsedCheckoutUrl.hostname !== 'checkout.stripe.com') {
    throw new Error('provider_checkout_redirect_unsafe');
  }
}

form?.addEventListener('submit', async (event) => {
  event.preventDefault();

  const selectedType = subjectType?.value as SubjectType | undefined;
  const value = subject?.value.trim();

  if (!selectedType || !value) {
    renderStatus('error', 'Missing scan input', 'Choose a type and enter a value to scan.');
    return;
  }

  try {
    renderStatus('running', 'Creating durable scan', 'Writing to Cloud SQL and publishing to Pub/Sub…');
    const created = await requestScan(marketingApiBaseUrl, {
      subjectType: selectedType,
      subject: value,
      source: 'marketing-funnel',
      attribution: {
        fSessionId,
        surface: 'marketing-web',
      },
    });

    renderStatus('running', `Scan queued: ${created.scanRunId}`, 'Workers are checking providers and storing sanitized results…');
    const finalStatus = await pollScan(created.scanRunId);
    const succeeded = finalStatus.state === 'completed' || finalStatus.state === 'completed_partial';
    renderStatus(succeeded ? 'done' : 'error', `Scan ${finalStatus.state}`, `${describeScanResult(finalStatus)} ID: ${finalStatus.scanRunId}`);
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown_error';
    renderStatus('error', 'Scan request failed', message);
  }
});

checkoutForm?.addEventListener('submit', async (event) => {
  event.preventDefault();

  if (isCheckoutSubmitting) return;

  const email = checkoutEmail?.value.trim();
  if (!email) {
    renderPaymentStatus('error', 'Email required', 'Enter the email that should receive the account claim link.');
    return;
  }
  if (!offerResponse) {
    renderPaymentStatus('error', 'Offer unavailable', 'Refresh the offer before creating checkout.');
    return;
  }

  try {
    isCheckoutSubmitting = true;
    setCheckoutDisabled(true);
    activeMountedPaymentForm?.destroy?.();
    activeMountedPaymentForm = undefined;
    renderPaymentStatus('running', 'Creating checkout session', 'Backend is creating a provider checkout session…');
    const checkout = await createPaymentCheckoutSession(paymentApiBaseUrl, getCheckoutIdempotencyKey(email, offerResponse.offer_selection_token), {
      f_session_id: fSessionId,
      offer_selection_token: offerResponse.offer_selection_token,
      email,
    });
    const paymentOrderId = checkout.payment_order.payment_order_id;

    if (checkout.next_action === 'redirect_to_provider_checkout') {
      if (checkout.stripe?.mode !== 'hosted_redirect') {
        throw new Error('provider_checkout_redirect_missing');
      }
      validateStripeHostedCheckoutUrl(checkout.stripe.checkout_url);
      renderPaymentStatus('running', 'Redirecting to secure checkout', 'Complete payment with the provider. Access still waits for webhook confirmation.');
      window.location.assign(checkout.stripe.checkout_url);
      return;
    }

    if (checkout.stripe?.mode === 'embedded') {
      const container = document.querySelector<HTMLDivElement>('#solid-payment-form-container');
      if (!container) {
        throw new Error('payment_form_container_missing');
      }

      renderPaymentStatus('running', 'Payment Form ready', 'Render the secure checkout and complete payment. Access still waits for webhook confirmation.');
      activeMountedPaymentForm = await mountStripeEmbeddedCheckout({
        container,
        clientSecret: checkout.stripe.client_secret,
        publishableKey: checkout.stripe.publishable_key,
        onComplete() {
          refreshMountedPaymentStatus(paymentOrderId);
        },
      });
      return;
    }

    if (!checkout.merchant_data) {
      throw new Error('solidgate_merchant_data_missing');
    }

    renderPaymentStatus('running', 'Payment Form ready', 'Render the secure iframe and complete payment. Access still waits for webhook confirmation.');

    activeMountedPaymentForm = await mountSolidgatePaymentForm({
      containerId: 'solid-payment-form-container',
      merchantData: checkout.merchant_data,
      onEvent(eventName) {
        renderPaymentStatus('running', 'Payment form update', describePaymentFormUxEvent(eventName));
        refreshMountedPaymentStatus(paymentOrderId);
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown_error';
    renderPaymentStatus('error', 'Checkout failed', message);
  } finally {
    isCheckoutSubmitting = false;
    if (checkoutForm.isConnected) setCheckoutDisabled(false);
  }
});

function refreshMountedPaymentStatus(activePaymentOrderId: string) {
  void refreshPaymentStatus(activePaymentOrderId);
}

async function refreshPaymentStatus(paymentOrderId: string) {
  try {
    const status = await pollPaymentStatus(paymentOrderId);
    const success = status.payment.status === 'paid_reserved' || status.access.status === 'active';
    renderPaymentStatus(success ? 'done' : status.payment.status === 'failed' ? 'error' : 'running', 'Backend payment status', describePaymentOrderStatus(status));
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown_error';
    renderPaymentStatus('error', 'Status polling failed', message);
  }
}

async function loadStripeEmbeddedCheckoutScript(): Promise<void> {
  if (window.Stripe) return;
  if (stripeEmbeddedCheckoutScriptPromise) return stripeEmbeddedCheckoutScriptPromise;

  stripeEmbeddedCheckoutScriptPromise = new Promise((resolve, reject) => {
    const existingScript = document.querySelector<HTMLScriptElement>(`script[src="${stripeEmbeddedCheckoutScriptUrl}"]`);
    if (existingScript) {
      existingScript.addEventListener('load', () => resolve(), { once: true });
      existingScript.addEventListener('error', () => reject(new Error('stripe_script_failed')), { once: true });
      return;
    }

    const script = document.createElement('script');
    script.src = stripeEmbeddedCheckoutScriptUrl;
    script.async = true;
    script.addEventListener('load', () => resolve(), { once: true });
    script.addEventListener('error', () => reject(new Error('stripe_script_failed')), { once: true });
    document.head.append(script);
  });

  stripeEmbeddedCheckoutScriptPromise = stripeEmbeddedCheckoutScriptPromise.catch((error) => {
    stripeEmbeddedCheckoutScriptPromise = undefined;
    document.querySelector<HTMLScriptElement>(`script[src="${stripeEmbeddedCheckoutScriptUrl}"]`)?.remove();
    throw error;
  });

  return stripeEmbeddedCheckoutScriptPromise;
}

async function mountStripeEmbeddedCheckout(options: {
  container: HTMLElement;
  clientSecret: string;
  publishableKey: string;
  onComplete(): void;
}): Promise<StripeEmbeddedCheckoutInstance> {
  await loadStripeEmbeddedCheckoutScript();
  const stripe = window.Stripe?.(options.publishableKey);
  if (!stripe) throw new Error('stripe_sdk_unavailable');

  const checkout = await createStripeEmbeddedCheckout(stripe, {
    clientSecret: options.clientSecret,
    onComplete: options.onComplete,
  });
  checkout.mount(options.container);
  return checkout;
}

async function createStripeEmbeddedCheckout(stripe: StripeJs, options: {
  clientSecret: string;
  onComplete(): void;
}): Promise<StripeEmbeddedCheckoutInstance> {
  if (stripe.createEmbeddedCheckoutPage) {
    return stripe.createEmbeddedCheckoutPage({
      fetchClientSecret: async () => options.clientSecret,
      onComplete: options.onComplete,
    });
  }

  if (stripe.initEmbeddedCheckout) {
    return stripe.initEmbeddedCheckout({
      clientSecret: options.clientSecret,
      onComplete: options.onComplete,
    });
  }

  throw new Error('stripe_embedded_checkout_unavailable');
}

function delay(milliseconds: number) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

void loadPaymentOffer();
}
