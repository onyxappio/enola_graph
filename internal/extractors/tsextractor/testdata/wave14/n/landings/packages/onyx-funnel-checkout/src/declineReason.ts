export type PaymentDeclineReason = 'insufficient_funds' | 'generic'

// Stripe permits naming insufficient_funds to the cardholder. Every other
// decline code (incl. fraudulent / lost_card / stolen_card, which Stripe
// requires to render exactly like generic_decline) collapses to 'generic'.
const NAMEABLE_DECLINE_CODES = new Set(['insufficient_funds'])

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function readString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value : undefined
}

function normalizeCode(code: string | undefined): PaymentDeclineReason | null {
  if (!code) {
    return null
  }

  return NAMEABLE_DECLINE_CODES.has(code) ? 'insufficient_funds' : 'generic'
}

function rawStripeError(error: unknown): Record<string, unknown> | null {
  if (!isRecord(error)) {
    return null
  }

  // checkout-vue Stripe*Error instances carry the raw Stripe error in `raw`.
  if (isRecord(error.raw)) {
    return error.raw
  }

  return error
}

/**
 * Normalizes a client-side Stripe confirm failure into a decline reason.
 * Returns null when the error is not a card decline (network/setup errors),
 * so callers can keep their non-decline failure copy for those.
 */
export function declineReasonFromClientError(error: unknown): PaymentDeclineReason | null {
  const raw = rawStripeError(error)
  if (!raw) {
    return null
  }

  const declineCode = readString(raw.decline_code)
  if (declineCode) {
    return normalizeCode(declineCode)
  }

  const code = readString(raw.code)
  if (raw.type === 'card_error' && code) {
    // card_error without decline_code (e.g. expired_card, incorrect_cvc)
    // is still a decline the user can act on; keep the copy generic.
    return normalizeCode(code)
  }

  return null
}

// Stripe confirm-result codes that mean the payment attempt is terminal even
// though they are not card declines: the provider will not settle this order.
// 3DS authentication failed or was cancelled by the cardholder, or the attempt
// failed outright. Their persisted locator must be dropped so Try again mints a
// fresh session instead of resume-polling a forever-pending order.
const TERMINAL_CONFIRM_ERROR_CODES = new Set([
  'payment_intent_authentication_failure',
  'payment_intent_payment_attempt_failed',
])

// checkout-vue wrapper codes (on the thrown Stripe*Error itself, not its raw
// payload) that signal a terminal, non-charging PaymentIntent status — e.g. the
// intent fell back to requires_payment_method after a failed authentication.
const TERMINAL_WRAPPER_ERROR_CODES = new Set([
  'stripe_split_card_unexpected_status',
  'stripe_intents_unexpected_status',
  'stripe_custom_form_unexpected_status',
])

/**
 * True when an in-page Stripe confirm failure is definitively terminal — a card
 * decline, a 3DS/authentication failure or cancellation, or a non-charging
 * terminal intent status. Callers drop the persisted locator and mint a fresh
 * session for these. Returns false for ambiguous transport failures (network /
 * api_connection), where the charge outcome is unknown and the locator must be
 * kept so resume-polling can still discover a late paid_reserved (double-charge
 * safety).
 */
export function isTerminalConfirmFailure(error: unknown): boolean {
  if (declineReasonFromClientError(error) !== null) {
    return true
  }

  if (isRecord(error)) {
    const wrapperCode = readString(error.code)
    if (wrapperCode && TERMINAL_WRAPPER_ERROR_CODES.has(wrapperCode)) {
      return true
    }
  }

  const raw = rawStripeError(error)
  if (!raw) {
    return false
  }

  if (raw.type === 'card_error') {
    return true
  }

  const code = readString(raw.code)

  return Boolean(code && TERMINAL_CONFIRM_ERROR_CODES.has(code))
}

/**
 * Reads the backend-normalized decline reason from an order-status payload
 * (`payment.decline_reason`, added by payment-api). Tolerates old backends
 * that do not send the field.
 */
export function declineReasonFromOrderStatus(status: unknown): PaymentDeclineReason | null {
  if (!isRecord(status)) {
    return null
  }

  const payment = status.payment
  if (!isRecord(payment)) {
    return null
  }

  const reason = readString(payment.decline_reason)
  if (reason === 'insufficient_funds' || reason === 'generic') {
    return reason
  }

  return null
}
