import type { ProductCancellationReason } from '@onyx/contracts';
import { Effect } from 'effect';

import type {
  MobileScanStatus,
  ProductAppState,
  ProductPasswordResetResponse,
  ProductProfileCancellationResponse,
  ProductPricingContent,
  ProtectFindingRevealResponse,
  ProductSession,
} from '../api';
import type {
  TrainRuntimeCatalog,
  TrainCourseDetail,
  TrainEnrollment,
  TrainEnrollmentProgress,
  TrainCommandResult,
  TrainLearningCommand,
  TrainQuizAttemptSubmission,
  TrainQuizAttemptResult,
  TrainMe,
} from '@onyx/contracts';
import type { ExtensionActivationProof } from './extensionActivationService';
import {
  CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MESSAGE,
  type PhoneSecurityProofSnapshot,
} from './phoneSecurityProofs';
import type { StoredProductSession } from '../session';
import type { MobileAnalyticsTrackInput } from '../analytics/tracker';
import type {
  AdvanceFirstSessionInput,
  CheckExtensionActivationInput,
  ClearSessionInput,
  FirstSessionScanPollInput,
  FirstSessionScanPollOutput,
  ForgotPasswordTimerInput,
  FreeScanTimerInput,
  GetStartedTimerInput,
  LoadAppStateInput,
  AddMonitoringEmailInput,
  RunFreeScanInput,
  LoginAccountInput,
  OnboardingTimerInput,
  PersistGetStartedQuizAnswerInput,
  PersistSessionInput,
  ProtectToastTimerInput,
  RegisterAccountInput,
  RequestPasswordResetInput,
  ResetPasswordInput,
  ResolveProtectTaskInput,
  ResolveProtectTaskOutput,
  SetBrowserProtectionAppEnabledInput,
  SetBrowserProtectionAppEnabledOutput,
  RunFirstSessionScanInput,
  RunFirstSessionScanOutput,
  RunScanInput,
  SendFamilyInvitesInput,
  SendFamilyInvitesOutput,
} from '../state/mobileAppMachine.types';
import type { MobileAppEffectRequest, MobileAppEffectResult } from '../behavior/mobileAppInterpreter';

/** Account deletion request — injectable port over POST /v1/profile/account-deletion-requests. */
export type RequestAccountDeletionInput = {
  reason?: string;
  confirmation: 'DELETE';
  sessionToken: string;
};

/** Subscription cancellation request — injectable port over POST /v1/profile/cancellation-requests. */
export type RequestSubscriptionCancellationInput = {
  reason: ProductCancellationReason;
  otherReason?: string;
  idempotencyKey?: string;
  sessionToken: string;
  onSuccess?: () => void;
};

/**
 * Discriminated cancellation outcome preserved across the mobile service port.
 * App-state refresh must not erase the billing result (D2 / 2A).
 * `response` is the canonical product-api cancellation contract.
 */
export type RequestSubscriptionCancellationResult = {
  /**
   * Refreshed app state after the request. May be null when the cancellation
   * response was received but the subsequent app-state refresh failed — the
   * billing outcome in `response` remains authoritative (D2).
   */
  appState: ProductAppState | null;
  response: ProductProfileCancellationResponse;
};

/** Cancellation handle handed to services whose in-flight state blocks the UI. */
export type MobileAppServiceCallOptions = {
  signal: AbortSignal;
};

export type MobileAppServices = {
  loadStoredSession: () => Promise<StoredProductSession | null>;
  checkProductHealth: () => Promise<{ status: string; service: string }>;
  loginAccount: (input: LoginAccountInput) => Promise<ProductSession>;
  exchangeAppsFlyerAutologin?: (token: string) => Promise<ProductSession>;
  registerAccount: (input: RegisterAccountInput) => Promise<ProductSession>;
  persistSession: (input: PersistSessionInput) => Promise<StoredProductSession>;
  loadAppState: (input: LoadAppStateInput) => Promise<ProductAppState>;
  /** Authenticated provider-agnostic display content used by Profile pricing slots. */
  loadPricingContent?: (input: { sessionToken: string }) => Promise<ProductPricingContent>;
  /**
   * Chapter checkpoint. `options.signal` is passed by the effect runner so the
   * bounded wait below is a real cancellation rather than a local give-up; an
   * adapter that reaches the network MUST forward it to its transport.
   */
  advanceFirstSession: (input: AdvanceFirstSessionInput, options?: MobileAppServiceCallOptions) => Promise<ProductAppState>;
  onboardingTimer: (input: OnboardingTimerInput) => Promise<OnboardingTimerInput>;
  protectToastTimer: (input: ProtectToastTimerInput) => Promise<ProtectToastTimerInput>;
  forgotPasswordTimer: (input: ForgotPasswordTimerInput) => Promise<ForgotPasswordTimerInput>;
  getStartedTimer: (input: GetStartedTimerInput) => Promise<GetStartedTimerInput>;
  persistGetStartedQuizAnswer: (input: PersistGetStartedQuizAnswerInput) => Promise<{ status: 'ok' }>;
  changePassword: (input: { password: string; sessionToken: string }) => Promise<ProductSession>;
  requestPasswordReset: (input: RequestPasswordResetInput) => Promise<ProductPasswordResetResponse>;
  resetPassword: (input: ResetPasswordInput) => Promise<ProductPasswordResetResponse>;
  checkExtensionActivation: (input: CheckExtensionActivationInput) => Promise<ExtensionActivationProof>;
  /**
   * Device proofs + optional backend sync. `options.signal` is forwarded so the
   * outer deadline really cancels the provider write (T-537), matching
   * advanceFirstSession.
   */
  checkPhoneSecurityProofs: (
    input: { sessionToken?: string; includeAppUpdateCheck?: boolean },
    options?: MobileAppServiceCallOptions,
  ) => Promise<PhoneSecurityProofSnapshot>;
  resolveProtectTask: (input: ResolveProtectTaskInput) => Promise<ProductAppState>;
  setBrowserProtectionAppEnabled: (input: SetBrowserProtectionAppEnabledInput) => Promise<ProductAppState>;
  addMonitoringEmail: (input: AddMonitoringEmailInput) => Promise<ProductAppState>;
  revealProtectFinding: (input: { findingId: string; sessionToken: string }) => Promise<ProtectFindingRevealResponse>;
  runScan: (input: RunScanInput) => Promise<MobileScanStatus>;
  runFirstSessionScan: (input: RunFirstSessionScanInput) => Promise<RunFirstSessionScanOutput>;
  firstSessionScanPoll: (input: FirstSessionScanPollInput) => Promise<FirstSessionScanPollOutput>;
  sendFamilyInvites: (input: SendFamilyInvitesInput) => Promise<SendFamilyInvitesOutput>;
  runFreeScan: (input: RunFreeScanInput) => Promise<ProductAppState>;
  freeScanTimer: (input: FreeScanTimerInput) => Promise<FreeScanTimerInput>;
  // Train runtime methods (§1.4 frozen names) — stay OUT of runMobileAppEffect by design
  loadTrainCatalog: (input: { sessionToken: string }) => Promise<TrainRuntimeCatalog>;
  /**
   * Loads a course and, when lessonId is provided, resolves that lesson's
   * remote media into the offline cache before returning it to the renderer.
   */
  loadTrainCourse: (input: { courseId: string; lessonId?: string; sessionToken: string }) => Promise<TrainCourseDetail>;
  startTrainCourse: (input: { courseId: string; idempotencyKey: string; sessionToken: string }) => Promise<TrainEnrollment>;
  loadTrainEnrollment: (input: {
    enrollmentId: string;
    sessionToken: string;
    consistency?: 'cache-first' | 'network-authoritative';
  }) => Promise<TrainEnrollmentProgress>;
  submitTrainCommand: (input: { enrollmentId: string; command: TrainLearningCommand; sessionToken: string }) => Promise<TrainCommandResult>;
  submitTrainQuizAttempt: (input: { enrollmentId: string; attempt: TrainQuizAttemptSubmission; sessionToken: string }) => Promise<TrainQuizAttemptResult>;
  loadTrainMe: (input: { sessionToken: string }) => Promise<TrainMe>;
  trackAnalytics: (input: MobileAnalyticsTrackInput) => Promise<void>;
  clearSession: (input: ClearSessionInput) => Promise<void>;
  requestAccountDeletion: (input: RequestAccountDeletionInput) => Promise<void>;
  requestSubscriptionCancellation: (
    input: RequestSubscriptionCancellationInput,
  ) => Promise<RequestSubscriptionCancellationResult>;
  /**
   * Domain port for external browser/App Store URLs (not raw Linking).
   * Protect change-password and update_onyx destinations use this.
   */
  openExternalUrl: (input: { url: string }) => Promise<void>;
  /**
   * Domain port for public OS settings destinations (app settings or Safari
   * extension settings). Unsupported private schemes stay fail-closed in the
   * Protect destination adapter before this port is called.
   */
  openSystemSettings: (input: {
    target: 'app' | 'safariExtensions';
  }) => Promise<'opened' | 'failed'>;
  /**
   * App foreground/background subscription. Composition-root only; excluded
   * from scenario Behavior map (sync unsubscribe contract).
   */
  observeAppLifecycle: (
    listener: (state: 'active' | 'background' | 'inactive' | 'unknown') => void,
  ) => () => void;
  /** Cold-start deep link URL, if any. */
  getInitialDeepLink: () => Promise<string | null>;
  /**
   * Ongoing deep-link URL subscription. Composition-root only; excluded from
   * scenario Behavior map.
   */
  observeDeepLinks: (listener: (url: string) => void) => () => void;
};

/** Effect-runner / scenario Behavior service names (exclude composition-root ports). */
export type RuntimeMobileAppServiceName = Exclude<
  keyof MobileAppServices,
  | 'loadPricingContent'
  | 'observeAppLifecycle'
  | 'getInitialDeepLink'
  | 'observeDeepLinks'
>;

export function runMobileAppEffect(
  services: MobileAppServices,
  request: MobileAppEffectRequest,
): Effect.Effect<MobileAppEffectResult> {
  switch (request.type) {
    case 'loadStoredSession':
      return toEffectResult(request.type, () => services.loadStoredSession());
    case 'checkProductHealth':
      return toEffectResult(request.type, () => services.checkProductHealth());
    case 'loginAccount':
      return toEffectResult(request.type, () => services.loginAccount(request.input));
    case 'exchangeAppsFlyerAutologin':
      return toEffectResult(request.type, () => {
        if (!services.exchangeAppsFlyerAutologin) throw new Error('appsflyer_autologin_unavailable');
        return services.exchangeAppsFlyerAutologin(request.input.token);
      });
    case 'registerAccount':
      return toEffectResult(request.type, () => services.registerAccount(request.input));
    case 'persistSession':
      return toEffectResult(request.type, () => services.persistSession(request.input));
    case 'loadAppState':
      return toEffectResult(request.type, () => services.loadAppState(request.input));
    case 'advanceFirstSession':
      return toEffectResult(request.type, () => withCancellableDeadline(
        (signal) => services.advanceFirstSession(request.input, { signal }),
        {
          deadlineMs: ADVANCE_FIRST_SESSION_TIMEOUT_MS,
          graceMs: ADVANCE_FIRST_SESSION_LATE_RESPONSE_GRACE_MS,
          message: ADVANCE_FIRST_SESSION_TIMEOUT_MESSAGE,
        },
      ));
    case 'onboardingTimer':
      return toEffectResult(request.type, () => services.onboardingTimer(request.input), request.input);
    case 'protectToastTimer':
      return toEffectResult(request.type, () => services.protectToastTimer(request.input));
    case 'forgotPasswordTimer':
      return toEffectResult(request.type, () => services.forgotPasswordTimer(request.input));
    case 'getStartedTimer':
      return toEffectResult(request.type, () => services.getStartedTimer(request.input));
    case 'persistGetStartedQuizAnswer':
      return toEffectResult(request.type, () => services.persistGetStartedQuizAnswer(request.input));
    case 'requestPasswordReset':
      return toEffectResult(request.type, () => services.requestPasswordReset(request.input));
    case 'resetPassword':
      return toEffectResult(request.type, () => services.resetPassword(request.input));
    case 'checkExtensionActivation':
      return toEffectResult(request.type, async () => ({
        trigger: request.input.trigger,
        intent: request.input.intent,
        proof: await services.checkExtensionActivation(request.input),
      }));
    case 'checkPhoneSecurityProofs':
      // Outer ceiling over probes + backend sync. A hanging LocalAuthentication
      // / App Store lookup / provider write used to leave proofs.status ===
      // 'checking' forever and pin the Protect spinner (T-537).
      return toEffectResult(request.type, () => withCancellableDeadline(
        (signal) => services.checkPhoneSecurityProofs(request.input, { signal }),
        {
          deadlineMs: CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MS,
          graceMs: CHECK_PHONE_SECURITY_PROOFS_LATE_RESPONSE_GRACE_MS,
          message: CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MESSAGE,
        },
      ));
    case 'resolveProtectTask':
      return toEffectResult(request.type, async (): Promise<ResolveProtectTaskOutput> => ({
        input: request.input,
        appState: await services.resolveProtectTask(request.input),
      }));
    case 'setBrowserProtectionAppEnabled':
      return toEffectResult(request.type, async (): Promise<SetBrowserProtectionAppEnabledOutput> => {
        return {
          input: request.input,
          appState: await services.setBrowserProtectionAppEnabled(request.input),
        };
      });
    case 'addMonitoringEmail':
      return toEffectResult(request.type, () => services.addMonitoringEmail(request.input));
    case 'runScan':
      return toEffectResult(request.type, () => services.runScan(request.input));
    case 'runFirstSessionScan':
      return toEffectResult(request.type, () => services.runFirstSessionScan(request.input));
    case 'firstSessionScanPoll':
      return toEffectResult(request.type, () => services.firstSessionScanPoll(request.input));
    case 'sendFamilyInvites':
      return toEffectResult(request.type, () => services.sendFamilyInvites(request.input));
    case 'runFreeScan':
      return toEffectResult(request.type, () => services.runFreeScan(request.input));
    case 'freeScanTimer':
      return toEffectResult(request.type, () => services.freeScanTimer(request.input));
    case 'analytics.track':
      return toVoidEffectResult(request.type, () => services.trackAnalytics({
        eventName: request.eventName,
        properties: request.properties,
      }));
    case 'clearSession':
      return toEffectResult(request.type, () => services.clearSession(request.input));
    case 'requestAccountDeletion':
      return toEffectResult(request.type, () => services.requestAccountDeletion(request.input));
    case 'requestSubscriptionCancellation':
      return toEffectResult(request.type, () => services.requestSubscriptionCancellation(request.input));
  }
}

/**
 * Deadline for the `advancing` chapter checkpoint (feedback D6). The machine
 * enters `advancing` with the DESTINATION screen already mounted and every
 * control inert, and it only leaves on this effect settling — so a hanging
 * request used to strand the user on a dead screen forever (RN `fetch` has no
 * default timeout).
 *
 * This is an ABORT deadline, not a stopwatch on the answer: at 15s the request
 * is really cancelled through its `AbortSignal`, so nothing is left racing the
 * recovery. Recovery is reconciliation, never a blind retry — the interpreter's
 * `advanceFirstSession` error path re-routes through `authenticatedLoading`,
 * which re-reads backend state and lands the user on whatever the server
 * actually committed. The endpoint itself is a compare-and-set on the expected
 * `firstSessionStep`, so even a commit that landed after we stopped listening
 * cannot double-advance the flow.
 */
export const ADVANCE_FIRST_SESSION_TIMEOUT_MS = 15_000;

/**
 * Grace granted to a response that was already on the wire when the deadline
 * fired. Cancelling is not the same as declaring failure: if the transport
 * still delivers a real answer inside this window we use it, because rewriting
 * a delivered commit into an error is exactly what reloads the machine over the
 * top of server state that already moved.
 */
export const ADVANCE_FIRST_SESSION_LATE_RESPONSE_GRACE_MS = 2_000;

/**
 * Hard ceiling on how long `advancing` can block the UI. A request that ignores
 * cancellation (or a mock that never settles) still fails here, so the D6 dead
 * screen cannot come back.
 */
export const ADVANCE_FIRST_SESSION_SETTLE_CEILING_MS =
  ADVANCE_FIRST_SESSION_TIMEOUT_MS + ADVANCE_FIRST_SESSION_LATE_RESPONSE_GRACE_MS;

export const ADVANCE_FIRST_SESSION_TIMEOUT_MESSAGE = 'First-session chapter checkpoint timed out.';

/**
 * Outer deadline for Protect / first-session phone-security proofs (T-537).
 * Covers local probes plus the authenticated provider sync that follows them.
 * On timeout the interpreter settles proofs to `unknown` / "Not available yet"
 * instead of leaving the Device spinner on `checking` forever.
 */
export const CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MS = 10_000;

/** Grace for a response already on the wire when the phone-security deadline fires. */
export const CHECK_PHONE_SECURITY_PROOFS_LATE_RESPONSE_GRACE_MS = 1_000;

export const CHECK_PHONE_SECURITY_PROOFS_SETTLE_CEILING_MS =
  CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MS + CHECK_PHONE_SECURITY_PROOFS_LATE_RESPONSE_GRACE_MS;

export { CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MESSAGE } from './phoneSecurityProofs';

/**
 * Run a request that blocks the UI under a real cancellation deadline.
 *
 * At `deadlineMs` the request is aborted at the transport. It then has
 * `graceMs` to settle for real — a value that still arrives is honoured, an
 * abort-driven rejection becomes `message`, and a request that ignores the
 * abort entirely is abandoned at the ceiling with `message`. The returned
 * promise settles exactly once; an abandoned request's later resolution is
 * dropped and can never reach the state machine as a second result.
 */
function withCancellableDeadline<T>(
  run: (signal: AbortSignal) => Promise<T>,
  { deadlineMs, graceMs, message }: { deadlineMs: number; graceMs: number; message: string },
): Promise<T> {
  const controller = new AbortController();
  return new Promise<T>((resolve, reject) => {
    let settled = false;
    let ceiling: ReturnType<typeof setTimeout> | undefined;
    const settle = (apply: () => void): void => {
      if (settled) return;
      settled = true;
      clearTimeout(deadline);
      if (ceiling !== undefined) clearTimeout(ceiling);
      apply();
    };
    const deadline = setTimeout(() => {
      controller.abort();
      ceiling = setTimeout(() => settle(() => reject(new Error(message))), graceMs);
    }, deadlineMs);

    run(controller.signal).then(
      (value) => settle(() => resolve(value)),
      (error: unknown) => settle(() => reject(
        // Once we have cancelled, the transport's own AbortError says nothing
        // useful; report the deadline so recovery reconciles rather than
        // surfacing a transport artefact.
        controller.signal.aborted
          ? new Error(message)
          : error instanceof Error ? error : new Error(String(error)),
      )),
    );
  });
}

function toEffectResult<TType extends MobileAppEffectRequest['type'], TOutput>(
  type: TType,
  run: () => Promise<TOutput>,
  input?: Extract<MobileAppEffectRequest, { input: unknown }>['input'],
): Effect.Effect<MobileAppEffectResult> {
  return Effect.tryPromise({
    try: run,
    catch: (error) => error,
  }).pipe(
    Effect.match({
      onFailure: (error) => ({ type, status: 'error', error, ...(input === undefined ? {} : { input }) }) as MobileAppEffectResult,
      onSuccess: (output) => ({ type, status: 'success', output }) as MobileAppEffectResult,
    }),
  );
}

function toVoidEffectResult<TType extends MobileAppEffectRequest['type']>(
  type: TType,
  run: () => Promise<void>,
): Effect.Effect<MobileAppEffectResult> {
  return Effect.tryPromise({
    try: run,
    catch: (error) => error,
  }).pipe(
    Effect.match({
      onFailure: (error) => ({ type, status: 'error', error }) as MobileAppEffectResult,
      onSuccess: () => ({ type, status: 'success' }) as MobileAppEffectResult,
    }),
  );
}
