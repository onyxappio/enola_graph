export type ForgotPasswordRuntimeLocation =
  | 'welcome'
  | 'login'
  | 'forgotPassword'
  | 'requestingReset'
  | 'checkInbox'
  | 'changePassword'
  | 'resettingPassword'
  | 'passwordChanged';

export type ForgotPasswordRuntimeState = {
  location: ForgotPasswordRuntimeLocation;
  email: string;
  resetToken: string | null;
  newPassword: string;
  confirmPassword: string;
  resendCooldownSeconds: number;
  devResetToken: string | null;
  message: string | null;
  /**
   * Per-field reveal gates (T-217/T-241, same contract as create-account's
   * `emailBlurred`/`passwordBlurred`): they gate WHEN a validation error may
   * appear, never WHAT it says. A field scolds only once the user has left it,
   * not while they are still typing into it. Optional and default-false, so
   * every existing state literal (tests, fixtures, machine context) keeps the
   * silent-while-typing behaviour without being rewritten.
   */
  newPasswordBlurred?: boolean;
  confirmPasswordBlurred?: boolean;
};

export type ForgotPasswordTimerPhase = 'resendCooldown' | 'successRedirect';

export function createForgotPasswordRuntimeState(
  overrides: Partial<ForgotPasswordRuntimeState> = {},
): ForgotPasswordRuntimeState {
  return {
    location: 'login',
    email: '',
    resetToken: null,
    newPassword: '',
    confirmPassword: '',
    resendCooldownSeconds: 0,
    devResetToken: null,
    message: null,
    ...overrides,
  };
}

export function isValidForgotPasswordEmail(email: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
}

export function isValidResetPassword(password: string) {
  return password.length >= 8;
}

export function canSubmitResetRequest(state: ForgotPasswordRuntimeState) {
  return isValidForgotPasswordEmail(state.email) && state.location !== 'requestingReset';
}

export function canSubmitNewPassword(state: ForgotPasswordRuntimeState) {
  return Boolean(state.resetToken) && isValidResetPassword(state.newPassword) && state.newPassword === state.confirmPassword && state.location !== 'resettingPassword';
}

/**
 * Neutral guidance, shown by default. It is the field's requirement, not a
 * complaint, so it is always visible — only the error strings below are gated.
 * ProjectedFigmaScreen derives the red Figma error variant from the string
 * ("Password must…" / "Passwords must match"), so gating the string is exactly
 * gating the error state.
 */
const RESET_PASSWORD_GUIDANCE = 'Must be at least 8 characters';

export function passwordHelperFor(state: ForgotPasswordRuntimeState) {
  if (!state.newPasswordBlurred) return RESET_PASSWORD_GUIDANCE;
  if (!state.newPassword) return RESET_PASSWORD_GUIDANCE;
  if (!isValidResetPassword(state.newPassword)) return 'Password must be at least 8 characters';
  return RESET_PASSWORD_GUIDANCE;
}

export function confirmPasswordHelperFor(state: ForgotPasswordRuntimeState) {
  if (!state.confirmPasswordBlurred) return RESET_PASSWORD_GUIDANCE;
  if (!state.confirmPassword) return RESET_PASSWORD_GUIDANCE;
  if (!isValidResetPassword(state.confirmPassword)) return 'Password must be at least 8 characters';
  if (state.newPassword !== state.confirmPassword) return 'Passwords must match';
  return RESET_PASSWORD_GUIDANCE;
}
