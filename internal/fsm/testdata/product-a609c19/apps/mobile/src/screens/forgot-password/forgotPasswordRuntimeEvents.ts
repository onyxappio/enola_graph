import type { ForgotPasswordRuntimeIntent } from './forgotPasswordRuntimeView';

export function forgotPasswordIntentToEvent(intent: ForgotPasswordRuntimeIntent) {
  switch (intent.type) {
    case 'forgotPassword.goToLogin':
    case 'forgotPassword.backToLogin':
      return { type: 'FORGOT_PASSWORD_BACK_TO_LOGIN' } as const;
    case 'forgotPassword.openForgotPassword':
      return { type: 'FORGOT_PASSWORD_START' } as const;
    case 'forgotPassword.submitEmail':
      return { type: 'FORGOT_PASSWORD_SUBMIT_EMAIL' } as const;
    case 'forgotPassword.resendEmail':
      return { type: 'FORGOT_PASSWORD_RESEND_EMAIL' } as const;
    case 'forgotPassword.openReset':
      return { type: 'FORGOT_PASSWORD_OPEN_RESET', token: intent.token } as const;
    case 'forgotPassword.submitNewPassword':
      return { type: 'FORGOT_PASSWORD_SUBMIT_NEW_PASSWORD' } as const;
  }
}
