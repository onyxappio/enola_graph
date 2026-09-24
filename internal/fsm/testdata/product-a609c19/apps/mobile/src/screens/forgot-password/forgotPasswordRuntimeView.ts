import type { ForgotPasswordScreenStateId } from '../../stories/forgotPasswordScreenCatalog';
import {
  canSubmitNewPassword,
  canSubmitResetRequest,
  confirmPasswordHelperFor,
  passwordHelperFor,
  type ForgotPasswordRuntimeState,
} from './forgotPasswordRuntimeState';

export type ForgotPasswordRuntimeIntent =
  | { type: 'forgotPassword.goToLogin' }
  | { type: 'forgotPassword.openForgotPassword' }
  | { type: 'forgotPassword.submitEmail' }
  | { type: 'forgotPassword.resendEmail' }
  | { type: 'forgotPassword.openReset'; token: string }
  | { type: 'forgotPassword.submitNewPassword' }
  | { type: 'forgotPassword.backToLogin' };

export type ForgotPasswordRuntimeView = {
  stateId: ForgotPasswordScreenStateId;
  primaryIntent: ForgotPasswordRuntimeIntent | null;
  secondaryIntent: ForgotPasswordRuntimeIntent | null;
  secondaryDisabled?: boolean;
  backIntent: ForgotPasswordRuntimeIntent | null;
  textOverrides: Record<string, string>;
  inputHelpers: {
    newPassword?: string;
    confirmPassword?: string;
  };
  autoRedirect?: 'login';
};

export type ForgotPasswordSlotTarget = {
  componentId?: string | null;
  figmaNodeId: string;
  name: string;
  text?: string;
};

export function resolveForgotPasswordRuntimeView(state: ForgotPasswordRuntimeState): ForgotPasswordRuntimeView {
  switch (state.location) {
    case 'welcome':
      return view('welcome', { primaryIntent: { type: 'forgotPassword.goToLogin' } });
    case 'login':
      return view('login');
    case 'forgotPassword':
    case 'requestingReset':
      return view('forgot-password', {
        primaryIntent: canSubmitResetRequest(state) ? { type: 'forgotPassword.submitEmail' } : null,
        backIntent: { type: 'forgotPassword.backToLogin' },
      });
    case 'checkInbox': {
      const canResend = state.resendCooldownSeconds <= 0;
      return view('check-inbox', {
        primaryIntent: { type: 'forgotPassword.backToLogin' },
        secondaryIntent: canResend ? { type: 'forgotPassword.resendEmail' } : null,
        secondaryDisabled: !canResend,
        textOverrides: {
          '1070:21227': canResend ? 'Got it Resend link' : `Got it Resend link ${formatCooldown(state.resendCooldownSeconds)}`,
          '1070:21229': `Check your inbox If an account exists for ${state.email.trim()}, we’ll send a password reset link. If you don’t see it, check your spam folder.`,
        },
      });
    }
    case 'changePassword':
    case 'resettingPassword':
      return view('change-password', {
        primaryIntent: canSubmitNewPassword(state) ? { type: 'forgotPassword.submitNewPassword' } : null,
        backIntent: { type: 'forgotPassword.backToLogin' },
        inputHelpers: {
          newPassword: passwordHelperFor(state),
          confirmPassword: confirmPasswordHelperFor(state),
        },
      });
    case 'passwordChanged':
      return view('password-changed', { autoRedirect: 'login' });
  }
}

export function resolveForgotPasswordSlotIntent(
  viewState: ForgotPasswordRuntimeView,
  target: ForgotPasswordSlotTarget,
): ForgotPasswordRuntimeIntent | null {
  // Recovery login still authors "Forgot password?" at 2455:16122. Legacy
  // onboarding leaf 2696:25089 is kept only for honest short-copy BC; current
  // onboarding support link 7259:13364 is owned by FigmaSignInScreen / T-450
  // (Freshdesk), not this forgot-password opener.
  if (target.figmaNodeId === '2455:16122' || target.figmaNodeId === '2696:25089' || target.text === 'Forgot password?') {
    return { type: 'forgotPassword.openForgotPassword' };
  }

  if (target.componentId === '709:8523') return viewState.backIntent;
  if (target.componentId === '850:7906') return viewState.primaryIntent;
  if (target.componentId === '1663:135') {
    if (target.text?.includes('Resend link')) return viewState.secondaryIntent;
    return viewState.primaryIntent;
  }

  return null;
}

function view(
  stateId: ForgotPasswordScreenStateId,
  overrides: Partial<ForgotPasswordRuntimeView> = {},
): ForgotPasswordRuntimeView {
  return {
    stateId,
    primaryIntent: null,
    secondaryIntent: null,
    backIntent: null,
    textOverrides: {},
    inputHelpers: {},
    ...overrides,
  };
}

function formatCooldown(seconds: number) {
  const bounded = Math.max(0, Math.floor(seconds));
  return `00:${String(bounded).padStart(2, '0')}`;
}
