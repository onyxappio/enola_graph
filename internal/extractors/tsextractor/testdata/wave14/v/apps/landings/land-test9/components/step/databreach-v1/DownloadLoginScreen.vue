<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRef } from 'vue'
import type {
  Web2AppClaimEmailResendLocators,
  Web2AppHandoffView,
} from '../../../utils/web2appHandoffApi'
import {
  ONYX_SUPPORT_TICKET_URL,
  buildWeb2AppSupportUrl,
  resolveWeb2AppAppLink,
} from '../../../utils/web2appHandoffUrls'
import { useClaimEmailResend } from '../../../composables/useClaimEmailResend'
import onyxSymbol from './brand-icons/onyx-symbol.svg?url'
import illustrationLoginBase from './download-login/illustration-login-base@2x.png'
import tapHand from './download-login/tap-hand.svg?url'
import ClaimEmailRecovery from './ClaimEmailRecovery.vue'

if (import.meta.dev) {
  setLandPageMetadata({
    page: 'step/databreach-v1/DownloadLoginScreen',
    description: 'Web2App post-payment download and login handoff',
  })
}

const props = withDefaults(defineProps<{
  view?: Web2AppHandoffView
  email?: string | null
  password?: string | null
  appLink?: string | null
  installStarted?: boolean
  claimResend?: Web2AppClaimEmailResendLocators | null
}>(), {
  view: 'ready',
  email: null,
  password: null,
  appLink: null,
  installStarted: false,
  claimResend: null,
})

type CredentialField = 'email' | 'password'

const emit = defineEmits<{
  download: []
  retry: []
  copyCredential: [field: CredentialField]
  openApp: []
  support: []
  resendClaim: []
}>()
const copiedField = ref<CredentialField | null>(null)
const localInstallStarted = ref(false)
const installStarted = computed(() => Boolean(props.installStarted) || localInstallStarted.value)
const downloadHref = computed(() => resolveWeb2AppAppLink(props.appLink))
const deepLinkRef = ref<HTMLAnchorElement | null>(null)
const credsRef = ref<HTMLElement | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const visibleEmail = computed(() => props.email ?? '')
const supportHref = computed(() => (
  visibleEmail.value ? buildWeb2AppSupportUrl(visibleEmail.value) : ONYX_SUPPORT_TICKET_URL
))
const isReady = computed(() => props.view === 'ready')
const isProcessing = computed(() => props.view === 'processing')
const isRetryable = computed(() => props.view === 'retryable_error' || props.view === 'unknown_timeout')
const isPaymentRetry = computed(() => props.view === 'payment_retry')
const isExpired = computed(() => props.view === 'expired_support')
const isConflict = computed(() => props.view === 'conflict_support')
const isClaimEmailSent = computed(() => props.view === 'claim_email_sent')
const isClaimEmailPending = computed(() => props.view === 'claim_email_pending')
const isClaimEmailRecovery = computed(() => isClaimEmailSent.value || isClaimEmailPending.value)
const showsRetryCta = computed(() => isRetryable.value || isPaymentRetry.value)
const claimResendRef = toRef(props, 'claimResend')
const {
  canResendClaimEmail,
  resendClaimDisabled,
  resendClaimButtonText,
  resendClaimFeedbackText,
  handleResendClaimEmail,
} = useClaimEmailResend({
  locators: () => (isClaimEmailSent.value ? claimResendRef.value : null),
})
const showClaimResend = computed(() => isClaimEmailSent.value && canResendClaimEmail.value)

async function onClaimEmailResend() {
  emit('resendClaim')
  await handleResendClaimEmail()
}
const title = computed(() => {
  if (isProcessing.value) {
    return 'One moment — we are preparing your login.'
  }
  if (props.view === 'unknown_timeout') {
    return 'This is taking longer than expected.'
  }
  if (isPaymentRetry.value) {
    return 'Your payment did not go through.'
  }
  if (isRetryable.value) {
    return 'We could not finish setting up your login.'
  }
  if (isClaimEmailSent.value) {
    return 'Check your email to finish logging in.'
  }
  if (isClaimEmailPending.value) {
    return 'We are preparing your access email.'
  }
  if (isExpired.value || isConflict.value) {
    return 'Contact support to finish logging in.'
  }
  return "One step left. Let's log in!"
})

async function copyCredential(field: CredentialField) {
  const value = field === 'email' ? props.email : props.password
  if (!value || typeof navigator === 'undefined') {
    return
  }

  // Field enum only — never include credential values in analytics.
  emit('copyCredential', field)

  try {
    await navigator.clipboard.writeText(value)
  } catch {
    const helper = document.createElement('textarea')
    helper.value = value
    helper.setAttribute('readonly', '')
    helper.setAttribute('aria-hidden', 'true')
    helper.setAttribute('data-sentry-mask', '')
    helper.setAttribute('data-sentry-block', '')
    helper.style.position = 'fixed'
    helper.style.opacity = '0'
    const host = credsRef.value ?? document.body
    host.appendChild(helper)
    helper.select()
    document.execCommand?.('copy')
    helper.remove()
  }

  copiedField.value = field
  if (copiedTimer) {
    clearTimeout(copiedTimer)
  }
  copiedTimer = setTimeout(() => {
    copiedField.value = null
    copiedTimer = undefined
  }, 1600)
}

async function handleDownload() {
  localInstallStarted.value = true
  emit('download')
  await nextTick()
  deepLinkRef.value?.focus()
}

onBeforeUnmount(() => {
  if (copiedTimer) {
    clearTimeout(copiedTimer)
  }
})
</script>

<template>
  <div class="dl-page">
    <header class="dl-topbar">
      <span class="dl-logo">
        <img class="dl-logo__symbol" :src="onyxSymbol" alt="" draggable="false">
        <span class="dl-logo__name">Onyx</span>
      </span>
    </header>

    <main class="dl-scroll">
      <h1 class="dl-title">{{ title }}</h1>

      <p v-if="isProcessing" class="dl-status">
        Processing your payment. This can take a few seconds.
      </p>
      <p v-else-if="isPaymentRetry" class="dl-status">
        Please try the payment again. We will not keep retrying login setup for this failed order.
      </p>
      <p v-else-if="isRetryable" class="dl-status">
        Something went wrong on our side. You can try again without creating a second account.
      </p>
      <p v-else-if="isClaimEmailSent" class="dl-status">
        Use the login button in your email — we will not show a password here.
      </p>
      <p v-else-if="isClaimEmailPending" class="dl-status">
        Your access email is still being prepared. Check back shortly.
      </p>
      <p v-else-if="isConflict" class="dl-status">
        This email already has an account. Contact support — we will not reset your password.
      </p>
      <p v-else-if="isExpired" class="dl-status">
        This login is no longer available in this browser. Contact support to recover access.
      </p>

      <ClaimEmailRecovery
        v-if="isClaimEmailRecovery"
        class="dl-claim-recovery"
        :mode="isClaimEmailPending ? 'pending' : 'sent'"
        :inbox-email="email"
        :show-resend="showClaimResend"
        :resend-disabled="resendClaimDisabled"
        :resend-button-text="resendClaimButtonText"
        :resend-feedback-text="resendClaimFeedbackText"
        @resend="onClaimEmailResend"
      />

      <section v-if="isReady" class="dl-steps-card">
        <ol class="dl-steps">
          <li class="dl-step">
            <span class="dl-step__num" aria-hidden="true">1</span>
            <p class="dl-step__text">
              Download
              <a
                class="dl-step__link"
                :href="downloadHref"
                target="_blank"
                rel="noopener noreferrer"
                @click="handleDownload"
              >the Onyx app</a>
              on the App Store.
            </p>
          </li>
          <li class="dl-step">
            <span class="dl-step__num" aria-hidden="true">2</span>
            <p class="dl-step__text">
              Tap <strong>‘Log in’</strong>.
            </p>
          </li>
          <li class="dl-step">
            <span class="dl-step__num" aria-hidden="true">3</span>
            <p class="dl-step__text">
              Log in with this <strong>email and password</strong>.
            </p>
          </li>
        </ol>

        <div ref="credsRef" class="dl-creds" data-sentry-mask data-sentry-block>
          <div class="dl-cred">
            <span class="dl-cred__value" aria-label="Email">{{ email }}</span>
            <button
              type="button"
              class="dl-cred__copy"
              :class="{ 'dl-cred__copy--done': copiedField === 'email' }"
              aria-label="Copy email"
              @click="copyCredential('email')"
            >
              {{ copiedField === 'email' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <div class="dl-cred">
            <span class="dl-cred__value" aria-label="Password">{{ password }}</span>
            <button
              type="button"
              class="dl-cred__copy"
              :class="{ 'dl-cred__copy--done': copiedField === 'password' }"
              aria-label="Copy password"
              @click="copyCredential('password')"
            >
              {{ copiedField === 'password' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <div v-if="installStarted" class="dl-open-app">
            <p class="dl-open-app__note">Tap this once the app is downloaded</p>
            <a
              ref="deepLinkRef"
              class="dl-open-app__btn"
              :href="downloadHref"
              @click="emit('openApp')"
            >
              Open Onyx and log in
            </a>
          </div>
        </div>
      </section>

      <div v-if="isReady" class="dl-art">
        <div class="dl-art__frame">
          <img class="dl-art__img" :src="illustrationLoginBase" alt="" draggable="false">
          <span class="dl-art__ripple" aria-hidden="true" />
          <img class="dl-art__hand" :src="tapHand" alt="" draggable="false">
        </div>
      </div>

      <a
        v-if="isReady || isExpired || isConflict || isClaimEmailRecovery"
        class="dl-help"
        :href="supportHref"
        target="_blank"
        rel="noopener noreferrer"
        referrerpolicy="no-referrer"
        @click="emit('support')"
      >
        <p class="dl-help__title">Trouble logging in?</p>
        <p class="dl-help__text">
          Contact our support team <span class="dl-help__link">here</span>.
        </p>
      </a>

      <div v-if="showsRetryCta" class="dl-retry">
        <button type="button" class="dl-retry__btn" @click="emit('retry')">
          {{ isPaymentRetry ? 'Try payment again' : 'Try again' }}
        </button>
      </div>

      <div class="dl-scroll-gap" aria-hidden="true" />
    </main>

    <footer v-if="isReady" class="dl-footer">
      <a
        class="dl-download"
        :href="downloadHref"
        target="_blank"
        rel="noopener noreferrer"
        @click="handleDownload"
      >
        Download on the App Store
      </a>
    </footer>
  </div>
</template>

<style scoped>
@property --dl-border-angle {
  syntax: '<angle>';
  initial-value: 0deg;
  inherits: false;
}

.dl-page {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  width: 100%;
  min-height: 100dvh;
  padding: 16px 16px max(16px, env(safe-area-inset-bottom));
  background: var(--color-bg);
  color: var(--color-text);
}
.dl-page *,
.dl-page *::before,
.dl-page *::after { box-sizing: inherit; }

.dl-topbar,
.dl-scroll,
.dl-footer {
  width: 100%;
  max-width: 430px;
  margin-inline: auto;
}

.dl-topbar {
  display: flex;
  justify-content: center;
  padding: 4px 0 24px;
}

.dl-logo {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 8px 12px;
  border-radius: 12px;
  background: #111213;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.dl-logo__symbol {
  display: block;
  width: 20px;
  height: 20px;
}

.dl-logo__name {
  font-size: 20px;
  line-height: 24px;
  font-weight: 500;
  color: #ffffff;
}

.dl-scroll {
  display: flex;
  flex-direction: column;
  flex: 1;
}
.dl-scroll-gap { flex-shrink: 0; height: 48px; }

.dl-title {
  margin: 0;
  text-align: center;
  font-size: 24px;
  line-height: 28px;
  font-weight: 600;
  letter-spacing: -0.02em;
  text-wrap: balance;
}

.dl-status {
  margin: 16px 0 0;
  text-align: center;
  font-size: 16px;
  line-height: 22px;
  color: var(--color-text-secondary, #c8c9cc);
}

.dl-claim-recovery {
  margin-top: 24px;
}

.dl-steps-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-top: 24px;
  padding: 16px;
  border-radius: 24px;
  border: 1px solid var(--color-border);
  background: var(--color-bg-surface-tertiary);
}

.dl-steps {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.dl-step {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.dl-step__num {
  flex: none;
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--color-bg-surface-success);
  border: 1px solid var(--color-border);
  font-size: 13px;
  font-weight: 600;
  color: var(--color-success);
}

.dl-step__text {
  margin: 0;
  padding-top: 2px;
  font-size: 16px;
  line-height: 22px;
  font-weight: 500;
  color: var(--color-text-secondary);
}

.dl-step__text strong {
  font-weight: 600;
  color: var(--color-text);
}

.dl-step__link {
  font-weight: 600;
  color: var(--legal-link-c, #6eb5ff);
  text-decoration: none;
}

.dl-creds {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dl-cred {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-radius: 16px;
  border: 1px solid transparent;
  background:
    linear-gradient(var(--color-bg-surface-secondary), var(--color-bg-surface-secondary)) padding-box,
    conic-gradient(from var(--dl-border-angle), #3ad07e, #3e66ff, #3ad07e) border-box;
  padding: 14px 16px;
  animation: dl-border-spin 6s linear infinite;
}

@keyframes dl-border-spin {
  to { --dl-border-angle: 360deg; }
}

@media (prefers-reduced-motion: reduce) {
  .dl-cred { animation: none; }
}

.dl-cred__value {
  min-width: 0;
  overflow-wrap: break-word;
  font-size: 15px;
  line-height: 20px;
  font-weight: 600;
  color: var(--color-text);
}

.dl-cred__copy {
  flex: none;
  border: 0;
  background: none;
  min-height: 44px;
  padding: 4px 0;
  cursor: pointer;
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-text-subdued);
}

.dl-cred__copy--done,
.dl-cred__copy--done:hover {
  color: var(--color-success);
}

.dl-open-app {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dl-open-app__note {
  margin: 0;
  text-align: center;
  font-size: 13px;
  line-height: 18px;
  font-weight: 500;
  color: var(--color-text-subdued);
}

.dl-open-app__btn {
  display: grid;
  place-items: center;
  min-height: 48px;
  border-radius: 16px;
  background: #ffffff;
  font-size: 15px;
  font-weight: 600;
  color: #18191a;
  text-decoration: none;
  padding: 12px 16px;
}

.dl-art {
  margin-top: 16px;
}

.dl-art__frame {
  position: relative;
  overflow: hidden;
  border-radius: 24px;
  border: 1px solid var(--color-border);
}

.dl-art__img {
  display: block;
  width: 100%;
  height: auto;
}

.dl-art__hand {
  position: absolute;
  left: 70.75%;
  top: 52.4%;
  width: 20.5%;
  height: auto;
  transform-origin: 27% 27%;
  animation: dl-hand-tap 2.8s cubic-bezier(.4, 0, .2, 1) infinite;
}

@keyframes dl-hand-tap {
  0%, 50%, 100% { transform: translate(0, 0) scale(1); }
  16%, 30% { transform: translate(-5%, -9%) scale(0.9); }
}

.dl-art__ripple {
  position: absolute;
  left: 71.7%;
  top: 54%;
  width: 9%;
  aspect-ratio: 1;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.85);
  opacity: 0;
  transform: scale(0.3);
  animation: dl-tap-ripple 2.8s ease-out infinite;
  pointer-events: none;
}

@keyframes dl-tap-ripple {
  0%, 14% { opacity: 0; transform: scale(0.3); }
  18% { opacity: 0.7; transform: scale(0.5); }
  38%, 100% { opacity: 0; transform: scale(1.6); }
}

@media (prefers-reduced-motion: reduce) {
  .dl-art__hand,
  .dl-art__ripple {
    animation: none;
  }
}

.dl-help {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 16px;
  border-radius: 20px;
  border: 1px solid var(--color-border);
  background: var(--color-bg-surface-tertiary);
  padding: 16px;
  color: var(--color-text);
  text-decoration: none;
}

.dl-help__title {
  margin: 0;
  font-size: 16px;
  line-height: 20px;
  font-weight: 600;
}

.dl-help__text {
  margin: 0;
  font-size: 16px;
  line-height: 22px;
  font-weight: 500;
  color: var(--color-text-subdued);
}

.dl-help__link {
  color: var(--legal-link-c, #6eb5ff);
  font-weight: 600;
}

.dl-retry {
  margin-top: 24px;
}

.dl-retry__btn {
  width: 100%;
  min-height: 48px;
  border: 0;
  border-radius: 16px;
  background: var(--color-bg-surface-secondary, #1a1a1d);
  color: var(--color-text);
  font: inherit;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
}

.dl-footer {
  position: sticky;
  bottom: 0;
  flex-shrink: 0;
  z-index: 10;
  margin-top: -40px;
  padding: 40px 0 max(16px, env(safe-area-inset-bottom));
  margin-bottom: calc(-1 * max(16px, env(safe-area-inset-bottom)));
  background: linear-gradient(180deg, transparent 0%, var(--color-bg) 40px);
}

.dl-download {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 56px;
  border-radius: 16px;
  background: var(--color-bg-brand, #ff5533);
  color: var(--color-text-on-brand, #fff);
  font-size: 16px;
  font-weight: 600;
  text-decoration: none;
}
</style>
