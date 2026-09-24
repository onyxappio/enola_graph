import {
  describe,
  expect,
  it,
} from 'vitest'
import {
  declineReasonFromClientError,
  declineReasonFromOrderStatus,
  isTerminalConfirmFailure,
} from './declineReason'

class FakeSdkStripeError extends Error {
  constructor(readonly raw: unknown) {
    super('confirm failed')
  }
}

class FakeWrapperError extends Error {
  constructor(readonly code: string, readonly raw: unknown = undefined) {
    super(code)
  }
}

describe('declineReasonFromClientError', () => {
  it('maps insufficient_funds decline_code from an SDK error raw payload', () => {
    const error = new FakeSdkStripeError({
      type: 'card_error',
      code: 'card_declined',
      decline_code: 'insufficient_funds',
      message: 'Your card has insufficient funds.',
    })

    expect(declineReasonFromClientError(error)).toBe('insufficient_funds')
  })

  it('masks every other decline_code as generic (incl. codes Stripe requires masking)', () => {
    for (const declineCode of ['generic_decline', 'do_not_honor', 'fraudulent', 'stolen_card', 'lost_card']) {
      const error = new FakeSdkStripeError({
        type: 'card_error',
        code: 'card_declined',
        decline_code: declineCode,
      })

      expect(declineReasonFromClientError(error)).toBe('generic')
    }
  })

  it('treats card_error without decline_code as a generic decline', () => {
    const error = new FakeSdkStripeError({
      type: 'card_error',
      code: 'expired_card',
    })

    expect(declineReasonFromClientError(error)).toBe('generic')
  })

  it('reads a bare Stripe error object without an SDK wrapper', () => {
    expect(declineReasonFromClientError({
      type: 'card_error',
      code: 'card_declined',
      decline_code: 'insufficient_funds',
    })).toBe('insufficient_funds')
  })

  it('returns null for non-decline failures', () => {
    expect(declineReasonFromClientError(new TypeError('failed to fetch'))).toBeNull()
    expect(declineReasonFromClientError(new FakeSdkStripeError({ type: 'api_error' }))).toBeNull()
    expect(declineReasonFromClientError(null)).toBeNull()
    expect(declineReasonFromClientError('boom')).toBeNull()
  })
})

describe('isTerminalConfirmFailure', () => {
  it('is terminal for card declines', () => {
    expect(isTerminalConfirmFailure(new FakeSdkStripeError({
      type: 'card_error',
      code: 'card_declined',
      decline_code: 'insufficient_funds',
    }))).toBe(true)
    expect(isTerminalConfirmFailure(new FakeSdkStripeError({
      type: 'card_error',
      code: 'expired_card',
    }))).toBe(true)
  })

  it('is terminal for a failed or cancelled 3DS authentication (no decline_code)', () => {
    // Stripe surfaces 3DS failure/cancellation without a decline_code, often
    // with type invalid_request_error — the reported dead Try again case.
    expect(isTerminalConfirmFailure(new FakeSdkStripeError({
      type: 'invalid_request_error',
      code: 'payment_intent_authentication_failure',
    }))).toBe(true)
    expect(isTerminalConfirmFailure({ code: 'payment_intent_payment_attempt_failed' })).toBe(true)
  })

  it('is terminal for a non-charging terminal intent status wrapper code', () => {
    expect(isTerminalConfirmFailure(new FakeWrapperError('stripe_split_card_unexpected_status'))).toBe(true)
    expect(isTerminalConfirmFailure(new FakeWrapperError('stripe_intents_unexpected_status'))).toBe(true)
  })

  it('is NOT terminal for ambiguous transport failures (keep locator for resume)', () => {
    expect(isTerminalConfirmFailure(new TypeError('failed to fetch'))).toBe(false)
    expect(isTerminalConfirmFailure(new FakeSdkStripeError({ type: 'api_connection_error' }))).toBe(false)
    expect(isTerminalConfirmFailure(new FakeSdkStripeError({ type: 'api_error' }))).toBe(false)
    expect(isTerminalConfirmFailure(new FakeWrapperError('stripe_custom_form_confirm_failed'))).toBe(false)
    expect(isTerminalConfirmFailure(null)).toBe(false)
    expect(isTerminalConfirmFailure('boom')).toBe(false)
  })
})

describe('declineReasonFromOrderStatus', () => {
  it('reads the backend-normalized payment.decline_reason', () => {
    expect(declineReasonFromOrderStatus({
      payment: {
        status: 'failed',
        decline_reason: 'insufficient_funds',
      },
    })).toBe('insufficient_funds')
    expect(declineReasonFromOrderStatus({
      payment: {
        status: 'failed',
        decline_reason: 'generic',
      },
    })).toBe('generic')
  })

  it('ignores missing, empty, or unknown reasons from older backends', () => {
    expect(declineReasonFromOrderStatus({ payment: { status: 'failed' } })).toBeNull()
    expect(declineReasonFromOrderStatus({
      payment: {
        status: 'failed',
        decline_reason: 'do_not_honor',
      },
    })).toBeNull()
    expect(declineReasonFromOrderStatus({})).toBeNull()
    expect(declineReasonFromOrderStatus(null)).toBeNull()
  })
})
