import type {
  MobileScanStatus,
  ProductAppState,
  ProductPasswordResetResponse,
  ProductSession,
} from '../api';
import { screenNameForState } from '../analytics/screenNames';
import type { MobileAnalyticsEventName, MobileAnalyticsProperties, MobileAnalyticsTrackInput } from '../analytics/tracker';
import type { StoredProductSession } from '../session';
import { formatMobileScanStatus } from '../utils';
import { getCurrentOpenId } from '../analytics/openCorrelation';
import { isAppsFlyerAutologinNetworkError } from '../appsflyerAutologin';
import {
  getOnboardingChapter,
  isFirstSessionComplete,
  type OnboardingChapter,
} from '../state/authorization';
import { firstSessionScanOutcomeFor, scanOutcomeCounts, scanOutcomeHasNoLeaksToShow } from '../state/firstSessionScanOutcome';
import { deviceHealthSnapshotForAppState } from '../state/deviceHealthSnapshot';
import type {
  AddMonitoringEmailInput,
  AdvanceFirstSessionInput,
  CheckExtensionActivationInput,
  SetBrowserProtectionAppEnabledInput,
  SetBrowserProtectionAppEnabledOutput,
  CheckExtensionActivationOutput,
  ClearSessionInput,
  FirstSessionScanPollInput,
  FirstSessionScanPollOutput,
  FirstSessionDeepScanRunState,
  ForgotPasswordTimerInput,
  GetStartedTimerInput,
  LoadAppStateInput,
  LoginAccountInput,
  MobileAppContext,
  MobileAppEvent,
  MobileAppInput,
  OnboardingChapterScreen,
  OnboardingHistoryEntry,
  OnboardingTimerInput,
  OnboardingTimerPhase,
  PersistGetStartedQuizAnswerInput,
  PersistSessionInput,
  ProtectToastTimerInput,
  RegisterAccountInput,
  RequestAccountDeletionInput,
  RequestPasswordResetInput,
  RequestSubscriptionCancellationInput,
  ResolveProtectTaskInput,
  ResolveProtectTaskOutput,
  ResetPasswordInput,
  FirstSessionScanRunStatus,
  RunFirstSessionScanInput,
  RunFirstSessionScanOutput,
  RunScanInput,
  SendFamilyInvitesInput,
  SendFamilyInvitesOutput,
  FreeScanTimerInput,
  RunFreeScanInput,
} from '../state/mobileAppMachine.types';
import { isTrainRuntimeActionEvent } from '../state/mobileAppMachine.types';
import type { ProductFirstSessionEvent } from '../api';
import { beginTrainRuntimeRefreshOwner, createTrainRuntimeSyncState, type TrainRuntimePendingDestination, type TrainRuntimeSyncState } from '../state/trainRuntimeSync';
import {
  projectTrainLocationForMachine,
  sameSemanticTrainLocation,
} from '../screens/train/trainRuntimeEvents';
import { isTrainActionLegalForLocation } from './trainActionLegality';
import {
  canSubmitNewPassword,
  canSubmitResetRequest,
  createForgotPasswordRuntimeState,
} from '../screens/forgot-password/forgotPasswordRuntimeState';
import type { ForgotPasswordRuntimeLocation } from '../screens/forgot-password/forgotPasswordRuntimeState';
import {
  applyQuizSelection,
  canSubmitGetStartedAccount,
  createGetStartedRuntimeState,
  getStartedQuizStepName,
  isSingleSelectLocation,
  nextQuizLocation,
} from '../screens/get-started/getStartedRuntimeState';
import type { GetStartedRuntimeLocation } from '../screens/get-started/getStartedRuntimeState';
import { canRunFreeScan } from '../screens/protect/free/protectFreeRuntimeState';
import {
  canSendFamilyInvites,
  setFamilyInviteMemberEmail,
  validFamilyInviteEmails,
} from '../screens/family-invite/familyInviteRuntimeState';
import {
  isProtectTaskSheetLocation,
  clearProtectToast,
  dismissProtectOverlay,
  enableProtectBrowserProtection,
  markProtectTaskOpened,
  openProtectBrowserSheet,
  protectBrowsingScreenFromRuntime,
  protectLeakDetectionLocation,
  protectMainLocation,
  protectTodoLocation,
  selectOpenProtectTasks,
  selectResolvedProtectTasks,
  setProtectBrowsingProtection,
} from '../state/protectRuntimeState';
import { rememberProtectTaskHandledId } from '../state/protectTaskHandledStorage';
import { rememberProtectTaskOpenedId } from '../state/protectTaskOpenedStorage';
import { sheetBackdropEntry } from '../components/figma/bottomSheet/sheetBackdropOrigin';
import type { ProtectRuntimeLocation, ProtectRuntimeMachineState, ProtectRuntimeTask } from '../state/protectRuntimeState';
import { deriveTaskDetailVariant } from '../state/protectTaskDetailVariant';
import {
  beginProtectTaskAction,
  resolveProtectTaskRecheckRefreshPolicy,
} from '../state/protectTaskActionLifecycle';
import { canonicalRiskStatusFromAppState } from '../state/canonicalRisk';
import { shouldShowRiskDropClaim } from '../state/riskTransition';
import {
  advanceDemonstrativeRisk,
  demonstrativeRiskStart,
  reconcileDemonstrativeRiskWithServer,
  type DemonstrativeRiskSection,
} from '../state/onboardingDemonstrativeRisk';
import { rootLocations, type CanonicalLocation, type ProtectedCanonicalLocation } from '../state/appLocations';
import {
  applyPhoneSecurityProofs,
  CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MESSAGE,
  markPhoneSecurityProofsChecking,
  preparePhoneSecurityProofsForRefresh,
  settlePhoneSecurityProofsAfterError,
  settlePhoneSecurityProofsAfterTimeout,
  type PhoneSecurityProofSnapshot,
} from '../effects/phoneSecurityProofs';
import * as update from '../state/mobileAppMachine.updates';
import { resolveExposureLevel } from '../screens/get-started/getStartedExposureModel';
import {
  FIRST_SESSION_ADVANCE_RECOVERY_LIMIT,
  FIRST_SESSION_PREDECESSOR_CONFLICT,
  firstSessionAdvanceReachedDestination,
  firstSessionSurfaceForMembership,
} from '@onyx/contracts';

function isUnauthorizedProductApiError(error: unknown) {
  if (error instanceof Error && /(?:status\s+401|unauthori[sz]ed|session expired)/i.test(error.message)) return true;
  return typeof error === 'object'
    && error !== null
    && 'status' in error
    && (error as { status?: unknown }).status === 401;
}

/**
 * Onboarding chapter runtime mirroring the approved Behavior Model:
 * welcome -> riskIntro -> deepScan -> scanResult -> leak -> spotFake ->
 * browser -> device -> postFlow, with an `advancing` state that persists each
 * backend chapter checkpoint. Intra-chapter screen progression for the tail is
 * tracked in `context.onboarding.chapterScreen`.
 */
export type FirstSessionStateValue =
  | 'route'
  | 'returningBridge'
  | 'inviteFamily'
  | 'inviteFamilySuccess'
  | 'welcome'
  | 'riskUnknownNewMember'
  | 'riskIntro'
  | 'deepScan'
  | 'scanResult'
  | 'leak'
  | 'spotFake'
  | 'browser'
  | 'device'
  | 'postFlow'
  | 'advancing';

export type MobileAppNestedStateValue =
  | { authenticating: 'login' | 'register' | 'persist' }
  | { forgotPassword: ForgotPasswordRuntimeLocation }
  | { getStarted: GetStartedRuntimeLocation }
  | { firstSession: FirstSessionStateValue }
  | { commandCenter: 'ready' | 'scanning' };

export type MobileAppStateValue =
  | 'booting'
  | 'checkingHealth'
  | 'unauthenticated'
  | 'authenticatedLoading'
  | 'visualReady'
  | 'clearingExpiredSession'
  | 'clearingSession'
  | 'degraded'
  | MobileAppNestedStateValue;

export type MobileAppStateMatcher =
  | Extract<MobileAppStateValue, string>
  | 'authenticating'
  | 'forgotPassword'
  | 'getStarted'
  | 'firstSession'
  | 'commandCenter'
  | MobileAppNestedStateValue;

export type MobileAppSnapshot = {
  value: MobileAppStateValue;
  context: MobileAppContext;
  matches: (matcher: MobileAppStateMatcher) => boolean;
};

export type MobileAppEffectRequest =
  | { type: 'loadStoredSession'; input: Record<string, never> }
  | { type: 'checkProductHealth'; input: Record<string, never> }
  | { type: 'exchangeAppsFlyerAutologin'; input: { token: string } }
  | { type: 'loginAccount'; input: LoginAccountInput }
  | { type: 'registerAccount'; input: RegisterAccountInput }
  | { type: 'persistSession'; input: PersistSessionInput }
  | { type: 'loadAppState'; input: LoadAppStateInput }
  | { type: 'advanceFirstSession'; input: AdvanceFirstSessionInput }
  | { type: 'onboardingTimer'; input: OnboardingTimerInput }
  | { type: 'protectToastTimer'; input: ProtectToastTimerInput }
  | { type: 'forgotPasswordTimer'; input: ForgotPasswordTimerInput }
  | { type: 'requestPasswordReset'; input: RequestPasswordResetInput }
  | { type: 'resetPassword'; input: ResetPasswordInput }
  | { type: 'getStartedTimer'; input: GetStartedTimerInput }
  | { type: 'persistGetStartedQuizAnswer'; input: PersistGetStartedQuizAnswerInput }
  | { type: 'checkExtensionActivation'; input: CheckExtensionActivationInput }
  | { type: 'setBrowserProtectionAppEnabled'; input: SetBrowserProtectionAppEnabledInput }
  | { type: 'checkPhoneSecurityProofs'; input: { sessionToken?: string; includeAppUpdateCheck?: boolean } }
  | { type: 'resolveProtectTask'; input: ResolveProtectTaskInput }
  | { type: 'addMonitoringEmail'; input: AddMonitoringEmailInput }
  | { type: 'runScan'; input: RunScanInput }
  | { type: 'runFirstSessionScan'; input: RunFirstSessionScanInput }
  | { type: 'firstSessionScanPoll'; input: FirstSessionScanPollInput }
  | { type: 'sendFamilyInvites'; input: SendFamilyInvitesInput }
  | { type: 'freeScanTimer'; input: FreeScanTimerInput }
  | { type: 'runFreeScan'; input: RunFreeScanInput }
  | { type: 'requestAccountDeletion'; input: RequestAccountDeletionInput }
  | { type: 'requestSubscriptionCancellation'; input: RequestSubscriptionCancellationInput }
  | MobileAnalyticsEffectRequest
  | { type: 'clearSession'; input: ClearSessionInput };

export type MobileAnalyticsEffectRequest<TEventName extends MobileAnalyticsEventName = MobileAnalyticsEventName> = {
  type: 'analytics.track';
  eventName: TEventName;
  properties: MobileAnalyticsProperties<TEventName>;
};

export type MobileAppEffectType = MobileAppEffectRequest['type'];

/**
 * Runtime vocabulary for anti-drift checks. Keep this beside the effect
 * request union so a newly added machine effect cannot silently disappear
 * from the Phase-9 census. This is metadata only; dispatch remains exhaustive
 * in `runMobileAppEffect`.
 */
export const MOBILE_APP_EFFECT_TYPES = [
  'loadStoredSession',
  'checkProductHealth',
  'exchangeAppsFlyerAutologin',
  'loginAccount',
  'registerAccount',
  'persistSession',
  'loadAppState',
  'advanceFirstSession',
  'onboardingTimer',
  'protectToastTimer',
  'forgotPasswordTimer',
  'requestPasswordReset',
  'resetPassword',
  'getStartedTimer',
  'persistGetStartedQuizAnswer',
  'checkExtensionActivation',
  'setBrowserProtectionAppEnabled',
  'checkPhoneSecurityProofs',
  'resolveProtectTask',
  'addMonitoringEmail',
  'runScan',
  'runFirstSessionScan',
  'firstSessionScanPoll',
  'sendFamilyInvites',
  'freeScanTimer',
  'runFreeScan',
  'requestAccountDeletion',
  'requestSubscriptionCancellation',
  'analytics.track',
  'clearSession',
] as const satisfies readonly MobileAppEffectType[];

/** Compile-time reverse coverage: every union member must appear above. */
export type MissingMobileAppEffectTypes = Exclude<MobileAppEffectType, (typeof MOBILE_APP_EFFECT_TYPES)[number]>;
export const MOBILE_APP_EFFECT_VOCABULARY_COMPLETE: MissingMobileAppEffectTypes extends never ? true : never = true;

type MobileAppEffectInput = Extract<MobileAppEffectRequest, { input: unknown }>['input'];

export type MobileAppEffectSuccess =
  | { type: 'loadStoredSession'; status: 'success'; output: StoredProductSession | null }
  | { type: 'checkProductHealth'; status: 'success'; output: { status: string; service: string } }
  | { type: 'exchangeAppsFlyerAutologin'; status: 'success'; output: ProductSession }
  | { type: 'loginAccount'; status: 'success'; output: ProductSession }
  | { type: 'registerAccount'; status: 'success'; output: ProductSession }
  | { type: 'persistSession'; status: 'success'; output: StoredProductSession }
  | { type: 'loadAppState'; status: 'success'; output: ProductAppState }
  | { type: 'advanceFirstSession'; status: 'success'; output: ProductAppState }
  | { type: 'onboardingTimer'; status: 'success'; output: OnboardingTimerInput }
  | { type: 'protectToastTimer'; status: 'success'; output: ProtectToastTimerInput }
  | { type: 'forgotPasswordTimer'; status: 'success'; output: ForgotPasswordTimerInput }
  | { type: 'requestPasswordReset'; status: 'success'; output: ProductPasswordResetResponse }
  | { type: 'resetPassword'; status: 'success'; output: ProductPasswordResetResponse }
  | { type: 'getStartedTimer'; status: 'success'; output: GetStartedTimerInput }
  | { type: 'persistGetStartedQuizAnswer'; status: 'success'; output: { status: 'ok' } }
  | { type: 'checkExtensionActivation'; status: 'success'; output: CheckExtensionActivationOutput }
  | { type: 'setBrowserProtectionAppEnabled'; status: 'success'; output: SetBrowserProtectionAppEnabledOutput }
  | { type: 'checkPhoneSecurityProofs'; status: 'success'; output: PhoneSecurityProofSnapshot }
  | { type: 'resolveProtectTask'; status: 'success'; output: ResolveProtectTaskOutput }
  | { type: 'addMonitoringEmail'; status: 'success'; output: ProductAppState }
  | { type: 'runScan'; status: 'success'; output: MobileScanStatus }
  | { type: 'runFirstSessionScan'; status: 'success'; output: RunFirstSessionScanOutput }
  | { type: 'firstSessionScanPoll'; status: 'success'; output: FirstSessionScanPollOutput }
  | { type: 'sendFamilyInvites'; status: 'success'; output: SendFamilyInvitesOutput }
  | { type: 'freeScanTimer'; status: 'success'; output: FreeScanTimerInput }
  | { type: 'runFreeScan'; status: 'success'; output: ProductAppState }
  | { type: 'requestAccountDeletion'; status: 'success'; output?: void }
  | { type: 'requestSubscriptionCancellation'; status: 'success'; output: ProductAppState }
  | { type: 'analytics.track'; status: 'success' }
  | { type: 'clearSession'; status: 'success'; output?: void };

export type MobileAppEffectFailure = {
  type: MobileAppEffectType;
  status: 'error';
  error: unknown;
  input?: MobileAppEffectInput;
};

export type MobileAppEffectResult = MobileAppEffectSuccess | MobileAppEffectFailure;

export type MobileAppTransitionResult = {
  snapshot: MobileAppSnapshot;
  effects: MobileAppEffectRequest[];
  accepted: boolean;
  trace: string[];
};

type ContextUpdate = Partial<MobileAppContext>;
type UpdateEvent = MobileAppEvent | { output: unknown } | { error: unknown };

export function createInitialMobileAppSnapshot(input: MobileAppInput): MobileAppSnapshot {
  const visual = input.visualMode ? input.visual ?? null : null;
  const visualMode = Boolean(visual);
  const visualScenario = input.initialVisualScenario ?? 'calm';
  const visualScan = visual ? visual.getScanStatus(visualScenario) : null;
  const visualAppState = visual ? visual.getAppState(visualScenario) : null;

  return snapshot('booting', {
    visualMode,
    visual,
    session: visual ? visual.session : null,
    appState: visualAppState,
    authMessage: visual ? 'Visual fixture session is active.' : update.defaultAuthMessage,
    authError: null,
    appStateMessage: visual ? 'Visual fixture mode is active.' : 'Loading local session...',
    authEmail: visual ? visual.email : '',
    authPassword: '',
    authRoute: 'signIn',
    fSessionId: null,
    pendingAppsFlyerAutologinRetry: null,
    pendingAppsFlyerOpenId: null,
    pendingWeb2AppLoginMethod: null,
    selectedTab: input.initialVisualTab ?? (visualMode ? 'protect' : 'profile'),
    trainLocation: { ...rootLocations.train },
    profileLocation: { ...rootLocations.profile },
    pendingReturnTo: null,
    visualScenario,
    pendingSession: null,
    scanSubjectType: 'password',
    scanSubject: 'password',
    scanStatus: visualScan,
    scanBusy: false,
    scanMessage: visualScan ? `Visual scan fixture: ${formatMobileScanStatus(visualScan)}.` : 'No scan submitted yet.',
    onboarding: update.onboardingStateFrom(visualAppState),
    protect: update.protectStateFrom(visualAppState),
    extensionActivationProof: null,
    forgotPassword: update.forgotPasswordStateFrom(),
    getStarted: update.getStartedStateFrom(input.initialGetStarted?.location, input.initialGetStarted?.answers),
    getStartedHistory: [...(input.initialGetStarted?.history ?? [])],
    getStartedForwardHistory: [],
    ...(input.initialGetStarted ? { getStartedEntry: input.initialGetStarted } : {}),
    familyInvite: update.familyInviteStateFrom(),
    protectFree: update.protectFreeStateFrom(visualAppState),
    lastScreenViewedName: null,
    trainRuntimeSync: createTrainRuntimeSyncState(),
  });
}

export function startMobileAppInterpreter(initialSnapshot: MobileAppSnapshot): MobileAppTransitionResult {
  return finalizeScreenView(enterState('booting', initialSnapshot.context, true, ['start']));
}

// screen_viewed is finalized at the three public entry points, not inside
// enterState: accepted transitions that change the resolved screen through
// stay()/raw result() (NAVIGATE tab switches, ONBOARDING_RUN_DEEP_SCAN, …)
// must be counted too, and lastScreenViewedName must never go stale on them.
export function sendMobileAppEvent(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  return finalizeScreenView(sendMobileAppEventTransition(snapshotValue, event));
}

function sendMobileAppEventTransition(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  if (event.type === 'BOOT' || event.type === 'RETRY') {
    // A user-initiated relaunch is a new recovery budget. Keep the in-flow
    // bound (one reconcile, then degrade) without making Retry a dead end.
    return enterState('booting', {
      ...snapshotValue.context,
      pendingAppsFlyerAutologinRetry: null,
      pendingAppsFlyerOpenId: null,
      pendingWeb2AppLoginMethod: null,
      onboarding: {
        ...snapshotValue.context.onboarding,
        pendingChapterEvent: null,
        firstSessionAdvanceRecoveryCount: 0,
      },
    }, true, [`event:${event.type}`]);
  }

  if (event.type === 'APPSFLYER_AUTOLOGIN_SESSION_READY') {
    // The AppsFlyer callback can arrive during any unauthenticated boot state.
    // A manually authenticated session always wins; the one-time server token
    // has already been consumed only after the caller persisted this session.
    if (snapshotValue.context.visualMode || snapshotValue.context.session || snapshotValue.matches('authenticating')) {
      return rejected(snapshotValue, event.type);
    }
    return withPrependedEffect(
      enterState('authenticatedLoading', {
        ...snapshotValue.context,
        session: event.session,
        appState: null,
        pendingSession: null,
        authPassword: '',
        authError: null,
        lastError: undefined,
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: null,
        pendingWeb2AppLoginMethod: null,
      }, true, [`event:${event.type}`]),
      webToAppAutologinProgress(snapshotValue.context, {
        stage: 'login_completed',
        login_method: 'autologin',
      }),
    );
  }

  if (event.type === 'APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED') {
    // AppsFlyer must enter the same auth/persist pipeline as manual login. A
    // manual flow or established session owns the storage and cannot be
    // overwritten by a late deferred-link callback.
    let correlatedOpenId: string | undefined;
    try {
      correlatedOpenId = getCurrentOpenId();
    } catch {
      correlatedOpenId = undefined;
    }
    const attemptOpenId = event.openId?.trim()
      || correlatedOpenId?.trim()
      || snapshotValue.context.pendingAppsFlyerOpenId;
    if (snapshotValue.context.visualMode || snapshotValue.context.session || snapshotValue.matches('authenticating')) {
      // Visual mode intentionally suppresses production analytics.
      if (snapshotValue.context.visualMode) {
        return rejected(snapshotValue, event.type);
      }
      const skipReason = snapshotValue.context.session
        ? 'existing_session' as const
        : 'manual_auth_in_progress' as const;
      return result(
        snapshotValue,
        [
          webToAppAutologinProgress(snapshotValue.context, {
            stage: 'skipped',
            skip_reason: skipReason,
            ...(attemptOpenId ? { open_id: attemptOpenId } : {}),
          }),
        ],
        true,
        [`rejected:${event.type}`],
      );
    }
    // The native SDK can deliver its cold-start callback before the async
    // secure-storage read settles. Stage that token while booting so an
    // existing stored session wins; only a confirmed missing session may
    // start the one-time exchange.
    if (snapshotValue.matches('booting')) {
      return stay(snapshotValue, {
        ...snapshotValue.context,
        authError: null,
        lastError: undefined,
        pendingAppsFlyerAutologinRetry: {
          token: event.token,
          retryAttempt: 0,
          ...(attemptOpenId ? { openId: attemptOpenId } : {}),
        },
        pendingAppsFlyerOpenId: attemptOpenId ?? null,
        pendingWeb2AppLoginMethod: 'autologin',
      }, `event:${event.type}:stagedDuringBoot`);
    }
    return enterState({ authenticating: 'login' }, {
      ...snapshotValue.context,
      authError: null,
      lastError: undefined,
      pendingAppsFlyerAutologinRetry: {
        token: event.token,
        retryAttempt: 0,
        ...(attemptOpenId ? { openId: attemptOpenId } : {}),
      },
      pendingAppsFlyerOpenId: attemptOpenId ?? null,
      pendingWeb2AppLoginMethod: 'autologin',
    }, true, [`event:${event.type}`], {
      effects: [{ type: 'exchangeAppsFlyerAutologin', input: { token: event.token } }],
    });
  }

  if (event.type === 'APPSFLYER_AUTOLOGIN_NETWORK_AVAILABLE') {
    const pending = snapshotValue.context.pendingAppsFlyerAutologinRetry;
    if (
      snapshotValue.context.visualMode
      || snapshotValue.context.session
      || snapshotValue.matches('authenticating')
      || !pending
      || pending.retryAttempt !== 0
    ) {
      return rejected(snapshotValue, event.type);
    }
    return enterState({ authenticating: 'login' }, {
      ...snapshotValue.context,
      authError: null,
      lastError: undefined,
      pendingAppsFlyerAutologinRetry: {
        token: pending.token,
        retryAttempt: 1,
        ...(pending.openId ? { openId: pending.openId } : {}),
      },
      pendingAppsFlyerOpenId: pending.openId ?? snapshotValue.context.pendingAppsFlyerOpenId,
      pendingWeb2AppLoginMethod: 'autologin',
    }, true, [`event:${event.type}`], {
      effects: [
        webToAppAutologinProgress(snapshotValue.context, {
          stage: 'retry_started',
          attempt: 'network_retry',
          ...(pending.openId ? { open_id: pending.openId } : {}),
        }),
        { type: 'exchangeAppsFlyerAutologin', input: { token: pending.token } },
      ],
    });
  }

  if (event.type === 'AUTH_EMAIL_CHANGED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignAuthEmail, event), `event:${event.type}`);
  }

  if (event.type === 'AUTH_PASSWORD_CHANGED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignAuthPassword, event), `event:${event.type}`);
  }

  if (event.type === 'FUNNEL_SESSION_RECEIVED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignFunnelSession, event), `event:${event.type}`);
  }

  if (event.type === 'NAVIGATE') {
    const intercepted = interceptTrainDeparture(
      snapshotValue,
      isProtectOrProfileTab(event.tab) ? { kind: 'navigate', tab: event.tab } : null,
    );
    if (intercepted) return intercepted;
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignNavigation, event), `event:${event.type}`);
  }

  if (event.type === 'ROUTE_REQUESTED') {
    const destTab = destinationTabFromLocation(event.location);
    const intercepted = destTab
      ? interceptTrainDeparture(snapshotValue, {
          kind: 'route',
          location: event.location,
          source: event.source,
          ...(event.returnTo === undefined ? {} : { returnTo: event.returnTo }),
        })
      : null;
    if (intercepted) return intercepted;
    return transitionRouteRequested(snapshotValue, event);
  }

  if (event.type === 'TRAIN_RUNTIME_MUTATION_STARTED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignTrainRuntimeMutationStarted, event), `event:${event.type}`);
  }

  if (event.type === 'TRAIN_RUNTIME_MUTATION_SETTLED') {
    return transitionTrainRuntimeMutationSettled(snapshotValue, event);
  }

  if (event.type === 'TRAIN_LOCATION_CHANGED') {
    return transitionTrainLocation(snapshotValue, event.location);
  }

  if (isTrainRuntimeActionEvent(event)) {
    return transitionTrainAction(snapshotValue, event);
  }

  if (event.type === 'SCAN_SUBJECT_TYPE_CHANGED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignScanSubjectType, event), `event:${event.type}`);
  }

  if (event.type === 'SCAN_SUBJECT_CHANGED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignScanSubject, event), `event:${event.type}`);
  }

  if (event.type === 'LOGOUT') {
    if (snapshotValue.matches('visualReady')) {
      return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignVisualLoggedOut, event), `event:${event.type}`);
    }

    return enterState('clearingSession', snapshotValue.context, true, [`event:${event.type}`]);
  }

  if (
    event.type === 'PROTECT_FREE_RUN_SCAN' ||
    event.type === 'PROTECT_FREE_SCAN_SETTLED' ||
    event.type === 'PROTECT_FREE_COOLDOWN_TICK'
  ) {
    return transitionProtectFree(snapshotValue, event);
  }

  if (snapshotValue.matches('visualReady')) {
    return transitionVisualReady(snapshotValue, event);
  }

  if (snapshotValue.matches('unauthenticated')) {
    return transitionUnauthenticated(snapshotValue, event);
  }

  if (snapshotValue.matches('forgotPassword')) {
    return transitionForgotPassword(snapshotValue, event);
  }

  if (snapshotValue.matches('getStarted')) {
    return transitionGetStarted(snapshotValue, event);
  }

  if (snapshotValue.matches('firstSession')) {
    return transitionFirstSession(snapshotValue, event);
  }

  if (snapshotValue.matches('commandCenter')) {
    return transitionCommandCenter(snapshotValue, event);
  }

  if (snapshotValue.matches('degraded') && event.type === 'REFRESH_APP_STATE') {
    return enterState('authenticatedLoading', snapshotValue.context, true, [`event:${event.type}`]);
  }

  return rejected(snapshotValue, event.type);
}

export function settleMobileAppEffect(snapshotValue: MobileAppSnapshot, result: MobileAppEffectResult): MobileAppTransitionResult {
  return finalizeScreenView(settleMobileAppEffectTransition(snapshotValue, result));
}

function settleMobileAppEffectTransition(snapshotValue: MobileAppSnapshot, result: MobileAppEffectResult): MobileAppTransitionResult {
  if (result.status === 'error') {
    return settleMobileAppEffectError(snapshotValue, result);
  }

  switch (result.type) {
    case 'loadStoredSession': {
      if (!snapshotValue.matches('booting')) return rejected(snapshotValue, `effect:${result.type}`);
      const output = result.output;
      if (output && !update.isStoredSessionExpired(output)) {
        const stagedAutologin = snapshotValue.context.pendingAppsFlyerAutologinRetry;
        const context = {
          ...applyUpdate(snapshotValue.context, update.assignStoredSession, { output }),
          // Existing stored session wins over a boot-staged AppsFlyer token.
          pendingAppsFlyerAutologinRetry: null,
          pendingAppsFlyerOpenId: null,
          pendingWeb2AppLoginMethod: null,
        };
        return withPrependedEffect(
          enterState('authenticatedLoading', context, true, [`effect:${result.type}:success`]),
          stagedAutologin
            ? webToAppAutologinProgress(snapshotValue.context, {
                stage: 'skipped',
                skip_reason: 'existing_session',
              })
            : null,
        );
      }
      if (output && update.isStoredSessionExpired(output)) {
        const context = applyUpdate(snapshotValue.context, update.assignExpiredSession, { output });
        return enterState('clearingExpiredSession', context, true, [`effect:${result.type}:expired`]);
      }
      const stagedAutologin = snapshotValue.context.pendingAppsFlyerAutologinRetry;
      const context = applyUpdate(snapshotValue.context, update.clearProtectedState, { output });
      if (stagedAutologin) {
        return enterState({ authenticating: 'login' }, {
          ...context,
          authError: null,
          lastError: undefined,
          pendingAppsFlyerAutologinRetry: stagedAutologin,
          pendingWeb2AppLoginMethod: 'autologin',
        }, true, [`effect:${result.type}:missing:stagedAutologin`], {
          effects: [{ type: 'exchangeAppsFlyerAutologin', input: { token: stagedAutologin.token } }],
        });
      }
      return enterState('checkingHealth', context, true, [`effect:${result.type}:missing`]);
    }

    case 'checkProductHealth': {
      if (!snapshotValue.matches('checkingHealth')) return rejected(snapshotValue, `effect:${result.type}`);
      const context = applyUpdate(snapshotValue.context, update.assignHealthMessage, { output: result.output });
      return enterState('unauthenticated', context, true, [`effect:${result.type}:success`]);
    }

    case 'exchangeAppsFlyerAutologin': {
      if (!snapshotValue.matches({ authenticating: 'login' })) return rejected(snapshotValue, `effect:${result.type}`);
      const attempt = snapshotValue.context.pendingAppsFlyerAutologinRetry?.retryAttempt === 1
        ? 'network_retry'
        : 'initial';
      // Keep pendingAppsFlyerOpenId through persist so login_completed /
      // persist_failed stay correlated to the originating open even if a warm
      // app_opened arrives during the async exchange/persist boundary.
      const context = {
        ...applyUpdate(snapshotValue.context, update.assignPendingSession, { output: result.output }),
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: snapshotValue.context.pendingAppsFlyerOpenId,
        pendingWeb2AppLoginMethod: 'autologin' as const,
      };
      return withPrependedEffect(
        enterState({ authenticating: 'persist' }, context, true, [`effect:${result.type}:success`]),
        webToAppAutologinProgress(snapshotValue.context, {
          stage: 'exchange_succeeded',
          attempt,
        }),
      );
    }

    case 'loginAccount': {
      if (!snapshotValue.matches({ authenticating: 'login' })) return rejected(snapshotValue, `effect:${result.type}`);
      const context = applyUpdate(snapshotValue.context, update.assignPendingSession, { output: result.output });
      return enterState({ authenticating: 'persist' }, context, true, [`effect:${result.type}:success`]);
    }

    case 'registerAccount': {
      if (snapshotValue.matches({ getStarted: 'creatingAccount' })) {
        const session = { ...result.output, savedAt: new Date().toISOString() };
        const context: MobileAppContext = {
          ...snapshotValue.context,
          session,
          pendingSession: null,
          authPassword: '',
          getStarted: { ...snapshotValue.context.getStarted, location: 'accountAllSet', message: null },
        };
        // Persist the fresh session so a get-started user who restarts the app
        // resumes authenticated instead of landing back on the login screen.
        return {
          snapshot: snapshot({ getStarted: 'accountAllSet' }, context),
          effects: [{ type: 'persistSession', input: { session } }],
          accepted: true,
          trace: [`effect:${result.type}:getStarted`],
        };
      }
      if (!snapshotValue.matches({ authenticating: 'register' })) return rejected(snapshotValue, `effect:${result.type}`);
      const context = applyUpdate(snapshotValue.context, update.assignPendingSession, { output: result.output });
      return enterState({ authenticating: 'persist' }, context, true, [`effect:${result.type}:success`]);
    }

    case 'persistSession': {
      if (snapshotValue.matches({ getStarted: 'accountAllSet' })) {
        const context = applyUpdate(snapshotValue.context, update.assignPersistedSession, { output: result.output });
        return { snapshot: snapshot({ getStarted: 'accountAllSet' }, context), effects: [], accepted: true, trace: [`effect:${result.type}:getStarted`] };
      }
      if (!snapshotValue.matches({ authenticating: 'persist' })) return rejected(snapshotValue, `effect:${result.type}`);
      const loginMethod = snapshotValue.context.pendingWeb2AppLoginMethod;
      const context = {
        ...applyUpdate(snapshotValue.context, update.assignPersistedSession, { output: result.output }),
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: null,
        pendingWeb2AppLoginMethod: null,
      };
      return withPrependedEffect(
        enterState('authenticatedLoading', context, true, [`effect:${result.type}:success`]),
        loginMethod
          ? webToAppAutologinProgress(snapshotValue.context, {
              stage: 'login_completed',
              login_method: loginMethod,
            })
          : null,
      );
    }

    case 'loadAppState': {
      if (snapshotValue.context.onboarding.pendingChapterRiskPersist) {
        const context = applyUpdate(snapshotValue.context, update.assignChapterOwnedRiskPersist, { output: result.output });
        return stay(snapshotValue, context, `effect:${result.type}:chapterRiskPersist`);
      }
      if (!snapshotValue.matches('authenticatedLoading')) return rejected(snapshotValue, `effect:${result.type}`);
      // Preserve the in-session cooldown epoch across app-state reload so a
      // re-arm can supersede any runFreeScan timer instead of colliding at gen 0.
      const previousCooldownGeneration = snapshotValue.context.protectFree.cooldownGeneration ?? 0;
      const loaded = restoreMonitoringEmailAddedAfterRefresh(
        applyUpdate(snapshotValue.context, update.assignLoadedAppState, { output: result.output }),
        snapshotValue.context,
      );
      const context = withProtectFree(loaded, {
        ...loaded.protectFree,
        cooldownGeneration: previousCooldownGeneration,
      });
      const refreshed = consumeTrainRuntimeRefresh(context);
      if (refreshed.reload) {
        return enterState('authenticatedLoading', refreshed.context, true, [`effect:${result.type}:trainRuntimeNewerCommit`]);
      }
      if (isFirstSessionComplete(result.output)) {
        // Route through enterState so a cold reload re-arms the same pending
        // work (extension activation probe, toast timer) as every live entry
        // into commandCenter:ready (N3 rehydration invariant), then resume the
        // server-persisted free-scan cooldown countdown (AW). Arming bumps the
        // cooldown generation so an in-session runFreeScan timer cannot run in
        // parallel after this load.
        const armed = armFreeScanCooldown(refreshed.context, refreshed.context.protectFree);
        const entered = enterState({ commandCenter: 'ready' }, armed.context, true, [`effect:${result.type}:complete`]);
        return { ...entered, effects: [...entered.effects, ...armed.effects] };
      }
      return enterState({ firstSession: 'route' }, refreshed.context, true, [`effect:${result.type}:incomplete`]);
    }

    case 'runFirstSessionScan': {
      if (!snapshotValue.matches({ firstSession: 'deepScan' })) {
        return rejected(snapshotValue, `effect:${result.type}`);
      }
      if (snapshotValue.context.onboarding.deepScanRun.status !== 'pending') {
        return rejected(snapshotValue, `effect:${result.type}:awaitingDeviceProof`);
      }
      const { appState, scanRun } = normalizeFirstSessionScanReplay(result.output);
      const replayed = applyUpdate(snapshotValue.context, update.assignFirstSessionScanReplay, { output: appState });
      const run = snapshotValue.context.onboarding.deepScanRun;
      const live = scanRun !== null && (scanRun.status === 'queued' || scanRun.status === 'running' || scanRun.status === 'waiting');
      if (live) {
        // A real backend scan run is in flight: poll it until terminal (or the
        // hard ceiling) — the loader now reflects the actual scan lifecycle.
        const context = withDeepScanRun(replayed, { status: 'polling', scanRunId: scanRun.scanRunId });
        return {
          snapshot: snapshot(snapshotValue.value, context),
          effects: [firstSessionScanPollEffect(context, scanRun.scanRunId)],
          accepted: true,
          trace: [`effect:${result.type}:polling`],
        };
      }
      // reused (fresh run already imported), failed, or the legacy plain
      // app-state contract: nothing live to wait on — the replayed findings
      // ARE this scan's findings, so the run is settled.
      //
      // SCAN ATTESTATION (T-209 / pass-5 CRITICAL 1): only a POSITIVE
      // result-bearing marker counts as a delivery. Absence of the marker is
      // not evidence — see `replayDeliveredResult`.
      const context = withDeepScanRun(replayed, {
        status: 'settled',
        scanRunId: scanRun?.scanRunId ?? null,
        attestation: replayDeliveredResult(scanRun) ? 'delivered' : 'attempted',
      });
      if (run.floorElapsed) {
        return advanceDeepScanCompleted(context, [`effect:${result.type}:settledAfterFloor`]);
      }
      return stay(snapshotValue, context, `effect:${result.type}:firstSession`);
    }

    case 'firstSessionScanPoll': {
      if (!snapshotValue.matches({ firstSession: 'deepScan' })) {
        return rejected(snapshotValue, `effect:${result.type}`);
      }
      const run = snapshotValue.context.onboarding.deepScanRun;
      if (run.status !== 'polling' || run.scanRunId !== result.output.scanRunId) {
        // Stale tick from a superseded run: the live chain is polled separately.
        return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:stale`);
      }
      const refreshed = result.output.appState
        ? applyUpdate(snapshotValue.context, update.assignFirstSessionScanReplay, { output: result.output.appState })
        : snapshotValue.context;
      if (TERMINAL_SCAN_RUN_STATES.has(result.output.state)) {
        // SCAN ATTESTATION (T-209 / pass-4 CRITICAL 2): `failed` / `expired`
        // are terminal but produced no result, so only the two completed states
        // license a clean claim downstream.
        const context = withDeepScanRun(refreshed, {
          status: 'settled',
          attestation: RESULT_BEARING_SCAN_RUN_STATES.has(result.output.state) ? 'delivered' : 'attempted',
        });
        if (run.floorElapsed) return advanceDeepScanCompleted(context, [`effect:${result.type}:terminal`]);
        // Terminal before the theatrical floor: dwell (G13) with the fresh
        // findings already in context; the timer settle will advance.
        return stay(snapshotValue, context, `effect:${result.type}:terminal`);
      }
      const pollsRemaining = run.pollsRemaining - 1;
      if (pollsRemaining <= 0) {
        // Hard ceiling: advance anyway with current findings (offline/slow
        // backend graceful path — never a frozen loader, never invented rows).
        const context = withDeepScanRun(refreshed, { status: 'settled', pollsRemaining: 0 });
        if (run.floorElapsed) return advanceDeepScanCompleted(context, [`effect:${result.type}:ceiling`]);
        return stay(snapshotValue, context, `effect:${result.type}:ceiling`);
      }
      const context = withDeepScanRun(refreshed, { pollsRemaining });
      return {
        snapshot: snapshot(snapshotValue.value, context),
        effects: [firstSessionScanPollEffect(context, result.output.scanRunId)],
        accepted: true,
        trace: [`effect:${result.type}:tick`],
      };
    }

    case 'advanceFirstSession': {
      if (!snapshotValue.matches({ firstSession: 'advancing' })) return rejected(snapshotValue, `effect:${result.type}`);
      const pending = snapshotValue.context.onboarding.pendingChapterEvent;
      if (!pending || !firstSessionAdvanceReachedDestination(result.output.firstSessionStep, pending)) {
        return recoverFirstSessionAdvance(
          snapshotValue,
          new Error(FIRST_SESSION_PREDECESSOR_CONFLICT),
          `effect:${result.type}:destinationMismatch`,
        );
      }
      const context = applyUpdate(snapshotValue.context, update.assignFirstSessionAdvanced, { output: result.output });
      // The final checkpoint (onboarding_completed) hands off to the app shell.
      if (isFirstSessionComplete(result.output)) {
        return enterState({ commandCenter: 'ready' }, context, true, [`effect:${result.type}:complete`]);
      }
      return enterState({ firstSession: 'route' }, context, true, [`effect:${result.type}:success`]);
    }

    case 'onboardingTimer': {
      return settleOnboardingTimerElapsed(snapshotValue, result.output.phase, result.type, result.output.generation);
    }

    case 'protectToastTimer': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}`);
      // Disarm first: the `ready` entry below re-arms only when the advanced
      // state still needs a timer (beat → resolved → origin).
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, advanceProtectToast(disarmProtectToastTimer(snapshotValue.context.protect))), true, [`effect:${result.type}:elapsed`]);
    }

    case 'requestPasswordReset': {
      if (!snapshotValue.matches({ forgotPassword: 'requestingReset' })) return rejected(snapshotValue, `effect:${result.type}`);
      const context = withForgotPassword(snapshotValue.context, {
        ...snapshotValue.context.forgotPassword,
        location: 'checkInbox',
        resendCooldownSeconds: 30,
        devResetToken: result.output.devResetToken ?? null,
        message: null,
      });
      return enterState({ forgotPassword: 'checkInbox' }, context, true, [`effect:${result.type}:success`]);
    }

    case 'resetPassword': {
      if (!snapshotValue.matches({ forgotPassword: 'resettingPassword' })) return rejected(snapshotValue, `effect:${result.type}`);
      const context = withForgotPassword(snapshotValue.context, {
        ...snapshotValue.context.forgotPassword,
        location: 'passwordChanged',
        message: null,
      });
      return enterState({ forgotPassword: 'passwordChanged' }, context, true, [`effect:${result.type}:success`]);
    }

    case 'forgotPasswordTimer': {
      if (!snapshotValue.matches('forgotPassword')) return rejected(snapshotValue, `effect:${result.type}`);
      if (result.output.phase === 'resendCooldown') {
        // A stale tick after the user left CheckInbox must not teleport them back.
        if (snapshotValue.context.forgotPassword.location !== 'checkInbox') {
          return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:staleResendCooldown`);
        }
        const context = withForgotPassword(snapshotValue.context, {
          ...snapshotValue.context.forgotPassword,
          resendCooldownSeconds: Math.max(0, result.output.secondsRemaining),
        });
        // Re-entering CheckInbox arms the next 1s tick until the countdown drains.
        return enterState({ forgotPassword: 'checkInbox' }, context, true, [`effect:${result.type}:resendCooldown`]);
      }

      const context: MobileAppContext = {
        ...snapshotValue.context,
        authPassword: '',
        authMessage: 'Password changed. Log in with your new password.',
        forgotPassword: createForgotPasswordRuntimeState({ email: snapshotValue.context.forgotPassword.email }),
      };
      return enterState('unauthenticated', context, true, [`effect:${result.type}:successRedirect`]);
    }

    case 'getStartedTimer': {
      if (!snapshotValue.matches('getStarted')) return rejected(snapshotValue, `effect:${result.type}`);
      const location = snapshotValue.context.getStarted.location;
      if (result.output.phase === 'selectionDwell') {
        return settleGetStartedSelectionDwell(snapshotValue, result.output, `effect:${result.type}:selectionDwell`);
      }
      if (result.output.phase === 'analyzing') {
        // Only settle the analyzing dwell if the user is STILL on analyzing.
        // Nothing cancels an armed timer, so without this guard a Back out of
        // the loader (T-233: the owner drew a back chevron on 2421:486) is
        // yanked forward to `result` seconds later, from whatever question she
        // returned to. The error path below has always guarded on location;
        // the success path did not, which is why `analyzing` could not be given
        // a back intent before.
        if (location !== 'analyzing') return rejected(snapshotValue, `effect:${result.type}:analyzingAbandoned`);
        const context = withGetStarted(snapshotValue.context, {
          ...snapshotValue.context.getStarted,
          location: 'result',
          message: null,
        });
        return enterState({ getStarted: 'result' }, context, true, [`effect:${result.type}:analyzing`]);
      }
      // phase 'scan': the 2s theatrical floor elapsed. When the floor beats the
      // scan data, only the machine VALUE moves to scanResult (dwell satisfied)
      // while the rendered location holds on the scanning beat until
      // runFreeScan settles with real findings — never show a result screen
      // built from stale app-state.
      if (location === 'scan') {
        if (snapshotValue.matches({ getStarted: 'scan' })) {
          return { snapshot: snapshot({ getStarted: 'scanResult' }, snapshotValue.context), effects: [], accepted: true, trace: [`effect:${result.type}:scanFloor`] };
        }
        return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:scanFloorRepeat`);
      }
      if (location === 'scanResult') {
        const context = withGetStarted(snapshotValue.context, {
          ...snapshotValue.context.getStarted,
          location: 'scanResult',
          message: null,
        });
        return enterState({ getStarted: 'scanResult' }, context, true, [`effect:${result.type}:scan`]);
      }
      // Stale scan timer after the user already advanced past the result.
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:stale`);
    }

    case 'persistGetStartedQuizAnswer': {
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:success`);
    }

    case 'checkExtensionActivation': {
      const contextWithProof = withExtensionActivationProof(snapshotValue.context, result.output.proof);
      if (result.output.trigger === 'protect') {
        if (!snapshotValue.matches({ commandCenter: 'ready' })) return rejected(snapshotValue, `effect:${result.type}:protect`);
        const protect = contextWithProof.protect;
        if (result.output.proof.state !== 'active') {
          // A turn-on request is decided by this fresh system-level proof. If
          // Safari is still inactive, show the existing Settings sheet only
          // after the proof settles; never infer inactivity from the combined
          // app-level/effective state cached in appState.
          if (result.output.intent === 'turnOn') {
            // T-385: the sheet is an overlay owned by the shell, so it can open
            // over whatever screen raised the turn-on. Teleporting to the
            // Protect tab here both lost the screen the user was reading and
            // left a `browsing:off` entry on the stack that Back would replay
            // long after protection came back on. Stay put and let the sheet
            // resolve on top of the caller's screen.
            const sheetRuntime = openProtectBrowserSheet(protect.runtime);
            return enterState({ commandCenter: 'ready' }, withProtect({
              ...contextWithProof,
              appStateMessage: extensionProofMessage(result.output.proof),
            }, {
              ...protect,
              runtime: sheetRuntime,
            }), true, [`effect:${result.type}:turnOnNeedsSettings`]);
          }

          // Feedback ("Enable protection does nothing"): a non-active proof must
          // produce visible feedback on the Protect surface, not just a context
          // message nothing renders. When the user asked from the Browser
          // Protection sheet (overlay open), surface a toast (enterState arms
          // the protectToastTimer via shouldRunProtectToastTimer) and keep the
          // sheet open for a retry. The background probe (overlay closed) stays
          // silent so landing on Protect never toasts unprompted.
          if (protect.runtime.overlay !== 'browserSheet') {
            return stay(snapshotValue, {
              ...contextWithProof,
              appStateMessage: extensionProofMessage(result.output.proof),
            }, `effect:${result.type}:${result.output.proof.state}`);
          }
          return enterState({ commandCenter: 'ready' }, withProtect({
            ...contextWithProof,
            appStateMessage: extensionProofMessage(result.output.proof),
          }, {
            ...protect,
            runtime: { ...protect.runtime, toast: 'browserProtectionNotDetected' },
          }), true, [`effect:${result.type}:${result.output.proof.state}`]);
        }

        // A native proof refresh only describes the system level. It must not
        // resurrect a preference the product state has intentionally disabled,
        // must not announce a state nobody asked for, and must not navigate.
        // Explicit activation from a turn-on CTA is the only path allowed to
        // turn the app level back on.
        //
        // T-386 / owner (2026-08-20): «Захист вмикається ТІЛЬКИ після явного
        // натискання Увімкнути protection».
        //
        // Two things went wrong here. Gating on `appState` alone left the
        // defect alive, because the in-app turn-off persists asynchronously:
        // for the whole round-trip (longer when the write is slow, retried, or
        // offline) appState still reports the *old* preference. A probe
        // landing in that window found the Safari extension still active — the
        // user had switched Onyx off in the app and never visited Settings —
        // and turned protection back on, announcing "Browsing protection is
        // on" for someone who had asked for nothing (owner frame 983). And
        // when the fence did fire it sent the user to the Protect tab, taking
        // away the screen they were reading: confirming the turn-off landed on
        // the Protect overview instead of the orange "Turn on browsing
        // protection" screen the owner explicitly calls correct.
        if (result.output.intent === 'background') {
          const appPreferenceOff = contextWithProof.appState?.extensionSync.appLevelEnabled === false;
          return stay(snapshotValue, withProtect({
            ...contextWithProof,
            appStateMessage: 'Safari extension activation proof confirmed.',
          }, {
            ...protect,
            runtime: appPreferenceOff
              ? setProtectBrowsingProtection(protect.runtime, 'off')
              : protect.runtime,
          }), `effect:${result.type}:protectBackgroundProofOnly`);
        }

        const enabledRuntime = enableProtectBrowserProtection(protect.runtime, result.output.proof);
        // T-449 (owner, TestFlight 175: «зелений екран зʼявляється лагануто, з
        // затримкою»): a turn-on raised from the orange screen no longer paints
        // the green dashboard while the preference round-trip is in flight. The
        // screen stays, its CTA shows loading, and the settle lands on the
        // Risk-dropped beat (proven level drop) or the dashboard — one move.
        const holdsScreenForSettle = protect.location.name === 'browsing' && protect.location.screen === 'off';
        const nextRuntime = holdsScreenForSettle
          ? { ...enabledRuntime, browsing: { ...enabledRuntime.browsing, activationPending: true }, toast: null }
          : enabledRuntime;
        const context = withProtect({
          ...contextWithProof,
          appStateMessage: 'Safari extension activation proof confirmed.',
        }, {
          ...protect,
          runtime: nextRuntime,
          location: holdsScreenForSettle ? protect.location : protectLocationAfterBrowserEnable(protect, nextRuntime),
        });
        const preferenceEffect = createBrowserProtectionPreferenceEffect(context, true);
        return {
          snapshot: snapshot({ commandCenter: 'ready' }, context),
          effects: [
            analyticsTrackEffect('browser_protection_enabled', {}),
            ...(preferenceEffect ? [preferenceEffect] : []),
          ],
          accepted: true,
          trace: [`effect:${result.type}:protectActivated`],
        };
      }

      // Browser chapter activation poll (§8.2). Meaningful on the setup screen
      // (manual/poll re-check) and on the intro/nudge screens (foreground
      // auto-detect); an active proof flips to the success screen, otherwise we
      // hold on the current screen until the user acts or activation is
      // detected.
      if (!snapshotValue.matches({ firstSession: 'browser' })) return rejected(snapshotValue, `effect:${result.type}`);
      const browserScreen = snapshotValue.context.onboarding.chapterScreen;
      if (browserScreen !== 'extensionSetup' && browserScreen !== 'extensionIntro' && browserScreen !== 'extensionNudge') {
        return rejected(snapshotValue, `effect:${result.type}:screen`);
      }
      if (result.output.proof.state === 'active') {
        const sessionToken = contextWithProof.session?.sessionToken;
        const context = sessionToken
          ? {
              ...contextWithProof,
              onboarding: {
                ...contextWithProof.onboarding,
                pendingChapterRiskPersist: true,
              },
            }
          : contextWithProof;
        return withPrependedEffects(
          enterState({ firstSession: 'browser' }, withChapterScreen(context, 'extensionOn'), true, [`effect:${result.type}:activated`]),
          [
            ...(sessionToken ? [{ type: 'loadAppState' as const, input: { sessionToken } }] : []),
            analyticsTrackEffect('browser_protection_enabled', {}),
          ],
        );
      }
      return { snapshot: snapshot(snapshotValue.value, contextWithProof), effects: [], accepted: true, trace: [`effect:${result.type}:${result.output.proof.state}`] };
    }

    case 'setBrowserProtectionAppEnabled': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}`);
      const nextProtect = update.withProtectRiskTrend(snapshotValue.context.protect, update.preserveProtectDrillInAcrossAppStateRefresh(
        snapshotValue.context.protect,
        update.protectStateFrom(result.output.appState),
      ));
      const enabled = result.output.input.enabled;
      const enabledToast = enabled ? ('browserProtectionEnabled' as const) : null;
      // T-449 (owner, 2026-09-04, TestFlight 174: «не показався зелений екран
      // пониження ризику коли перейшла з Medium на Low» after turning the
      // extension on). The activation's risk drop arrives with THIS app-state
      // projection, not with a task-resolution settle, so the Risk-dropped
      // beat has to be claimed here: two ordered server projections (before /
      // after enabling) proving a LEVEL change — same rule as every other
      // Protect resolution. The beat then hands off to the outcome toast and
      // returns to the overview; without a proven drop nothing changes.
      const fromStatus = canonicalRiskStatusFromAppState(snapshotValue.context.appState);
      const toStatus = canonicalRiskStatusFromAppState(result.output.appState);
      const fromEndpoint = fromStatus
        ? {
          status: fromStatus,
          handlePosition: snapshotValue.context.protect.runtime.canonicalHandlePosition,
          score: snapshotValue.context.protect.runtime.canonicalRiskScore ?? snapshotValue.context.appState?.protect.risk.score,
        }
        : null;
      const toEndpoint = toStatus
        ? {
          status: toStatus,
          handlePosition: nextProtect.runtime.canonicalHandlePosition,
          score: nextProtect.runtime.canonicalRiskScore ?? result.output.appState.protect.risk.score,
        }
        : null;
      const provenDrop = enabled && fromEndpoint != null && toEndpoint != null && shouldShowRiskDropClaim(fromEndpoint, toEndpoint);
      const beatOnScreen = nextProtect.location.name === 'taskResolution' && nextProtect.location.screen === 'riskDropped';
      const context = {
        ...snapshotValue.context,
        appState: result.output.appState,
        appStateMessage: enabled
          ? 'Browsing protection is on.'
          : 'Browsing protection is off.',
        protect: {
          ...nextProtect,
          runtime: {
            ...nextProtect.runtime,
            // A repeated settle while the beat is already on screen must not
            // play the toast under the scene; the hand-off owns it.
            toast: provenDrop || beatOnScreen ? null : enabledToast,
            provenRiskDrop: provenDrop && fromEndpoint && toEndpoint
              ? {
                from: fromEndpoint.status,
                to: toEndpoint.status,
                fromHandle: fromEndpoint.handlePosition,
                toHandle: toEndpoint.handlePosition,
              }
              : nextProtect.runtime.provenRiskDrop,
            riskDropReturnScreen: provenDrop ? ('resolved' as const) : nextProtect.runtime.riskDropReturnScreen,
            riskDropReturnToast: provenDrop ? enabledToast : nextProtect.runtime.riskDropReturnToast,
            resolutionReturnLocation: provenDrop ? protectMainLocation(nextProtect.runtime) : nextProtect.runtime.resolutionReturnLocation,
          },
          // The beat renders over the surface it RETURNS to: with the overview
          // in history the Risk-dropped scene and the outcome toast share one
          // underlay (an empty history fell back to To-Do under the beat, and
          // the underlay switch on hand-off lost the toast — journey
          // browsing-back-after-turn-off). Landing on main consumes the entry.
          history: provenDrop ? [protectMainLocation(nextProtect.runtime)] : nextProtect.history,
          location: provenDrop ? { name: 'taskResolution' as const, screen: 'riskDropped' as const } : nextProtect.location,
        },
      };
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, context, true, [`effect:${result.type}:success`]),
        result.output.input.enabled ? analyticsTrackEffect('browser_protection_enabled', {}) : null,
      );
    }

    case 'checkPhoneSecurityProofs': {
      if (snapshotValue.matches({ firstSession: 'deepScan' })) {
        const loaded = result.output.appState
          ? applyUpdate(snapshotValue.context, update.assignFirstSessionScanReplay, { output: result.output.appState })
          : snapshotValue.context;
        return startFirstSessionScanAfterDeviceProof(
          snapshotValue.value,
          loaded,
          `effect:${result.type}:deepScanSettled`,
        );
      }
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}`);
      const protect = snapshotValue.context.protect;
      const syncedProtect = result.output.appState
        ? update.withProtectRiskTrend(protect, update.protectStateFrom(result.output.appState))
        : protect;
      const runtime = applyPhoneSecurityProofs(syncedProtect.runtime, result.output);
      const context = result.output.appState
        ? { ...snapshotValue.context, appState: result.output.appState }
        : snapshotValue.context;
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, withProtect(context, {
          ...syncedProtect,
          runtime,
          location: protect.location,
          history: protect.history,
        }), true, [`effect:${result.type}:settled`]),
        phoneSecurityCompletedAnalytics(result.output),
      );
    }

    case 'resolveProtectTask': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}`);
      const context = reconcileProtectTaskResolution(snapshotValue.context, result.output);
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, context, true, [`effect:${result.type}:success`]),
        protectTaskResolvedAnalyticsEffect(snapshotValue.context, result.output.input),
      );
    }

    case 'addMonitoringEmail': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}`);
      const context = reconcileMonitoringEmailAdded(snapshotValue.context, result.output);
      // The add response mutates the backend projection, but the mounted list
      // must be proved from a subsequent app-state read. Keep the post-write
      // route in context while the exact refresh effect settles.
      return enterState('authenticatedLoading', context, true, [`effect:${result.type}:success`]);
    }

    case 'runScan': {
      if (!snapshotValue.matches({ commandCenter: 'scanning' })) return rejected(snapshotValue, `effect:${result.type}`);
      const context = applyUpdate(snapshotValue.context, update.assignScanSuccess, { output: result.output });
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, context, true, [`effect:${result.type}:success`]),
        scanViewedAnalyticsEffect(result.output.scanRunId),
      );
    }

    case 'sendFamilyInvites': {
      // Invites persisted (§ family-owner-first-session). Mark the step handled
      // and show the InviteFamilySuccess confirmation (owner 2026-08-07). The
      // screen authors its own StickyButton, so NO timer is armed here — only
      // its Continue (FAMILY_INVITE_SUCCESS_CONTINUE) resumes the regular
      // chapters. Deferred invites never pass through here and keep their
      // direct route (see FAMILY_INVITE_DEFER).
      if (!snapshotValue.matches({ firstSession: 'inviteFamily' })) return rejected(snapshotValue, `effect:${result.type}`);
      return enterState({ firstSession: 'inviteFamilySuccess' }, markFamilyInvitesHandled(snapshotValue.context), true, [`effect:${result.type}:success`]);
    }

    case 'runFreeScan': {
      // Free leaks-only scan completed: adopt materialized app-state (findings +
      // server cooldown) and start draining the countdown.
      const appState = result.output;
      const loadedFree = update.protectFreeStateFrom(appState);
      const armed = armFreeScanCooldown(
        {
          ...snapshotValue.context,
          appState,
          // The refreshed protect state must not wipe the drill-in back stack:
          // rebuilding with history [] leaves every back chevron rejected (G5).
          protect: {
            ...update.withProtectRiskTrend(snapshotValue.context.protect, update.protectStateFrom(appState)),
            history: snapshotValue.context.protect.history,
          },
        },
        loadedFree,
      );
      const context = armed.context;
      const effects = armed.effects;
      // GetStarted scan beat: the rendered location holds on 'scan' until the
      // data lands here, so this settle is what shows the result (the 2s scan
      // timer may already have flipped the machine value — see getStartedTimer).
      if (snapshotValue.matches('getStarted') && snapshotValue.context.getStarted.location === 'scan') {
        return {
          snapshot: snapshot({ getStarted: 'scanResult' }, {
            ...context,
            getStarted: { ...context.getStarted, location: 'scanResult', message: null },
          }),
          effects: [scanViewedAnalyticsEffect(null), ...effects],
          accepted: true,
          trace: [`effect:${result.type}:getStarted`],
        };
      }
      // Paid-tab leak re-scan (PROTECT_RUN_LEAK_SCAN): the refreshed protect
      // state resets the location to the overview by construction, so restore
      // the Leak detection focus the user is looking at and confirm with a
      // toast instead of silently teleporting them.
      const previousLocation = snapshotValue.context.protect.location;
      if (previousLocation.name === 'leakDetection' || previousLocation.name === 'monitoring') {
        const refreshedProtect = context.protect;
        const protectContext = withProtect(context, {
          ...refreshedProtect,
          runtime: { ...refreshedProtect.runtime, toast: 'leakScanCompleted', toastTimerArmed: true },
          location: previousLocation.name === 'monitoring'
            ? previousLocation
            : protectLeakDetectionLocation(refreshedProtect.runtime),
        });
        return {
          snapshot: snapshot(snapshotValue.value, protectContext),
          effects: [scanViewedAnalyticsEffect(null), ...effects, { type: 'protectToastTimer', input: {} }],
          accepted: true,
          trace: [`effect:${result.type}:leakDetection`],
        };
      }
      return {
        snapshot: snapshot(snapshotValue.value, context),
        effects: [scanViewedAnalyticsEffect(null), ...effects],
        accepted: true,
        trace: [`effect:${result.type}:success`],
      };
    }

    case 'freeScanTimer': {
      const currentGeneration = snapshotValue.context.protectFree.cooldownGeneration ?? 0;
      const tickGeneration = result.output.generation;
      // Superseded arms (runFreeScan + loadAppState) must not re-arm in parallel.
      // Missing generation is treated as current (legacy settle paths / pure unit ticks).
      if (tickGeneration !== undefined && tickGeneration !== currentGeneration) {
        return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:stale`);
      }
      const secondsRemaining = Math.max(0, result.output.secondsRemaining);
      const context = withProtectFree(snapshotValue.context, {
        ...snapshotValue.context.protectFree,
        cooldownSecondsRemaining: secondsRemaining,
      });
      const effects: MobileAppEffectRequest[] =
        secondsRemaining > 0
          ? [{ type: 'freeScanTimer', input: { secondsRemaining, generation: currentGeneration } }]
          : [];
      return {
        snapshot: snapshot(snapshotValue.value, context),
        effects,
        accepted: true,
        trace: [`effect:${result.type}:tick`],
      };
    }

    case 'analytics.track': {
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:success`);
    }

    case 'clearSession': {
      if (snapshotValue.matches('clearingExpiredSession')) {
        const stagedAutologin = snapshotValue.context.pendingAppsFlyerAutologinRetry;
        if (stagedAutologin) {
          return enterState({ authenticating: 'login' }, {
            ...snapshotValue.context,
            authError: null,
            lastError: undefined,
            pendingWeb2AppLoginMethod: 'autologin',
          }, true, [`effect:${result.type}:expiredCleared:stagedAutologin`], {
            effects: [{ type: 'exchangeAppsFlyerAutologin', input: { token: stagedAutologin.token } }],
          });
        }
        return enterState('checkingHealth', snapshotValue.context, true, [`effect:${result.type}:expiredCleared`]);
      }
      if (snapshotValue.matches('clearingSession')) {
        const context = applyUpdate(snapshotValue.context, update.assignLoggedOut, { output: undefined });
        return enterState('unauthenticated', context, true, [`effect:${result.type}:logoutCleared`]);
      }
      return rejected(snapshotValue, `effect:${result.type}`);
    }

    case 'requestAccountDeletion': {
      // Account deletion triggers logout flow; success follows clearSession path
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:success`);
    }

    case 'requestSubscriptionCancellation': {
      // Subscription cancellation is a background operation; app state refreshes but user stays logged in
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:success`);
    }
  }
}

function settleMobileAppEffectError(snapshotValue: MobileAppSnapshot, result: MobileAppEffectFailure): MobileAppTransitionResult {
  switch (result.type) {
    case 'loadStoredSession': {
      if (!snapshotValue.matches('booting')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const context = applyUpdate(snapshotValue.context, update.assignHealthError, { error: result.error });
      return enterState('degraded', context, true, [`effect:${result.type}:error`]);
    }

    case 'checkProductHealth': {
      if (!snapshotValue.matches('checkingHealth')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const context = applyUpdate(snapshotValue.context, update.assignHealthError, { error: result.error });
      return enterState('unauthenticated', context, true, [`effect:${result.type}:error`]);
    }

    case 'exchangeAppsFlyerAutologin': {
      if (!snapshotValue.matches({ authenticating: 'login' })) {
        return rejected(snapshotValue, `effect:${result.type}:error`);
      }
      const retryState = snapshotValue.context.pendingAppsFlyerAutologinRetry;
      const attempt = retryState?.retryAttempt === 1 ? 'network_retry' : 'initial';
      const networkFailure = isAppsFlyerAutologinNetworkError(result.error);
      const scheduleRetry = networkFailure && retryState?.retryAttempt === 0;
      const context: MobileAppContext = {
        ...snapshotValue.context,
        session: null,
        appState: null,
        pendingSession: null,
        authError: null,
        authMessage: update.defaultAuthMessage,
        appStateMessage: 'Sign in to continue.',
        lastError: undefined,
        pendingAppsFlyerAutologinRetry: scheduleRetry ? retryState : null,
        // Preserve attempt open_id only while a network retry remains pending.
        pendingAppsFlyerOpenId: scheduleRetry
          ? (retryState?.openId ?? snapshotValue.context.pendingAppsFlyerOpenId)
          : null,
        pendingWeb2AppLoginMethod: null,
      };
      const effects: MobileAppEffectRequest[] = [
        webToAppAutologinProgress(snapshotValue.context, {
          stage: 'exchange_failed',
          attempt,
          failure_kind: networkFailure ? 'network' : appsFlyerAutologinFailureKind(result.error),
        }),
      ];
      if (scheduleRetry) {
        effects.push(webToAppAutologinProgress(snapshotValue.context, {
          stage: 'retry_scheduled',
          attempt: 'network_retry',
        }));
      }
      return withPrependedEffects(
        enterState('unauthenticated', context, true, [`effect:${result.type}:error`]),
        effects,
      );
    }

    case 'loginAccount':
    case 'registerAccount':
    case 'persistSession': {
      if (result.type === 'persistSession' && snapshotValue.matches({ getStarted: 'accountAllSet' })) {
        // Storage write failed; the in-memory session still works for this run,
        // so stay on accountAllSet instead of bouncing the user to login.
        return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error:getStarted`);
      }
      if (result.type === 'registerAccount' && snapshotValue.matches({ getStarted: 'creatingAccount' })) {
        // T-453: an existing-account refusal renders the neutral helper under
        // Password ("Couldn't create an account. Try logging in") — never the
        // confirming "already exists" copy, and never the error field variant.
        if (update.isExistingAccountAuthError(result.error)) {
          const context = withGetStarted(snapshotValue.context, {
            ...snapshotValue.context.getStarted,
            location: 'createAccount',
            message: null,
            accountExists: true,
          });
          return enterState({ getStarted: 'createAccount' }, context, true, [`effect:${result.type}:error:getStarted:accountExists`]);
        }
        // Feedback ("a failed registration says nothing"): a get-started
        // registration failure must not strand the user on the creatingAccount
        // spinner. Return to the create-account form with a visible message.
        const message = update.formatProductAuthError(result.error);
        const context = withGetStarted(snapshotValue.context, {
          ...snapshotValue.context.getStarted,
          location: 'createAccount',
          message,
        });
        return enterState({ getStarted: 'createAccount' }, context, true, [`effect:${result.type}:error:getStarted`]);
      }
      if (!snapshotValue.matches('authenticating')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const loginMethod = snapshotValue.context.pendingWeb2AppLoginMethod;
      const context = {
        ...applyUpdate(snapshotValue.context, update.assignAuthError, { error: result.error }),
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: null,
        pendingWeb2AppLoginMethod: null,
      };
      return withPrependedEffect(
        enterState('unauthenticated', context, true, [`effect:${result.type}:error`]),
        result.type === 'persistSession' && loginMethod === 'autologin'
          ? webToAppAutologinProgress(snapshotValue.context, { stage: 'persist_failed' })
          : null,
      );
    }

    case 'loadAppState': {
      if (snapshotValue.context.onboarding.pendingChapterRiskPersist) {
        return stay(snapshotValue, {
          ...snapshotValue.context,
          onboarding: {
            ...snapshotValue.context.onboarding,
            pendingChapterRiskPersist: false,
          },
        }, `effect:${result.type}:chapterRiskPersistError`);
      }
      if (!snapshotValue.matches('authenticatedLoading')) return rejected(snapshotValue, `effect:${result.type}:error`);
      if (isUnauthorizedProductApiError(result.error)) {
        const previousSessionToken = snapshotValue.context.session?.sessionToken;
        const context = applyUpdate(snapshotValue.context, update.assignUnauthorizedAppStateError, { error: result.error });
        return {
          snapshot: snapshot('unauthenticated', context),
          effects: previousSessionToken ? [{ type: 'clearSession', input: { sessionToken: previousSessionToken } }] : [],
          accepted: true,
          trace: [`effect:${result.type}:error:unauthorized`],
        };
      }
      if (snapshotValue.context.appState) {
        const context = applyUpdate(snapshotValue.context, update.assignAppStateRefreshError, { error: result.error });
        const sync = update.trainRuntimeSyncOf(context);
        const owner = sync.refreshOwner;
        const destGeneration = sync.pendingDestinationGeneration ?? sync.departureGeneration ?? 0;
        if (sync.pendingDestination && owner) {
          const generationChanged = owner.committedGeneration !== sync.committedGeneration
            || owner.departureGeneration !== destGeneration;
          if (generationChanged) {
            return enterState('authenticatedLoading', {
              ...context,
              trainRuntimeSync: {
                ...sync,
                refreshOwner: beginTrainRuntimeRefreshOwner(sync, 1),
              },
            }, true, [`effect:${result.type}:error:retry`]);
          }
          if (owner.attempt === 1) {
            return enterState('authenticatedLoading', {
              ...context,
              trainRuntimeSync: {
                ...sync,
                refreshOwner: { ...owner, attempt: 2 },
              },
            }, true, [`effect:${result.type}:error:retry`]);
          }
        }
        const next = {
          ...context,
          trainRuntimeSync: { ...sync, refreshOwner: undefined },
        };
        const loadedState: MobileAppStateValue = isFirstSessionComplete(next.appState)
          ? { commandCenter: 'ready' }
          : { firstSession: 'route' };
        return {
          snapshot: snapshot(loadedState, next),
          effects: [],
          accepted: true,
          trace: [`effect:${result.type}:error:cached`],
        };
      }
      const context = applyUpdate(snapshotValue.context, update.assignAppStateError, { error: result.error });
      return enterState('degraded', context, true, [`effect:${result.type}:error`]);
    }

    case 'runFirstSessionScan': {
      if (snapshotValue.matches({ firstSession: 'deepScan' })) {
        if (snapshotValue.context.onboarding.deepScanRun.status !== 'pending') {
          return rejected(snapshotValue, `effect:${result.type}:errorAwaitingDeviceProof`);
        }
        // Offline graceful path: nothing to poll, so the run is settled and the
        // theatrical floor alone advances with the findings we already have.
        const run = snapshotValue.context.onboarding.deepScanRun;
        const context = withDeepScanRun(
          applyUpdate(snapshotValue.context, update.assignFirstSessionScanError, { error: result.error }),
          { status: 'settled' },
        );
        if (run.floorElapsed) return advanceDeepScanCompleted(context, [`effect:${result.type}:errorAfterFloor`]);
        return stay(snapshotValue, context, `effect:${result.type}:error`);
      }
      return rejected(snapshotValue, `effect:${result.type}:error`);
    }

    case 'firstSessionScanPoll': {
      if (!snapshotValue.matches({ firstSession: 'deepScan' })) {
        return rejected(snapshotValue, `effect:${result.type}:error`);
      }
      const run = snapshotValue.context.onboarding.deepScanRun;
      if (run.status !== 'polling' || run.scanRunId === null) {
        return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:errorStale`);
      }
      // A failed status poll consumes budget and retries, so a flaky network
      // is bounded by the same hard ceiling as a slow scan.
      const pollsRemaining = run.pollsRemaining - 1;
      if (pollsRemaining <= 0) {
        const context = withDeepScanRun(snapshotValue.context, { status: 'settled', pollsRemaining: 0 });
        if (run.floorElapsed) return advanceDeepScanCompleted(context, [`effect:${result.type}:errorCeiling`]);
        return stay(snapshotValue, context, `effect:${result.type}:errorCeiling`);
      }
      const context = withDeepScanRun(snapshotValue.context, { pollsRemaining });
      return {
        snapshot: snapshot(snapshotValue.value, context),
        effects: [firstSessionScanPollEffect(context, run.scanRunId)],
        accepted: true,
        trace: [`effect:${result.type}:errorRetry`],
      };
    }

    case 'advanceFirstSession': {
      // A failed checkpoint persist must not re-queue the same event. One
      // reconcile re-reads backend state; a second consecutive failure is a
      // recoverable degraded screen so a no-op/conflict cannot cycle.
      if (!snapshotValue.matches({ firstSession: 'advancing' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      return recoverFirstSessionAdvance(snapshotValue, result.error, `effect:${result.type}:error`);
    }

    case 'onboardingTimer': {
      // Timers cannot meaningfully fail; treat an error as the elapsed signal.
      const phase = onboardingTimerPhaseFromFailure(result)
        ?? (snapshotValue.matches({ firstSession: 'deepScan' }) ? 'deepScan' : null);
      if (!phase) return rejected(snapshotValue, `effect:${result.type}:error`);
      const generation = result.input && typeof result.input === 'object' && 'generation' in result.input
        && typeof result.input.generation === 'number'
        ? result.input.generation
        : undefined;
      return settleOnboardingTimerElapsed(snapshotValue, phase, result.type, generation);
    }

    case 'protectToastTimer': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}:error`);
      return stay(snapshotValue, withProtectRuntime(snapshotValue.context, clearProtectToast({ ...snapshotValue.context.protect.runtime, toastTimerArmed: false })), `effect:${result.type}:error`);
    }

    case 'requestPasswordReset': {
      if (!snapshotValue.matches({ forgotPassword: 'requestingReset' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      const context = withForgotPassword(snapshotValue.context, {
        ...snapshotValue.context.forgotPassword,
        location: 'forgotPassword',
        message: 'We could not send that reset link. Try again in a moment.',
      });
      return enterState({ forgotPassword: 'forgotPassword' }, context, true, [`effect:${result.type}:error`]);
    }

    case 'resetPassword': {
      if (!snapshotValue.matches({ forgotPassword: 'resettingPassword' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      const context = withForgotPassword(snapshotValue.context, {
        ...snapshotValue.context.forgotPassword,
        location: 'changePassword',
        message: 'That reset link did not work. Request a new password reset.',
      });
      return enterState({ forgotPassword: 'changePassword' }, context, true, [`effect:${result.type}:error`]);
    }

    case 'forgotPasswordTimer': {
      if (!snapshotValue.matches('forgotPassword')) return rejected(snapshotValue, `effect:${result.type}:error`);
      if (snapshotValue.context.forgotPassword.location === 'passwordChanged') {
        return enterState('unauthenticated', {
          ...snapshotValue.context,
          authPassword: '',
          authMessage: 'Password changed. Log in with your new password.',
          forgotPassword: createForgotPasswordRuntimeState({ email: snapshotValue.context.forgotPassword.email }),
        }, true, [`effect:${result.type}:errorSuccessRedirect`]);
      }
      return stay(snapshotValue, withForgotPassword(snapshotValue.context, {
        ...snapshotValue.context.forgotPassword,
        resendCooldownSeconds: 0,
      }), `effect:${result.type}:errorResendCooldown`);
    }

    case 'getStartedTimer': {
      if (!snapshotValue.matches('getStarted')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const failedInput = result.input as GetStartedTimerInput | undefined;
      if (failedInput?.phase === 'selectionDwell') {
        // Same advance semantics on timer error as success — dwell floor elapsed.
        return settleGetStartedSelectionDwell(snapshotValue, failedInput, `effect:${result.type}:errorSelectionDwell`);
      }
      if (snapshotValue.context.getStarted.location === 'analyzing') {
        return enterState({ getStarted: 'result' }, withGetStarted(snapshotValue.context, {
          ...snapshotValue.context.getStarted,
          location: 'result',
        }), true, [`effect:${result.type}:errorAnalyzing`]);
      }
      // A failed scan-floor timer is treated as elapsed: mark the floor via the
      // machine value and keep waiting for the scan data (same as success).
      if (snapshotValue.matches({ getStarted: 'scan' })) {
        return { snapshot: snapshot({ getStarted: 'scanResult' }, snapshotValue.context), effects: [], accepted: true, trace: [`effect:${result.type}:errorScanFloor`] };
      }
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error`);
    }

    case 'persistGetStartedQuizAnswer': {
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error`);
    }

    case 'checkExtensionActivation': {
      // A failed activation probe is non-fatal: stay on the setup screen so the
      // next poll or user action can proceed (§8.2).
      if (snapshotValue.matches({ commandCenter: 'ready' })) {
        // Same visibility rule as the non-active proof: the Enable CTA must
        // never fail silently while the Browser Protection sheet is open. The
        // background probe stays silent.
        const protect = snapshotValue.context.protect;
        if (protect.runtime.overlay !== 'browserSheet') {
          return stay(snapshotValue, {
            ...snapshotValue.context,
            appStateMessage: 'Safari extension proof could not be checked.',
          }, `effect:${result.type}:errorProtect`);
        }
        return enterState({ commandCenter: 'ready' }, withProtect({
          ...snapshotValue.context,
          appStateMessage: 'Safari extension proof could not be checked.',
        }, {
          ...protect,
          runtime: { ...protect.runtime, toast: 'browserProtectionNotDetected' },
        }), true, [`effect:${result.type}:errorProtect`]);
      }
      if (!snapshotValue.matches({ firstSession: 'browser' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      return { snapshot: snapshotValue, effects: [], accepted: true, trace: [`effect:${result.type}:error`] };
    }

    case 'setBrowserProtectionAppEnabled': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const input = result.input as SetBrowserProtectionAppEnabledInput | undefined;
      const label = input?.enabled ? 'on' : 'off';
      // codexfix #5 (adopted): a failed preference save must not leave the
      // optimistic toggle as durable truth — rebuild Protect from the last
      // authoritative app-state (navigation preserved) and surface the
      // unverified-action toast.
      const authoritativeProtect = snapshotValue.context.appState
        ? update.preserveProtectDrillInAcrossAppStateRefresh(
          snapshotValue.context.protect,
          update.protectStateFrom(snapshotValue.context.appState),
        )
        : snapshotValue.context.protect;
      return stay(snapshotValue, {
        ...snapshotValue.context,
        appStateMessage: `Unable to save Browser Protection ${label} preference. The displayed state is not yet confirmed by the server.`,
        lastError: `Unable to save Browser Protection ${label} preference.`,
        protect: {
          ...authoritativeProtect,
          runtime: {
            ...authoritativeProtect.runtime,
            toast: 'taskActionUnverified',
          },
        },
      }, `effect:${result.type}:error`);
    }

    case 'checkPhoneSecurityProofs': {
      if (snapshotValue.matches({ firstSession: 'deepScan' })) {
        return startFirstSessionScanAfterDeviceProof(
          snapshotValue.value,
          snapshotValue.context,
          `effect:${result.type}:error:deepScanUnknown`,
        );
      }
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}:error`);
      const protect = snapshotValue.context.protect;
      const timedOut = isPhoneSecurityProofsTimeoutError(result.error);
      if (timedOut) {
        // Domain outcome carries timed_out; settle without aggregate reason so
        // the iOS row paints "Not available yet" (not "Check failed.").
        return withPrependedEffect(
          enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
            ...protect,
            runtime: settlePhoneSecurityProofsAfterTimeout(protect.runtime),
          }), true, [`effect:${result.type}:error:timeout`]),
          analyticsTrackEffect('device_check_completed', { outcome: 'timed_out' }),
        );
      }
      // Genuine unexpected service/provider failure — not every error is a deadline.
      // Dynamic import keeps the interpreter free of the Sentry/RN graph in node tests.
      void import('../observability/sentry').then(({ captureHandledMobileException }) => {
        captureHandledMobileException(result.error, {
          operation: 'checkPhoneSecurityProofs.error',
        });
      }).catch(() => undefined);
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
        ...protect,
        runtime: settlePhoneSecurityProofsAfterError(protect.runtime, result.error),
      }), true, [`effect:${result.type}:error`]);
    }

    case 'resolveProtectTask': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}:error`);
      return stay(snapshotValue, {
        ...snapshotValue.context,
        appStateMessage: formatProtectResolutionError(result.error),
        lastError: formatProtectResolutionError(result.error),
      }, `effect:${result.type}:error`);
    }

    case 'addMonitoringEmail': {
      if (!snapshotValue.matches('commandCenter')) return rejected(snapshotValue, `effect:${result.type}:error`);
      return stay(snapshotValue, {
        ...snapshotValue.context,
        appStateMessage: 'We could not save that monitoring email. Try again in a moment.',
        lastError: 'We could not save that monitoring email. Try again in a moment.',
      }, `effect:${result.type}:error`);
    }

    case 'runScan': {
      if (!snapshotValue.matches({ commandCenter: 'scanning' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      const context = applyUpdate(snapshotValue.context, update.assignScanError, { error: result.error });
      return enterState({ commandCenter: 'ready' }, context, true, [`effect:${result.type}:error`]);
    }

    case 'sendFamilyInvites': {
      // Invite persistence failed: stay on InviteFamily with a retry message so
      // the owner can resend or defer.
      if (!snapshotValue.matches({ firstSession: 'inviteFamily' })) return rejected(snapshotValue, `effect:${result.type}:error`);
      const familyInvite = { ...snapshotValue.context.familyInvite, message: "We couldn't send those invites. Try again in a moment." };
      return stay(snapshotValue, withFamilyInvite(snapshotValue.context, familyInvite), `effect:${result.type}:error`);
    }

    case 'runFreeScan': {
      // Re-scan failed: publish the failed lifecycle/error slots and clear the
      // in-flight flag so the user can retry. Cooldown is only armed on success,
      // so the button stays available.
      const context = applyUpdate(snapshotValue.context, update.assignProtectFreeScanError, { error: result.error });
      // A leak re-scan requested from the paid Leak detection screen must not
      // fail silently (same visibility rule as the Enable CTA).
      const protectLocation = context.protect.location;
      if (protectLocation.name === 'leakDetection' || protectLocation.name === 'monitoring') {
        return {
          snapshot: snapshot(snapshotValue.value, withProtect(context, {
            ...context.protect,
            runtime: { ...context.protect.runtime, toast: 'leakScanFailed', toastTimerArmed: true },
          })),
          effects: [{ type: 'protectToastTimer', input: {} }],
          accepted: true,
          trace: [`effect:${result.type}:errorLeakDetection`],
        };
      }
      return stay(snapshotValue, context, `effect:${result.type}:error`);
    }

    case 'freeScanTimer': {
      // A dropped tick just stops the countdown loop; leave the remaining value
      // as-is so a later scan or app reload re-arms it.
      return { snapshot: snapshotValue, effects: [], accepted: true, trace: [`effect:${result.type}:error`] };
    }

    case 'analytics.track': {
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error`);
    }

    case 'clearSession': {
      if (snapshotValue.matches('clearingExpiredSession')) {
        return enterState('checkingHealth', snapshotValue.context, true, [`effect:${result.type}:errorExpired`]);
      }
      if (snapshotValue.matches('clearingSession')) {
        const context = applyUpdate(snapshotValue.context, update.assignLoggedOut, { error: result.error });
        return enterState('unauthenticated', context, true, [`effect:${result.type}:errorLogout`]);
      }
      return rejected(snapshotValue, `effect:${result.type}:error`);
    }

    case 'requestAccountDeletion': {
      // Deletion error prevents logout; stay in current state
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error`);
    }

    case 'requestSubscriptionCancellation': {
      // Cancellation error is non-blocking; stay in current state
      return stay(snapshotValue, snapshotValue.context, `effect:${result.type}:error`);
    }
  }
}

function transitionUnauthenticated(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  if (event.type === 'SUBMIT_LOGIN') {
    if (hasCredentials(snapshotValue.context)) {
      const context: MobileAppContext = {
        ...applyUpdate(snapshotValue.context, update.startLogin, event),
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: null,
        pendingWeb2AppLoginMethod: snapshotValue.context.fSessionId ? 'manual_fallback' : null,
      };
      return enterState({ authenticating: 'login' }, context, true, [`event:${event.type}`]);
    }
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignMissingCredentials, event), `event:${event.type}:missingCredentials`);
  }

  if (event.type === 'SUBMIT_REGISTER') {
    if (hasCredentials(snapshotValue.context)) {
      const context = {
        ...applyUpdate(snapshotValue.context, update.startRegistration, event),
        pendingAppsFlyerAutologinRetry: null,
        pendingAppsFlyerOpenId: null,
        pendingWeb2AppLoginMethod: null,
      };
      return enterState({ authenticating: 'register' }, context, true, [`event:${event.type}`]);
    }
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignMissingCredentials, event), `event:${event.type}:missingCredentials`);
  }

  if (event.type === 'FORGOT_PASSWORD_START') {
    const context = withForgotPassword(snapshotValue.context, createForgotPasswordRuntimeState({
      location: 'forgotPassword',
      email: snapshotValue.context.authEmail,
    }));
    return enterState({ forgotPassword: 'forgotPassword' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'FORGOT_PASSWORD_OPEN_RESET') {
    const context = withForgotPassword(snapshotValue.context, createForgotPasswordRuntimeState({
      location: 'changePassword',
      resetToken: event.token,
    }));
    return enterState({ forgotPassword: 'changePassword' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'GET_STARTED_START') {
    // T-453: Back on the sign-in reached through "Try logging in" returns to
    // the create-account form with everything typed preserved — never a
    // rewind to Welcome.
    if (snapshotValue.context.getStarted.tryLoginReturn && snapshotValue.context.getStarted.location === 'createAccount') {
      const context = withGetStarted(snapshotValue.context, { ...snapshotValue.context.getStarted, tryLoginReturn: false });
      return enterState({ getStarted: 'createAccount' }, context, true, [`event:${event.type}:tryLoginReturn`]);
    }
    // `welcome` unless the app was booted with a funnel entry seed. The seed is
    // the only way to open a mid-funnel beat (create-account, quiz, result):
    // the pre-account funnel has no persisted checkpoint to resume from, so
    // without it every entry rewinds to Welcome.
    const entry = snapshotValue.context.getStartedEntry;
    const location = entry?.location ?? 'welcome';
    const context = withGetStarted(
      { ...snapshotValue.context, getStartedHistory: [...(entry?.history ?? [])], getStartedForwardHistory: [] },
      // Restore the seed's recorded quiz answers alongside its location: the
      // answer-derived Result (level, pills, T-234 glow) must open exactly as
      // the walked funnel would have shown it, never as the empty-quiz fallback.
      createGetStartedRuntimeState({ location, ...(entry?.answers ? { answers: entry.answers } : {}) }),
    );
    return withPrependedEffect(
      enterState({ getStarted: location }, context, true, [`event:${event.type}`]),
      analyticsTrackEffect('onboarding_started', {}),
    );
  }

  return rejected(snapshotValue, event.type);
}

function isProtectOrProfileTab(tab: string): tab is 'protect' | 'profile' {
  return tab === 'protect' || tab === 'profile';
}

function destinationTabFromLocation(location: CanonicalLocation): 'protect' | 'profile' | null {
  if (location.area === 'protect') return 'protect';
  if (location.area === 'profile') return 'profile';
  return null;
}

function interceptTrainDeparture(
  snapshotValue: MobileAppSnapshot,
  destination: TrainRuntimePendingDestination | null,
): MobileAppTransitionResult | null {
  if (!destination) return null;
  const context = snapshotValue.context;
  if (context.selectedTab !== 'train') return null;
  const sync = update.trainRuntimeSyncOf(context);
  const dirty = sync.activeMutationIds.length > 0 || sync.committedGeneration > sync.synchronizedGeneration;
  if (!dirty) return null;

  const departureGeneration = (sync.departureGeneration ?? 0) + 1;
  const nextSync: TrainRuntimeSyncState = {
    ...sync,
    pendingDestination: destination,
    departureGeneration,
    pendingDestinationGeneration: departureGeneration,
  };
  if (sync.activeMutationIds.length > 0) {
    return stay(snapshotValue, { ...context, trainRuntimeSync: nextSync }, 'event:NAVIGATE:trainDepartureDeferred');
  }
  if (sync.refreshOwner) {
    return stay(snapshotValue, { ...context, trainRuntimeSync: nextSync }, 'event:NAVIGATE:trainDepartureRefreshInFlight');
  }
  return enterState('authenticatedLoading', {
    ...context,
    trainRuntimeSync: { ...nextSync, refreshOwner: beginTrainRuntimeRefreshOwner(nextSync, 1) },
  }, true, ['event:NAVIGATE:trainDepartureRefresh']);
}

function transitionTrainRuntimeMutationSettled(
  snapshotValue: MobileAppSnapshot,
  event: Extract<MobileAppEvent, { type: 'TRAIN_RUNTIME_MUTATION_SETTLED' }>,
): MobileAppTransitionResult {
  const context = applyUpdate(snapshotValue.context, update.assignTrainRuntimeMutationSettled, event);
  const sync = update.trainRuntimeSyncOf(context);
  if (sync.activeMutationIds.length > 0 || !sync.pendingDestination) {
    return stay(snapshotValue, context, `event:${event.type}`);
  }
  if (sync.committedGeneration > sync.synchronizedGeneration) {
    if (sync.refreshOwner) {
      return stay(snapshotValue, context, `event:${event.type}:refreshInFlight`);
    }
    return enterState('authenticatedLoading', {
      ...context,
      trainRuntimeSync: { ...sync, refreshOwner: beginTrainRuntimeRefreshOwner(sync, 1) },
    }, true, [`event:${event.type}:refreshPendingDestination`]);
  }
  const next = applyPendingTrainDestination(context, sync.pendingDestination);
  return stay(snapshotValue, {
    ...next,
    trainRuntimeSync: {
      ...sync,
      pendingDestination: undefined,
      pendingDestinationGeneration: undefined,
      refreshOwner: undefined,
    },
  }, `event:${event.type}:applyFailedDeparture`);
}

function consumeTrainRuntimeRefresh(
  context: MobileAppContext,
): { context: MobileAppContext; reload: boolean } {
  const sync = update.trainRuntimeSyncOf(context);
  if (sync.refreshOwner === undefined) return { context, reload: false };
  if (sync.committedGeneration > sync.refreshOwner.committedGeneration) {
    return {
      context: {
        ...context,
        trainRuntimeSync: { ...sync, refreshOwner: beginTrainRuntimeRefreshOwner(sync, 1) },
      },
      reload: true,
    };
  }
  const pending = sync.pendingDestination;
  let next: MobileAppContext = {
    ...context,
    trainRuntimeSync: {
      ...sync,
      synchronizedGeneration: sync.committedGeneration,
      refreshOwner: undefined,
      pendingDestination: undefined,
      pendingDestinationGeneration: undefined,
    },
  };
  if (pending) {
    next = applyPendingTrainDestination(next, pending);
  }
  return { context: next, reload: false };
}

function applyPendingTrainDestination(
  context: MobileAppContext,
  pending: TrainRuntimePendingDestination,
): MobileAppContext {
  if (pending.kind === 'navigate') {
    return applyUpdate(context, update.assignNavigation, { type: 'NAVIGATE', tab: pending.tab });
  }
  if (!isProtectedRoute(pending.location)) return context;
  const location = normalizeProtectedRoute(context, pending.location, pending.source);
  return applyProtectedRoute(context, location);
}

/**
 * Applies a URL-derived request through the existing machine/domain locations.
 * It intentionally does not replay UI clicks: a browser Back jump may cross
 * tabs and nested views, so translating it to a single `PROTECT_BACK` would be
 * wrong. Invalid or unavailable identifiers collapse to an allowed root.
 */
function transitionRouteRequested(
  snapshotValue: MobileAppSnapshot,
  event: Extract<MobileAppEvent, { type: 'ROUTE_REQUESTED' }>,
): MobileAppTransitionResult {
  const requested = event.location;
  const context = snapshotValue.context;

  // A session found in storage is not authority to navigate yet. The boot
  // service must first either hydrate app-state or definitively clear/expire
  // it. This includes public URLs: otherwise /sign-in can strand a valid
  // stored account before the normal first-session/entitlement guards run.
  if (context.session && !context.appState) {
    return stay(snapshotValue, context, `event:${event.type}:awaitHydration`);
  }

  // First-session is a higher-priority location than both public credentials
  // and protected deep links. Re-enter the canonical onboarding route instead
  // of allowing a browser URL to bypass a required onboarding chapter.
  if (context.session && context.appState && !isFirstSessionComplete(context.appState)) {
    if (snapshotValue.matches('firstSession')) return stay(snapshotValue, context, `event:${event.type}:firstSessionPriority`);
    return enterState({ firstSession: 'route' }, context, true, [`event:${event.type}:firstSessionPriority`]);
  }

  if (!isProtectedRoute(requested)) {
    // Auth/funnel routes are public entry points, not a way to hide an already
    // hydrated signed-in account behind an auth screen.
    if (context.session && context.appState) {
      return enterState({ commandCenter: 'ready' }, applyProtectedRoute(context, { ...rootLocations.profile }), true, [`event:${event.type}:alreadyAuthenticated`]);
    }
    const publicContext = {
      ...context,
      pendingReturnTo: event.returnTo ?? context.pendingReturnTo,
    };
    if (requested.area === 'getStarted') {
      return withPrependedEffect(
        enterState({ getStarted: 'welcome' }, withGetStarted({ ...publicContext, getStartedHistory: [], getStartedForwardHistory: [] }, createGetStartedRuntimeState({ location: 'welcome' })), true, [`event:${event.type}:getStarted`]),
        analyticsTrackEffect('onboarding_started', {}),
      );
    }
    if (requested.area === 'auth' && requested.screen === 'forgotPassword') {
      return enterState({ forgotPassword: 'forgotPassword' }, withForgotPassword(publicContext, createForgotPasswordRuntimeState({ location: 'forgotPassword' })), true, [`event:${event.type}:forgotPassword`]);
    }
    if (requested.area === 'auth' && requested.screen === 'passwordReset') {
      const resetToken = context.forgotPassword.resetToken;
      if (resetToken) {
        return enterState({ forgotPassword: 'changePassword' }, withForgotPassword(publicContext, {
          ...context.forgotPassword,
          location: 'changePassword',
          newPassword: '',
          confirmPassword: '',
          message: null,
        }), true, [`event:${event.type}:passwordReset`]);
      }
      return enterState({ forgotPassword: 'forgotPassword' }, withForgotPassword(publicContext, createForgotPasswordRuntimeState({ location: 'forgotPassword' })), true, [`event:${event.type}:missingResetToken`]);
    }
    // Create-account is an auth presentation choice; the underlying auth
    // machine remains the authority for registration and validation.
    return enterState('unauthenticated', { ...publicContext, authRoute: requested.area === 'auth' && requested.screen === 'createAccount' ? 'createAccount' : 'signIn' }, true, [`event:${event.type}:signIn`]);
  }

  if (!context.session) {
    return enterState('unauthenticated', {
      ...context,
      authRoute: 'signIn',
      pendingReturnTo: requested,
    }, true, [`event:${event.type}:authRequired`]);
  }

  const location = normalizeProtectedRoute(context, requested, event.source);
  const next = applyProtectedRoute(context, location);
  return enterState({ commandCenter: 'ready' }, next, true, [`event:${event.type}:${location.area}`]);
}

/**
 * Internal Train continuation deliberately does not use ROUTE_REQUESTED.
 * A public lesson URL identifies a course/lesson, while lesson beats such as
 * correct-answer, breakdown, and finished are machine-only presentation state.
 */
function isAuthenticatedTrainSurface(snapshotValue: MobileAppSnapshot): boolean {
  const context = snapshotValue.context;
  return Boolean(
    snapshotValue.matches('commandCenter')
    && context.selectedTab === 'train'
    && context.session
    && context.appState
    && isFirstSessionComplete(context.appState),
  );
}

function transitionTrainAction(snapshotValue: MobileAppSnapshot, event: { type: string; courseId?: unknown; lessonId?: unknown }): MobileAppTransitionResult {
  if (!isAuthenticatedTrainSurface(snapshotValue)) return rejected(snapshotValue, event.type);
  if (!isTrainActionLegalForLocation(event.type, snapshotValue.context.trainLocation, event)) {
    return rejected(snapshotValue, event.type);
  }
  return stay(snapshotValue, snapshotValue.context, `event:${event.type}`);
}

function transitionTrainLocation(snapshotValue: MobileAppSnapshot, location: Extract<CanonicalLocation, { area: 'train' }>): MobileAppTransitionResult {
  const context = snapshotValue.context;
  if (!context.session || !context.appState) return stay(snapshotValue, context, 'event:TRAIN_LOCATION_CHANGED:awaitHydration');
  if (!isFirstSessionComplete(context.appState)) return enterState({ firstSession: 'route' }, context, true, ['event:TRAIN_LOCATION_CHANGED:firstSessionPriority']);
  const normalized = normalizeProtectedRoute(context, location, 'internal');
  if (normalized.area !== 'train') {
    const next = applyProtectedRoute(context, normalized);
    return enterState({ commandCenter: 'ready' }, next, true, [`event:TRAIN_LOCATION_CHANGED:${normalized.area}`]);
  }
  const projected = projectTrainLocationForMachine(normalized);
  if (sameSemanticTrainLocation(context.trainLocation, projected)) {
    return rejected(snapshotValue, 'TRAIN_LOCATION_CHANGED');
  }
  const next = applyProtectedRoute(context, projected);
  return withPrependedEffects(
    enterState({ commandCenter: 'ready' }, next, true, [`event:TRAIN_LOCATION_CHANGED:${projected.area}`]),
    trainAnalyticsEffects(projected),
  );
}

function trainAnalyticsEffects(location: ProtectedCanonicalLocation): MobileAppEffectRequest[] {
  if (location.area !== 'train') return [];
  if (location.screen === 'lesson') {
    const properties = { course_id: location.courseId, lesson_id: location.lessonId };
    if (location.step === 'lesson-finished') {
      return [analyticsTrackEffect('train_lesson_completed', properties)];
    }
    if (!location.step || location.step === 'lesson') {
      return [analyticsTrackEffect('train_lesson_started', properties)];
    }
  }
  if (location.screen === 'quiz' && location.result === 'passed') {
    return [analyticsTrackEffect('train_course_completed', { course_id: location.courseId })];
  }
  return [];
}

function isProtectedRoute(location: CanonicalLocation): location is ProtectedCanonicalLocation {
  return location.area === 'protect' || location.area === 'train' || location.area === 'profile';
}

function normalizeProtectedRoute(
  context: MobileAppContext,
  location: ProtectedCanonicalLocation,
  source: Extract<MobileAppEvent, { type: 'ROUTE_REQUESTED' }>['source'] | 'internal',
): ProtectedCanonicalLocation {
  // Expired users have a deliberately small, role-aware recovery surface. In
  // particular, Profile is not a blanket exemption: cancellation routes and
  // owner continuations must fail closed because they imply an eligible owner
  // entitlement and an accepted billing outcome.
  if (context.appState?.subscription.status === 'expired') {
    if (location.area !== 'profile') return { ...rootLocations.profile };
    const role = context.appState.profileState?.subscription.role;
    const allowedExpiredProfileRoute = location.screen === 'main'
      || location.screen === 'password'
      || location.screen === 'delete'
      || location.screen === 'logout'
      || (location.screen === 'memberManage' && role === 'member');
    if (!allowedExpiredProfileRoute) return { ...rootLocations.profile };
  }
  if (location.area === 'train') {
    if (context.appState?.plan === 'free') return { ...rootLocations.profile };
    const paidAccount = context.appState?.plan === 'solo' || context.appState?.plan === 'family';
    const courses = context.appState?.trainRuntime.courses ?? [];
    if (location.screen === 'course' || location.screen === 'quiz' || location.screen === 'certificate') {
      // Entitlement is still guarded above; T-208 trainRuntime owns existence.
      return courses.some((course) => course.courseId === location.courseId) || paidAccount
        ? location
        : { ...rootLocations.train };
    }
    if (location.screen === 'lesson') {
      const course = courses.find((item) => item.courseId === location.courseId);
      if (!course) return paidAccount ? location : { ...rootLocations.train };
      return source === 'browser-initial' || source === 'browser-pop'
        ? { ...location, step: 'lesson', selectedLessonOptionText: undefined }
        : location;
    }
  }
  if (location.area === 'protect' && location.screen === 'task') {
    return context.protect.runtime.tasks.some((task) => task.id === location.taskId)
      ? location
      : { ...rootLocations.protect };
  }
  if (location.area === 'profile') {
    const subscription = context.appState?.profileState?.subscription;
    const cancellationScreens = new Set([
      'cancel',
      'cancelWarning',
      'cancelReason',
      'cancelSuccess',
      'cancelProblem',
    ]);
    if (cancellationScreens.has(location.screen)) {
      const eligibleCancellationOwner = subscription?.status !== 'expired'
        && ((subscription?.plan === 'solo' && subscription.role === 'none')
          || (subscription?.plan === 'family' && subscription.role === 'owner'));
      if (!eligibleCancellationOwner) return { ...rootLocations.profile };
    }
    const ownerContinuationScreens = new Set([
      'ownerManage',
      'ownerInvite',
      'ownerCancel',
      'ownerCancelWarning',
      'ownerCancelReason',
      'ownerCancelSuccess',
      'ownerCancelProblem',
    ]);
    // Every owner-only continuation requires household owner on an eligible paid plan.
    if (ownerContinuationScreens.has(location.screen)) {
      const eligibleOwner =
        subscription?.role === 'owner'
        && (subscription.plan === 'solo' || subscription.plan === 'family')
        && subscription.status !== 'expired';
      if (!eligibleOwner) return { ...rootLocations.profile };
    }
    if (location.screen === 'memberManage' && subscription?.role !== 'member') return { ...rootLocations.profile };
  }
  return location;
}

function applyProtectedRoute(context: MobileAppContext, location: ProtectedCanonicalLocation): MobileAppContext {
  if (location.area === 'train') {
    return { ...context, selectedTab: 'train', trainLocation: location, pendingReturnTo: null, authRoute: 'signIn' };
  }
  if (location.area === 'profile') {
    return { ...context, selectedTab: 'profile', profileLocation: location, pendingReturnTo: null, authRoute: 'signIn' };
  }

  const runtime = context.protect.runtime;
  if (location.screen === 'task') {
    const task = runtime.tasks.find((item) => item.id === location.taskId);
    if (!task) return applyProtectedRoute(context, { ...rootLocations.protect });
    const variant = deriveTaskDetailVariant(task);
    // T-299: fire-and-forget local opened persistence (pure state stays sync).
    rememberProtectTaskOpenedId(task.id);
    return {
      ...context,
      selectedTab: 'protect',
      pendingReturnTo: null,
      authRoute: 'signIn',
      protect: {
        ...context.protect,
        runtime: markProtectTaskOpened(runtime, task.id),
        location: { name: 'taskDetail', variant },
        history: [protectMainLocation(runtime)],
      },
    };
  }

  let protectLocation: ProtectRuntimeLocation = protectMainLocation(runtime);
  let nextRuntime = runtime;
  switch (location.screen) {
    case 'todo':
      nextRuntime = { ...runtime, todoTab: location.tab };
      protectLocation = protectTodoLocation(nextRuntime);
      break;
    case 'browsing':
      protectLocation = { name: 'browsing', screen: protectBrowsingScreenFromRuntime(runtime) };
      break;
    case 'phoneSecurity':
      protectLocation = { name: 'phoneSecurity', issues: runtime.phoneChecks.some((check) => check.status === 'issue') };
      break;
    case 'leakDetection':
      nextRuntime = { ...runtime, leakTab: location.tab };
      protectLocation = protectLeakDetectionLocation(nextRuntime);
      break;
    case 'monitoring':
      protectLocation = { name: 'monitoring', screen: 'list' };
      break;
    case 'root':
      break;
  }
  return {
    ...context,
    selectedTab: 'protect',
    pendingReturnTo: null,
    authRoute: 'signIn',
    protect: {
      ...context.protect,
      runtime: nextRuntime,
      location: protectLocation,
      history: location.screen === 'root' ? [] : [protectMainLocation(runtime)],
    },
  };
}

const GET_STARTED_LINEAR_NEXT: Partial<Record<GetStartedRuntimeLocation, GetStartedRuntimeLocation>> = {
  welcome: 'socialProof',
  socialProof: 'riskUnknown',
  riskUnknown: 'q1',
};

function transitionGetStarted(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const getStarted = snapshotValue.context.getStarted;
  const location = getStarted.location;

  // Back pops the recorded forward path (G2): the previous rest screen, never
  // Login. The funnel entry (empty history) has no Back — its explicit
  // "Log in" primary (T-439; formerly the "I already have an account"
  // secondary) is the only exit to sign-in.
  if (event.type === 'GET_STARTED_BACK') {
    const history = snapshotValue.context.getStartedHistory;
    const previous = history[history.length - 1];
    if (!previous) return rejected(snapshotValue, event.type);
    const context: MobileAppContext = {
      ...snapshotValue.context,
      getStartedHistory: history.slice(0, -1),
      getStartedForwardHistory: [...snapshotValue.context.getStartedForwardHistory, location],
      getStarted: { ...getStarted, location: previous, message: null },
    };
    return enterState({ getStarted: previous }, context, true, [`event:${event.type}:${previous}`]);
  }

  if (event.type === 'GET_STARTED_FORWARD') {
    const future = snapshotValue.context.getStartedForwardHistory;
    const next = future[future.length - 1];
    if (!next) return rejected(snapshotValue, event.type);
    const context: MobileAppContext = {
      ...snapshotValue.context,
      getStartedHistory: pushHistoryEntry(snapshotValue.context.getStartedHistory, location),
      getStartedForwardHistory: future.slice(0, -1),
      getStarted: { ...getStarted, location: next, message: null },
    };
    return enterState({ getStarted: next }, context, true, [`event:${event.type}:${next}`]);
  }

  if (event.type === 'GET_STARTED_TRY_LOGIN') {
    // T-453: "Try logging in" on the existing-account helper. Unlike
    // GET_STARTED_SIGN_IN this must NOT reset the funnel state — the sign-in
    // back chevron returns to create-account with everything typed preserved
    // (tryLoginReturn consumed by GET_STARTED_START). The typed email
    // prefills sign-in; the password field starts empty.
    if (location !== 'createAccount' || !getStarted.accountExists || snapshotValue.context.session) {
      return rejected(snapshotValue, event.type);
    }
    const context: MobileAppContext = {
      ...withGetStarted(snapshotValue.context, { ...getStarted, tryLoginReturn: true }),
      authEmail: getStarted.email.trim(),
      authPassword: '',
      authError: null,
      authRoute: 'signIn',
    };
    return enterState('unauthenticated', context, true, [`event:${event.type}`]);
  }

  if (event.type === 'GET_STARTED_SIGN_IN') {
    // Only the pre-account funnel offers the "Log in" exit (T-439; formerly
    // "I already have an account"). Once a session exists (post
    // create-account), leaving for sign-in would strand a signed-in session
    // behind an auth surface — reject instead.
    if (snapshotValue.context.session) return rejected(snapshotValue, event.type);
    return enterState('unauthenticated', withGetStarted({ ...snapshotValue.context, getStartedHistory: [], getStartedForwardHistory: [] }, createGetStartedRuntimeState()), true, [`event:${event.type}`]);
  }

  if (event.type === 'GET_STARTED_SELECT_CHOICE') {
    // The RiskUnknown gauge flows directly into the first quiz question: the
    // initial choice is the q1 answer.
    const quizLocation = location === 'riskUnknown' ? 'q1' : location;
    const updated = applyQuizSelection(getStarted, quizLocation, event.label);
    const persistEffect = createPersistGetStartedQuizAnswerEffect(snapshotValue.context, quizLocation, updated);
    if (location === 'riskUnknown') {
      return withPrependedEffect(
        enterState({ getStarted: 'q1' }, withGetStarted(pushGetStartedHistory(snapshotValue.context, 'riskUnknown'), { ...updated, location: 'q1' }), true, [`event:${event.type}`]),
        persistEffect,
      );
    }
    if (isSingleSelectLocation(quizLocation)) {
      // Keep the selected answer painted on this question for a perceptible
      // dwell, then auto-advance via getStartedTimer(selectionDwell). Do not
      // hard-jump location in the same transition (feedback_aaef7d51).
      const generation = (updated.selectionDwellGeneration ?? 0) + 1;
      const dwelled: typeof updated = {
        ...updated,
        location: quizLocation,
        selectionDwellGeneration: generation,
      };
      return withPrependedEffects(
        stay(
          snapshotValue,
          withGetStarted(snapshotValue.context, dwelled),
          `event:${event.type}:selectionDwell`,
        ),
        [
          ...(persistEffect ? [persistEffect] : []),
          {
            type: 'getStartedTimer',
            input: {
              phase: 'selectionDwell',
              fromLocation: quizLocation,
              generation,
            },
          },
        ],
      );
    }
    return withPrependedEffect(
      stay(snapshotValue, withGetStarted(snapshotValue.context, updated), `event:${event.type}`),
      persistEffect,
    );
  }

  if (event.type === 'GET_STARTED_CONTINUE') {
    const linear = GET_STARTED_LINEAR_NEXT[location];
    if (linear) {
      return enterState({ getStarted: linear }, withGetStarted(pushGetStartedHistory(snapshotValue.context, location), { ...getStarted, location: linear }), true, [`event:${event.type}`]);
    }
    if (location === 'q1' || location === 'q2' || location === 'q4') {
      const next = nextQuizLocation(location);
      const stepName = getStartedQuizStepName(location);
      // Multi-select questions complete on CONTINUE (not on each toggle).
      return withPrependedEffect(
        enterState({ getStarted: next }, withGetStarted(pushGetStartedHistory(snapshotValue.context, location), { ...getStarted, location: next }), true, [`event:${event.type}`]),
        stepName
          ? analyticsTrackEffect('onboarding_step_completed', { step_name: stepName })
          : null,
      );
    }
    if (location === 'scanResult') {
      const next = getStartedScanBranch(snapshotValue.context);
      return enterState({ getStarted: next }, withGetStarted(snapshotValue.context, { ...getStarted, location: next }), true, [`event:${event.type}`]);
    }
    if (location === 'scan') {
      // Escape hatch for the transient scan beat: if the scan data/effect never
      // settles, Continue leaves the loader without inventing findings. The
      // app shell reloads authoritative state from the backend.
      if (!snapshotValue.context.session) return rejected(snapshotValue, `${event.type}:missingSession`);
      return enterState('authenticatedLoading', snapshotValue.context, true, [`event:${event.type}:scanEscape`]);
    }
    if (location === 'passwordLeaked') {
      return enterState({ getStarted: 'leaksToMonitor' }, withGetStarted(snapshotValue.context, { ...getStarted, location: 'leaksToMonitor' }), true, [`event:${event.type}`]);
    }
    return rejected(snapshotValue, event.type);
  }

  if (event.type === 'GET_STARTED_EMAIL_CHANGED') {
    // T-453: a different email is a new attempt — the existing-account
    // response no longer describes it.
    return stay(snapshotValue, withGetStarted(snapshotValue.context, { ...getStarted, email: event.email, message: null, accountExists: false }), `event:${event.type}`);
  }

  if (event.type === 'GET_STARTED_PASSWORD_CHANGED') {
    return stay(snapshotValue, withGetStarted(snapshotValue.context, { ...getStarted, password: event.password, message: null }), `event:${event.type}`);
  }

  if (event.type === 'GET_STARTED_RUN_SCAN') {
    if (location === 'result') {
      return enterState({ getStarted: 'createAccount' }, withGetStarted(pushGetStartedHistory(snapshotValue.context, 'result'), { ...getStarted, location: 'createAccount' }), true, [`event:${event.type}`]);
    }
    if (location === 'accountAllSet') {
      const session = snapshotValue.context.session;
      if (!session) return rejected(snapshotValue, `${event.type}:missingSession`);
      const context = withGetStarted(
        applyUpdate(snapshotValue.context, update.assignProtectFreeScanStarted, event),
        { ...getStarted, location: 'scan' },
      );
      return result(
        snapshot({ getStarted: 'scan' }, context),
        [
          { type: 'runFreeScan', input: { sessionToken: update.requireStoredSession(session).sessionToken } },
          { type: 'getStartedTimer', input: { phase: 'scan' } },
        ],
        true,
        [`event:${event.type}`],
      );
    }
    return rejected(snapshotValue, event.type);
  }

  if (event.type === 'GET_STARTED_CREATE_ACCOUNT') {
    if (!canSubmitGetStartedAccount(getStarted)) return rejected(snapshotValue, `${event.type}:invalidAccount`);
    // T-453: while the existing-account refusal stands, the CTA is withheld in
    // the view and the machine refuses the event too — only an email change
    // (a new attempt) re-arms the submit.
    if (getStarted.accountExists) return rejected(snapshotValue, `${event.type}:accountExists`);
    const context = withGetStarted(snapshotValue.context, { ...getStarted, location: 'creatingAccount', message: null });
    return enterState({ getStarted: 'creatingAccount' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'GET_STARTED_FINISH') {
    if (location === 'noLeakFound' || location === 'leaksToMonitor') {
      // Route GET_STARTED_FINISH to Protect tab in the main shell.
      // After loadAppState completes, the app will be in commandCenter:ready
      // with Protect as the default selected tab.
      const contextWithProtectTab = { ...snapshotValue.context, selectedTab: 'protect' as const };
      return withPrependedEffect(
        enterState('authenticatedLoading', contextWithProtectTab, true, [`event:${event.type}`]),
        analyticsTrackEffect('onboarding_completed', {}),
      );
    }
    return rejected(snapshotValue, event.type);
  }

  return rejected(snapshotValue, event.type);
}

function getStartedScanBranch(context: MobileAppContext): GetStartedRuntimeLocation {
  const activeFindings = context.appState?.protect.findings.filter((finding) => finding.status !== 'resolved') ?? [];
  if (activeFindings.some((finding) => finding.severity === 'critical' || finding.type === 'password_leak')) {
    return 'passwordLeaked';
  }
  if (activeFindings.length > 0) {
    return 'leaksToMonitor';
  }
  return 'noLeakFound';
}

function transitionForgotPassword(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const forgotPassword = snapshotValue.context.forgotPassword;

  if (event.type === 'FORGOT_PASSWORD_BACK_TO_LOGIN') {
    const context = withForgotPassword(snapshotValue.context, createForgotPasswordRuntimeState({
      location: 'login',
      email: forgotPassword.email,
    }));
    return enterState('unauthenticated', context, true, [`event:${event.type}`]);
  }

  if (event.type === 'FORGOT_PASSWORD_START') {
    const context = withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      location: 'forgotPassword',
      message: null,
    });
    return enterState({ forgotPassword: 'forgotPassword' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'FORGOT_PASSWORD_EMAIL_CHANGED') {
    return stay(snapshotValue, withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      email: event.email,
      message: null,
    }), `event:${event.type}`);
  }

  if (event.type === 'FORGOT_PASSWORD_SUBMIT_EMAIL' || event.type === 'FORGOT_PASSWORD_RESEND_EMAIL') {
    if (!canSubmitResetRequest(forgotPassword)) return rejected(snapshotValue, `${event.type}:invalidEmail`);
    const context = withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      location: 'requestingReset',
      email: forgotPassword.email.trim(),
      message: null,
    });
    return enterState({ forgotPassword: 'requestingReset' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'FORGOT_PASSWORD_OPEN_RESET') {
    const context = withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      location: 'changePassword',
      resetToken: event.token,
      newPassword: '',
      confirmPassword: '',
      message: null,
    });
    return enterState({ forgotPassword: 'changePassword' }, context, true, [`event:${event.type}`]);
  }

  if (event.type === 'FORGOT_PASSWORD_NEW_PASSWORD_CHANGED') {
    return stay(snapshotValue, withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      newPassword: event.password,
      message: null,
    }), `event:${event.type}`);
  }

  if (event.type === 'FORGOT_PASSWORD_CONFIRM_PASSWORD_CHANGED') {
    return stay(snapshotValue, withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      confirmPassword: event.password,
      message: null,
    }), `event:${event.type}`);
  }

  if (event.type === 'FORGOT_PASSWORD_SUBMIT_NEW_PASSWORD') {
    if (!canSubmitNewPassword(forgotPassword)) return rejected(snapshotValue, `${event.type}:invalidPassword`);
    const context = withForgotPassword(snapshotValue.context, {
      ...forgotPassword,
      location: 'resettingPassword',
      message: null,
    });
    return enterState({ forgotPassword: 'resettingPassword' }, context, true, [`event:${event.type}`]);
  }

  return rejected(snapshotValue, event.type);
}

function transitionFirstSession(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  if (event.type === 'REFRESH_APP_STATE') {
    return enterState('authenticatedLoading', snapshotValue.context, true, [`event:${event.type}`]);
  }

  // Family-owner InviteFamily step (§ family-owner-first-session). Owner edits
  // up to two member emails, then sends them to the backend or defers; either
  // way the router falls through to the regular chapters afterwards.
  if (snapshotValue.matches({ firstSession: 'returningBridge' })) {
    if (event.type === 'ONBOARDING_ACKNOWLEDGE_RETURNING') {
      return enterState({ firstSession: 'route' }, markReturningAccountBridgeAcknowledged(snapshotValue.context), true, [`event:${event.type}`]);
    }
    return rejected(snapshotValue, event.type);
  }

  // Family-owner InviteFamily step (§ family-owner-first-session). Owner edits
  // up to two member emails, then sends them to the backend or defers; either
  // way the router falls through to the regular chapters afterwards.
  if (snapshotValue.matches({ firstSession: 'inviteFamily' })) {
    if (event.type === 'FAMILY_INVITE_MEMBER_CHANGED') {
      const familyInvite = setFamilyInviteMemberEmail(snapshotValue.context.familyInvite, event.index, event.email);
      return stay(snapshotValue, withFamilyInvite(snapshotValue.context, familyInvite), `event:${event.type}:${event.index}`);
    }
    if (event.type === 'FAMILY_INVITE_SEND') {
      if (!canSendFamilyInvites(snapshotValue.context.familyInvite)) return rejected(snapshotValue, `${event.type}:noEmails`);
      const emails = validFamilyInviteEmails(snapshotValue.context.familyInvite);
      const sessionToken = update.requireStoredSession(snapshotValue.context.session).sessionToken;
      // Capture WHICH emails this send carries so the success screen names the
      // invited seat(s) even if the user edits the fields while the effect is
      // in flight (InviteFamilySuccess reads `sentEmails`, never the inputs).
      const familyInvite = { ...snapshotValue.context.familyInvite, sentEmails: emails };
      return result(
        snapshot({ firstSession: 'inviteFamily' }, withFamilyInvite(snapshotValue.context, familyInvite)),
        [{ type: 'sendFamilyInvites', input: { sessionToken, emails } }],
        true,
        [`event:${event.type}`],
      );
    }
    if (event.type === 'FAMILY_INVITE_DEFER') {
      const context = markFamilyInvitesHandled(snapshotValue.context);
      return enterState({ firstSession: 'route' }, context, true, [`event:${event.type}`]);
    }
    if (event.type === 'ONBOARDING_BACK') {
      const popped = popHistory(snapshotValue.context);
      if (popped) {
        return enterState({ firstSession: popped.entry.firstSession as FirstSessionStateValue }, popped.context, true, [
          `event:${event.type}:${popped.entry.firstSession}:${popped.entry.chapterScreen ?? '-'}`,
        ], { suppressOnboardingTimer: popped.entry.firstSession === 'deepScan' });
      }
      // Spec predecessor is Login (Welcome → LogIn → InviteFamily). Empty
      // history here is the post-login flow entry; Back returns to sign-in
      // through the same session-clear path as LOGOUT so a stored session
      // cannot bounce the owner straight back into InviteFamily.
      return enterState('clearingSession', snapshotValue.context, true, [`event:${event.type}:login`]);
    }
    return rejected(snapshotValue, event.type);
  }

  // InviteFamilySuccess confirmation (owner 2026-08-07): shown only after
  // `sendFamilyInvites` settled successfully with >=1 invited email. It owns a
  // StickyButton and therefore waits for it — no timer, no auto-advance; the
  // Continue CTA resumes the regular first-session chapters.
  if (snapshotValue.matches({ firstSession: 'inviteFamilySuccess' })) {
    if (event.type === 'FAMILY_INVITE_SUCCESS_CONTINUE') {
      return enterState({ firstSession: 'route' }, snapshotValue.context, true, [`event:${event.type}`]);
    }
    return rejected(snapshotValue, event.type);
  }

  // Back returns to the exact previous step the user saw, popped from the
  // visited-history stack. Works across chapter boundaries (e.g. risk-intro ->
  // welcome, leak-first -> scan-result) without rewinding the backend
  // checkpoint. Rejected only at the very first screen (empty history).
  if (event.type === 'ONBOARDING_BACK') {
    const popped = popHistory(snapshotValue.context);
    // A resumed setup sheet has no visited stack. Dismiss to the nudge
    // backdrop used by the history-free browser setup projection; do not
    // manufacture a prior chapter or leave an undismissable modal behind.
    if (!popped && snapshotValue.matches({ firstSession: 'browser' })
      && snapshotValue.context.onboarding.chapterScreen === 'extensionSetup') {
      return enterState({ firstSession: 'browser' }, withChapterScreen(snapshotValue.context, 'extensionNudge'), true, [`event:${event.type}:sheetBackdrop`]);
    }
    if (!popped) return rejected(snapshotValue, event.type);
    return enterState({ firstSession: popped.entry.firstSession as FirstSessionStateValue }, popped.context, true, [
      `event:${event.type}:${popped.entry.firstSession}:${popped.entry.chapterScreen ?? '-'}`,
    ], { suppressOnboardingTimer: popped.entry.firstSession === 'deepScan' });
  }

  // Welcome CTA persists the risk_intro checkpoint, then resumes routing (§2).
  if (snapshotValue.matches({ firstSession: 'welcome' }) && event.type === 'ONBOARDING_CONTINUE') {
    return enterState({ firstSession: 'advancing' }, withChapterEvent(pushHistory(snapshotValue.context, 'welcome'), 'risk_intro_seen'), true, [`event:${event.type}`]);
  }

  // "Run deep scan" replays the backend-owned first-session findings and, when
  // the backend reports a live scan run, gates the loader on its real lifecycle.
  if (
    (snapshotValue.matches({ firstSession: 'riskIntro' }) || snapshotValue.matches({ firstSession: 'riskUnknownNewMember' }))
    && event.type === 'ONBOARDING_RUN_DEEP_SCAN'
  ) {
    const session = update.requireStoredSession(snapshotValue.context.session);
    const fromScreen = snapshotValue.matches({ firstSession: 'riskUnknownNewMember' }) ? 'riskUnknownNewMember' : 'riskIntro';
    const context = withDeepScanRun(pushHistory(snapshotValue.context, fromScreen), {
      status: 'awaitingProof',
      scanRunId: null,
      floorElapsed: false,
      pollsRemaining: FIRST_SESSION_SCAN_MAX_POLLS,
      // A fresh run starts with no proof: whatever an earlier attempt learned
      // is not evidence about THIS one (T-209 / pass-4 CRITICAL 2). It also
      // moves off `not-attempted`, which withdraws the resumed-session fallback
      // to the backend's completion record — a cached record of an older scan
      // must not re-prove the run the user is watching right now (pass-5
      // CRITICAL 1).
      attestation: 'attempted',
    });
    return withPrependedEffect(
      result(
        snapshot({ firstSession: 'deepScan' }, context),
        [
          { type: 'checkPhoneSecurityProofs', input: { sessionToken: session.sessionToken, includeAppUpdateCheck: false } },
          { type: 'onboardingTimer', input: { phase: 'deepScan' } },
        ],
        true,
        [`event:${event.type}`],
      ),
      analyticsTrackEffect('onboarding_step_completed', { step_name: 'risk_intro' }),
    );
  }

  // "Review findings" advances the backend into the leaks chapter (§3 -> §4).
  // The generic forward-signal aliases are accepted so the scan result —
  // including the no-leaks variant — can never be stuck behind an unmatched
  // CTA event (feedback: scan-result-no-leaks had no matching forward signal).
  if (
    snapshotValue.matches({ firstSession: 'scanResult' }) &&
    (event.type === 'ONBOARDING_REVIEW_FINDINGS' || event.type === 'ONBOARDING_ADVANCE_SCREEN' || event.type === 'ONBOARDING_CONTINUE')
  ) {
    return withPrependedEffect(
      enterState({ firstSession: 'advancing' }, withChapterEvent(pushHistory(snapshotValue.context, 'scanResult'), 'leaks_reviewed'), true, [`event:${event.type}`]),
      analyticsTrackEffect('onboarding_step_completed', { step_name: 'scan_result' }),
    );
  }

  if (snapshotValue.matches({ firstSession: 'leak' })) {
    return transitionLeak(snapshotValue, event);
  }

  if (snapshotValue.matches({ firstSession: 'spotFake' })) {
    return transitionSpotFake(snapshotValue, event);
  }

  if (snapshotValue.matches({ firstSession: 'browser' })) {
    return transitionBrowser(snapshotValue, event);
  }

  if (snapshotValue.matches({ firstSession: 'device' })) {
    return transitionDevice(snapshotValue, event);
  }

  if (snapshotValue.matches({ firstSession: 'postFlow' })) {
    return transitionPostFlow(snapshotValue, event);
  }

  return rejected(snapshotValue, event.type);
}

/**
 * Leak chapter (§4–§7). Screens advance with ONBOARDING_ADVANCE_SCREEN; the
 * fix flow creates a Protect change-password task. When both critical and
 * non-critical leaks exist the actionable flow always runs first, then the
 * monitoring flow (fixed order, §4). When the chapter ends the backend
 * checkpoint advances into spot-fake.
 */
function transitionLeak(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const screen = snapshotValue.context.onboarding.chapterScreen;

  if (event.type === 'ONBOARDING_CREATE_TASK') {
    return stay(snapshotValue, withCreatedTask(snapshotValue.context, event.taskId), `event:${event.type}`);
  }

  if (event.type !== 'ONBOARDING_ADVANCE_SCREEN') return rejected(snapshotValue, event.type);

  const desired = nextLeakScreen(screen, snapshotValue.context);
  // T-566: password saved / monitoring set are completed sections — the beat
  // always paints, one demonstrative step left (owner 2026-09-13).
  const navigated = desired === 'riskDrop' || desired === 'monitorRiskDrop'
    ? takeDemonstrativeRiskDropBeat(snapshotValue.context, desired, desired === 'riskDrop' ? 'password' : 'monitor')
    : { context: snapshotValue.context, screen: desired };
  const next = navigated.screen;
  // Leaving a beat for a non-beat screen drops the claim; Back re-stamps it.
  const ctx = pushHistory(
    next === 'riskDrop' || next === 'monitorRiskDrop' ? navigated.context : clearActiveRiskDropClaim(navigated.context),
    'leak',
  );
  if (next === 'advance') {
    return withPrependedEffect(
      enterState({ firstSession: 'advancing' }, withChapterEvent(clearActiveRiskDropClaim(ctx), 'spot_fake_completed'), true, [`event:${event.type}:chapterDone`]),
      analyticsTrackEffect('onboarding_step_completed', { step_name: 'leaks' }),
    );
  }
  return enterState({ firstSession: 'leak' }, withChapterScreen(ctx, next), true, [`event:${event.type}:${next}`]);
}

/**
 * Spot-the-fake quiz (§8.1). Intro -> quiz -> reveal (with answer) -> result,
 * then advance into the browser chapter. The answer is recorded but does not
 * change downstream routing (browser is independent, §8.2).
 */
function transitionSpotFake(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const screen = snapshotValue.context.onboarding.chapterScreen;

  if (event.type === 'ONBOARDING_ANSWER_QUIZ' && screen === 'quiz') {
    return enterState({ firstSession: 'spotFake' }, withSpotFakeAnswer(pushHistory(snapshotValue.context, 'spotFake'), event.correct), true, [`event:${event.type}:${event.correct ? 'right' : 'missed'}`]);
  }

  if (event.type !== 'ONBOARDING_ADVANCE_SCREEN') return rejected(snapshotValue, event.type);

  // Intro → quiz goes through `advancing` so the sticky CTA paints Figma's
  // loading state while the chapter checkpoint is in flight (T-209 D6). The
  // destination surface stays Intro (Start quick check) until the effect
  // settles; `chapterScreen: 'quiz'` is the post-advance resume target.
  if (screen === 'intro') {
    return enterState(
      { firstSession: 'advancing' },
      withChapterEvent(withChapterScreen(pushHistory(snapshotValue.context, 'spotFake'), 'quiz'), 'spot_fake_completed'),
      true,
      [`event:${event.type}:quiz`],
    );
  }
  if (screen === 'reveal') return enterState({ firstSession: 'spotFake' }, withChapterScreen(pushHistory(snapshotValue.context, 'spotFake'), 'result'), true, [`event:${event.type}:result`]);
  if (screen === 'result') {
    return withPrependedEffect(
      enterState({ firstSession: 'advancing' }, withChapterEvent(pushHistory(snapshotValue.context, 'spotFake'), 'browser_reviewed'), true, [`event:${event.type}:chapterDone`]),
      analyticsTrackEffect('onboarding_step_completed', { step_name: 'spot_fake' }),
    );
  }
  return rejected(snapshotValue, event.type);
}

/**
 * Browser / extension chapter (§8.2). Intro offers set up or skip. Setting up
 * polls the activation port; activation -> extensionOn -> riskDrop -> advance.
 * Skipping shows the nudge; saving for later creates a Protect task and
 * advances without a risk drop.
 */
function transitionBrowser(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const screen = snapshotValue.context.onboarding.chapterScreen;

  if (event.type === 'ONBOARDING_SET_UP_EXTENSION' && (screen === 'extensionIntro' || screen === 'extensionNudge')) {
    // Entering setup arms the activation poll (handled in enterState).
    return enterState({ firstSession: 'browser' }, withChapterScreen(pushHistory(snapshotValue.context, 'browser'), 'extensionSetup'), true, [`event:${event.type}`]);
  }

  if (event.type === 'ONBOARDING_SKIP_EXTENSION' && screen === 'extensionIntro') {
    return enterState({ firstSession: 'browser' }, withChapterScreen(pushHistory(snapshotValue.context, 'browser'), 'extensionNudge'), true, [`event:${event.type}`]);
  }

  if ((event.type === 'ONBOARDING_CHECK_EXTENSION_ACTIVATION' || event.type === 'ONBOARDING_EXTENSION_ACTIVATED') && screen === 'extensionSetup') {
    return enterState({ firstSession: 'browser' }, withChapterScreen(snapshotValue.context, 'extensionSetup'), true, [`event:${event.type}:check`]);
  }

  // Returning to the app during the browser chapter re-runs the activation
  // proof check automatically, so a user who just enabled the extension in
  // Settings advances without hunting for a manual re-check (feedback: detect
  // activation on foreground). The manual re-check path above stays intact.
  if (
    event.type === 'APP_FOREGROUNDED' &&
    (screen === 'extensionIntro' || screen === 'extensionNudge' || screen === 'extensionSetup')
  ) {
    return result(
      snapshot(snapshotValue.value, snapshotValue.context),
      [{ type: 'checkExtensionActivation', input: { trigger: 'onboarding', sessionToken: snapshotValue.context.session?.sessionToken } }],
      true,
      [`event:${event.type}:recheck`],
    );
  }

  if (
    (event.type === 'ONBOARDING_SAVE_EXTENSION_FOR_LATER' &&
      (screen === 'extensionIntro' || screen === 'extensionNudge' || screen === 'extensionSetup')) ||
    // Explicit skip from the setup-steps screen takes the same documented
    // save-for-later machinery so the modal can never dead-end (feedback:
    // "no way forward from browser-nudge-steps / add a skip").
    (event.type === 'ONBOARDING_SKIP_EXTENSION' && screen === 'extensionSetup')
  ) {
    // Creates the activate-browser Protect task and SHOWS that it was saved
    // (§8.2). The catalog and Figma both author `browser-saved` for exactly
    // this beat, but the chapter used to jump straight to `advancing`, so the
    // user tapped "Save it for later" and landed in the phone chapter with no
    // confirmation that anything had been saved (owner 2026-08-05). The
    // chapter completes from that screen, not from the tap.
    const withTask = withCreatedTask(pushHistory(snapshotValue.context, 'browser'), 'task.onboarding.activateBrowserProtection');
    return enterState({ firstSession: 'browser' }, withChapterScreen(withTask, 'savedForLater'), true, [`event:${event.type}:savedForLater`]);
  }

  if (event.type === 'ONBOARDING_ADVANCE_SCREEN') {
    if (screen === 'savedForLater') {
      return withPrependedEffect(
        enterState({ firstSession: 'advancing' }, withChapterEvent(pushHistory(snapshotValue.context, 'browser'), 'device_reviewed'), true, [`event:${event.type}:chapterDone`]),
        analyticsTrackEffect('onboarding_step_completed', { step_name: 'browser' }),
      );
    }
    if (screen === 'extensionOn') {
      // T-566: the extension is on — a completed section, so the beat always
      // paints one demonstrative step left (owner 2026-09-13; T-275 server
      // gate stays only for the in-app Protect beat).
      const navigated = takeDemonstrativeRiskDropBeat(snapshotValue.context, 'riskDrop', 'extension');
      return enterState({ firstSession: 'browser' }, withChapterScreen(pushHistory(navigated.context, 'browser'), 'riskDrop'), true, [`event:${event.type}:riskDrop`]);
    }
    if (screen === 'riskDrop') {
      return withPrependedEffect(
        enterState({ firstSession: 'advancing' }, withChapterEvent(clearActiveRiskDropClaim(pushHistory(snapshotValue.context, 'browser')), 'device_reviewed'), true, [`event:${event.type}:chapterDone`]),
        analyticsTrackEffect('onboarding_step_completed', { step_name: 'browser' }),
      );
    }
  }

  return rejected(snapshotValue, event.type);
}

/**
 * Device chapter (§9). If the funnel reports an outdated iOS the chapter runs
 * intro -> outdatedOs (creates an update-iOS Protect task) -> saved -> riskDrop;
 * an up-to-date iOS runs intro -> updatedOs -> backend advance.
 *
 * T-236: the up-to-date path used to jump straight from `intro` to `advancing`,
 * so "Last thing. The phone itself" promised a phone check and then silently
 * skipped the whole chapter — the user never saw the result of the check they
 * were just promised. The owner drew the answer (Figma 6378:19862 "Updated iOS",
 * 2026-08-05): the chapter now REPORTS the clear outcome instead of swallowing
 * it. The analytics payload is unchanged and simply moves one screen later —
 * `device_check_completed {outcome:'clear'}` + `onboarding_step_completed` still
 * fire exactly once per clear run, on the hop that actually leaves the chapter.
 */
function transitionDevice(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const screen = snapshotValue.context.onboarding.chapterScreen;

  if (event.type !== 'ONBOARDING_ADVANCE_SCREEN') return rejected(snapshotValue, event.type);

  if (screen === 'intro') {
    if (!isDeviceOutdated(snapshotValue.context)) {
      return enterState({ firstSession: 'device' }, withChapterScreen(pushHistory(snapshotValue.context, 'device'), 'updatedOs'), true, [`event:${event.type}:updatedOs`]);
    }
    return enterState({ firstSession: 'device' }, withChapterScreen(pushHistory(snapshotValue.context, 'device'), 'outdatedOs'), true, [`event:${event.type}:outdatedOs`]);
  }
  if (screen === 'updatedOs') {
    // Carries the former §9-skip payload forward verbatim. NOT timed: the frame
    // authors its own StickyButton ("Got it", 6378:19873), and a screen that
    // owns a control waits for that control — same rule as `monitoringSet` /
    // `extensionOn` in firstSessionTimedScreens below.
    // T-567 (review-2 C1b): the recap is already mounted while the
    // post_flow_reached checkpoint is in flight, so its demonstrative claim
    // is stamped BEFORE advancing (re-reconciled with the server on settle).
    return withPrependedEffects(
      enterState({ firstSession: 'advancing' }, withChapterEvent(takePostflowRiskLoweredClaim(pushHistory(snapshotValue.context, 'device')), 'post_flow_reached'), true, [`event:${event.type}:skip`]),
      [
        analyticsTrackEffect('device_check_completed', { outcome: 'clear' }),
        analyticsTrackEffect('onboarding_step_completed', { step_name: 'device' }),
      ],
    );
  }
  if (screen === 'outdatedOs') {
    const withTask = withCreatedTask(pushHistory(snapshotValue.context, 'device'), 'task.onboarding.updateIos');
    return withPrependedEffect(
      enterState({ firstSession: 'device' }, withChapterScreen(withTask, 'saved'), true, [`event:${event.type}:saved`]),
      analyticsTrackEffect('device_check_completed', { outcome: 'outdated' }),
    );
  }
  if (screen === 'saved') {
    // T-566: the update-iOS task is saved — a completed section, so the beat
    // always paints one demonstrative step left (owner 2026-09-13).
    const navigated = takeDemonstrativeRiskDropBeat(snapshotValue.context, 'riskDrop', 'device');
    return enterState({ firstSession: 'device' }, withChapterScreen(pushHistory(navigated.context, 'device'), 'riskDrop'), true, [`event:${event.type}:riskDrop`]);
  }
  if (screen === 'riskDrop') {
    // T-567 (review-2 C1b): stamp the recap's demonstrative claim before
    // advancing — the recap paints during the checkpoint round trip.
    return withPrependedEffect(
      enterState({ firstSession: 'advancing' }, withChapterEvent(takePostflowRiskLoweredClaim(pushHistory(snapshotValue.context, 'device')), 'post_flow_reached'), true, [`event:${event.type}:chapterDone`]),
      analyticsTrackEffect('onboarding_step_completed', { step_name: 'device' }),
    );
  }
  return rejected(snapshotValue, event.type);
}

/**
 * Post-flow handoff (§10). riskLowered -> preparingProfile, and PreparingProfile
 * itself completes onboarding so loadAppState routes to the shell.
 *
 * AllSet is gone (T-253, owner 2026-08-07: «екран AllSet взагалі прибираємо він
 * виглядає зайвим кроком, на фігмі теж видаляю його»). It was a pure
 * acknowledgement between the profile beat and the Protect tab, and the owner
 * removed it from Figma too — only the FREE-funnel `account-all-set` stays
 * («у безкоштовній воронці хай буде»), which is a different screen.
 *
 * Its two analytics events are NOT dropped with it: `onboarding_step_completed`
 * (post_flow) and `onboarding_completed` fired on AllSet's "Go to Protect", so
 * they move onto the beat that now ends the chapter. Losing them would silently
 * break onboarding funnel reporting, which no screenshot would ever show.
 */
function transitionPostFlow(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const screen = snapshotValue.context.onboarding.chapterScreen;

  if (event.type !== 'ONBOARDING_ADVANCE_SCREEN') return rejected(snapshotValue, event.type);

  if (screen === 'riskLowered') {
    // T-275: one-shot claim — clear activeRiskDropClaim and do NOT push
    // riskLowered into back-history (Back must not replay the same claim).
    return enterState(
      { firstSession: 'postFlow' },
      withChapterScreen(clearActiveRiskDropClaim(snapshotValue.context), 'preparingProfile'),
      true,
      [`event:${event.type}:preparingProfile`],
    );
  }
  if (screen === 'preparingProfile') {
    return withPrependedEffects(
      enterState({ firstSession: 'advancing' }, withChapterEvent(snapshotValue.context, 'onboarding_completed'), true, [`event:${event.type}:completed`]),
      [
        analyticsTrackEffect('onboarding_step_completed', { step_name: 'post_flow' }),
        analyticsTrackEffect('onboarding_completed', {}),
      ],
    );
  }
  return rejected(snapshotValue, event.type);
}

/**
 * Theatrical first-session beats and the timer phase that governs each
 * (behavior model `timer.onboarding.*` transitions; G1/G13). Each entry gets
 * a fresh generation so Back can re-arm the full dwell without allowing the
 * abandoned timer completion to settle the restored state.
 */
const firstSessionTimedScreens: Partial<Record<
  Extract<FirstSessionStateValue, 'leak' | 'spotFake' | 'browser' | 'device' | 'postFlow'>,
  Partial<Record<OnboardingChapterScreen, OnboardingTimerPhase>>
>> = {
  leak: {
    saved: 'success',
    riskDrop: 'riskDrop',
    monitorDetails: 'monitorSetup',
    // `monitoringSet` is deliberately NOT timed. The owner asked for a CTA on
    // the body-copy confirmations (2026-08-06, refs 1070-18494 / 1070-18503),
    // and a screen with its own control waits for that control — the same rule
    // already applied to spot-fake Intro B and the post-flow recap. Its
    // Continue dispatches ONBOARDING_ADVANCE_SCREEN into `nextLeakScreen`.
    monitorRiskDrop: 'riskDrop',
  },
  browser: {
    // `extensionOn` is deliberately NOT timed either — same owner decision,
    // same rule; its Continue dispatches ONBOARDING_ADVANCE_SCREEN, which the
    // browser transition already routes to `riskDrop`.
    // Save-for-later confirmation. Figma authors it without a StickyButton, so
    // it is a control-less beat on the shared budget, and its authored copy
    // reaches the reading threshold — reading dwell, not a private duration.
    savedForLater: 'successWithBody',
    riskDrop: 'riskDrop',
  },
  device: {
    saved: 'success',
    riskDrop: 'riskDrop',
  },
  postFlow: {
    // `riskLowered` is deliberately NOT timed. It already carries a visible
    // Continue button plus a progress -> cards -> confetti choreography, and
    // arming a timer on top of that moved the user off the recap they were
    // still reading (owner 2026-08-05). A screen with its own control waits
    // for that control — same rule as the spot-fake intros.
    preparingProfile: 'preparingProfile',
  },
  spotFake: {
    // `intro` deliberately has no timed phase: both intro variants advance on
    // the user's tap (owner 2026-08-05). Only the answer reveal is timed.
    reveal: 'spotFakeReveal',
  },
};

function firstSessionTimerPhase(
  value: MobileAppStateValue,
  screen: OnboardingChapterScreen | null,
  context: MobileAppContext,
): OnboardingTimerPhase | null {
  if (!isFirstSessionState(value) || !screen) return null;
  if (value.firstSession === 'spotFake' && screen === 'intro') {
    // Owner decision (2026-08-05): Intro B is NOT an auto-advancing beat.
    // Making it one also removed its Figma StickyButton (1070:18617), so the
    // no-leak path silently moved the user off an explainer they never
    // dismissed. Both intro variants now wait for the same explicit tap.
    return null;
  }
  if (value.firstSession === 'spotFake' && screen === 'reveal') {
    return 'spotFakeReveal';
  }
  const chapter = value.firstSession as keyof typeof firstSessionTimedScreens;
  return firstSessionTimedScreens[chapter]?.[screen] ?? null;
}

function isOnboardingTimerPhase(value: unknown): value is OnboardingTimerPhase {
  return value === 'deepScan'
    || value === 'success'
    || value === 'successWithBody'
    || value === 'riskDrop'
    || value === 'riskLowered'
    || value === 'monitorSetup'
    || value === 'preparingProfile'
    || value === 'spotFakeIntro'
    || value === 'spotFakeReveal';
}

function onboardingTimerPhaseFromFailure(result: MobileAppEffectFailure): OnboardingTimerPhase | null {
  const input = result.input;
  if (input && typeof input === 'object' && 'phase' in input && isOnboardingTimerPhase(input.phase)) {
    return input.phase;
  }
  return null;
}

/**
 * Timer elapsed. Advances only the exact (chapter, screen) the phase was
 * armed for; stale timers (user advanced manually or went back) are ignored.
 */
function settleOnboardingTimerElapsed(
  snapshotValue: MobileAppSnapshot,
  phase: OnboardingTimerPhase,
  effectType: string,
  generation?: number,
): MobileAppTransitionResult {
  if (phase === 'deepScan') {
    if (!snapshotValue.matches({ firstSession: 'deepScan' })) return rejected(snapshotValue, `effect:${effectType}`);
    if (snapshotValue.context.onboarding.deepScanRun.status !== 'settled') {
      // The theatrical floor elapsed but the real scan run is still live: hold
      // the loader on backend data — liveness is carried by the armed poll
      // chain, which advances on terminal or at its hard ceiling (task 11).
      return stay(snapshotValue, withDeepScanRun(snapshotValue.context, { floorElapsed: true }), `effect:${effectType}:deepScanFloorHold`);
    }
    return advanceDeepScanCompleted(withDeepScanRun(snapshotValue.context, { floorElapsed: true }), [`effect:${effectType}:deepScan`]);
  }

  const expected = firstSessionTimerPhase(snapshotValue.value, snapshotValue.context.onboarding.chapterScreen, snapshotValue.context);
  const currentGeneration = snapshotValue.context.onboarding.onboardingTimerGeneration;
  if (expected !== phase || generation === undefined || generation !== currentGeneration) {
    return { snapshot: snapshotValue, effects: [], accepted: false, trace: [`rejected:${effectType}:${phase}:generation-${generation ?? 'missing'}`] };
  }

  if (!isFirstSessionState(snapshotValue.value)) {
    return { snapshot: snapshotValue, effects: [], accepted: false, trace: [`rejected:${effectType}:${phase}:state`] };
  }

  const advance = { type: 'ONBOARDING_ADVANCE_SCREEN' } as const;
  switch (snapshotValue.value.firstSession) {
    case 'leak': return transitionLeak(snapshotValue, advance);
    case 'browser': return transitionBrowser(snapshotValue, advance);
    case 'device': return transitionDevice(snapshotValue, advance);
    case 'spotFake': return transitionSpotFake(snapshotValue, advance);
    case 'postFlow': return transitionPostFlow(snapshotValue, advance);
    default: return { snapshot: snapshotValue, effects: [], accepted: false, trace: [`rejected:${effectType}:${phase}:state`] };
  }
}

/**
 * Next screen in the leak chapter, or 'advance' when the chapter is complete.
 * Actionable (password) flow: leaked -> fix -> saved -> riskDrop, then either
 * the monitoring flow (if non-critical leaks remain) or chapter end. Monitoring
 * flow (§6): monitorIntro -> leaksToMonitor -> monitorDetails -> monitoringSet
 * -> monitorRiskDrop -> end. `monitorDetails` is the "Setting up monitoring..."
 * data-bearing screen; its setup beat is settled by the machine-owned timer.
 * No-leak: single success screen -> end.
 */
function nextLeakScreen(
  screen: OnboardingChapterScreen | null,
  context: MobileAppContext,
): OnboardingChapterScreen | 'advance' {
  switch (screen) {
    case 'leaked': return 'fix';
    case 'fix': return 'saved';
    case 'saved': return 'riskDrop';
    case 'riskDrop': return hasPendingMonitoring(context) ? 'monitorIntro' : 'advance';
    case 'monitorIntro': return 'leaksToMonitor';
    case 'leaksToMonitor': return 'monitorDetails';
    case 'monitorDetails': return 'monitoringSet';
    case 'monitoringSet': return 'monitorRiskDrop';
    case 'monitorRiskDrop': return 'advance';
    case 'noLeakFound': return 'advance';
    default: return 'advance';
  }
}

/**
 * T-566: one completed onboarding section → the Risk dropped beat, always.
 * The claim carries the demonstrative endpoints (ghost = where the run was,
 * handle = one equal step left) so App.tsx / the projected indicator render
 * exactly as they did for a server-proven claim. The T-275 server pair
 * (`pendingRiskDrop` / `consumePendingRiskDrop`) is NOT consulted in the first
 * session any more — the owner's 2026-09-13 rule makes onboarding
 * demonstrative; the in-app Protect `taskResolution/riskDropped` beat keeps
 * the proven pair (see `provenRiskDrop` below).
 */
function takeDemonstrativeRiskDropBeat(
  context: MobileAppContext,
  desired: 'riskDrop' | 'monitorRiskDrop',
  section: DemonstrativeRiskSection,
): { context: MobileAppContext; screen: 'riskDrop' | 'monitorRiskDrop' } {
  const demonstrativeRisk = advanceDemonstrativeRisk(context.onboarding.demonstrativeRisk, context.onboarding, section);
  return {
    context: withSectionClaim({ ...context, onboarding: { ...context.onboarding, demonstrativeRisk } }, section),
    screen: desired,
  };
}

/** Stamp the section's stored claim (one step, replayed verbatim on every visit). */
function withSectionClaim(context: MobileAppContext, section: DemonstrativeRiskSection): MobileAppContext {
  const claim = context.onboarding.demonstrativeRisk?.sections[section];
  if (!claim) return context;
  return {
    ...context,
    onboarding: {
      ...context.onboarding,
      activeRiskDropClaim: {
        from: claim.from.status,
        to: claim.to.status,
        fromHandle: claim.from.handlePosition,
        toHandle: claim.to.handlePosition,
        correlationId: `demo:${section}:${claim.step}`,
      },
    },
  };
}

/** Which section a mounted beat screen belongs to (Back lands here with no claim). */
function demonstrativeSectionForBeat(
  value: MobileAppStateValue,
  screen: OnboardingChapterScreen | null,
): DemonstrativeRiskSection | null {
  if (!isFirstSessionState(value)) return null;
  if (value.firstSession === 'leak' && screen === 'riskDrop') return 'password';
  if (value.firstSession === 'leak' && screen === 'monitorRiskDrop') return 'monitor';
  if (value.firstSession === 'browser' && screen === 'riskDrop') return 'extension';
  if (value.firstSession === 'device' && screen === 'riskDrop') return 'device';
  return null;
}

/**
 * T-567: the postflow recap claim = the whole run (start → current). A run
 * that completed no section (no leaks, extension skipped, iOS current) still
 * gets the recap: the ghost and the handle coincide at the start endpoint and
 * the cards carry the real session facts.
 */
function takePostflowRiskLoweredClaim(context: MobileAppContext): MobileAppContext {
  const existing = context.onboarding.demonstrativeRisk
    ?? ((): NonNullable<MobileAppContext['onboarding']['demonstrativeRisk']> => {
      const start = demonstrativeRiskStart(context.onboarding);
      return { start, current: start, steps: 0, sections: {} };
    })();
  // The recap never paints above the latest server projection (a real
  // resolve during the run is honoured), and never below the demonstration.
  const current = reconcileDemonstrativeRiskWithServer(existing.current, {
    status: context.onboarding.lastObservedServerRiskStatus,
    handlePosition: context.onboarding.lastObservedServerRiskHandle,
  });
  const run = { ...existing, current };
  return {
    ...context,
    onboarding: {
      ...context.onboarding,
      demonstrativeRisk: run,
      activeRiskDropClaim: {
        from: run.start.status,
        to: run.current.status,
        fromHandle: run.start.handlePosition,
        toHandle: run.current.handlePosition,
        correlationId: `demo:postflow:${run.steps}`,
      },
    },
  };
}

function clearActiveRiskDropClaim(context: MobileAppContext): MobileAppContext {
  if (!context.onboarding.activeRiskDropClaim) return context;
  return {
    ...context,
    onboarding: {
      ...context.onboarding,
      activeRiskDropClaim: null,
    },
  };
}

/**
 * Only iOS-version truth drives the iOS-specific device chapter. Aggregate
 * Device issues also include passcode and other independent checks; treating
 * their count as an outdated-iOS verdict creates the wrong remediation task.
 * Before authenticated app state is available, retain the legacy funnel
 * fallback because its device signal is the only compatible iOS evidence.
 */
function isDeviceOutdated(context: MobileAppContext): boolean {
  if (context.appState) {
    return deviceHealthSnapshotForAppState(context.appState).ios.verdict === 'outdated';
  }
  return (scanOutcomeCounts(firstSessionScanOutcomeFor(context))?.deviceIssueCount ?? 0) > 0;
}

type ProtectEvent = Exclude<
  Extract<MobileAppEvent, { type: `PROTECT_${string}` }>,
  { type: `PROTECT_FREE_${string}` }
>;

type ForgotPasswordEvent = Extract<MobileAppEvent, { type: `FORGOT_PASSWORD_${string}` }>;

function isProtectEvent(event: MobileAppEvent): event is ProtectEvent {
  return event.type.startsWith('PROTECT_') && !event.type.startsWith('PROTECT_FREE_');
}

function isForgotPasswordEvent(event: MobileAppEvent): event is ForgotPasswordEvent {
  return event.type.startsWith('FORGOT_PASSWORD_');
}

function transitionProtect(snapshotValue: MobileAppSnapshot, event: ProtectEvent): MobileAppTransitionResult {
  if (!snapshotValue.matches({ commandCenter: 'ready' })) return rejected(snapshotValue, event.type);

  const protect = snapshotValue.context.protect;
  const runtime = protect.runtime;

  switch (event.type) {
    case 'PROTECT_BACK': {
      let previous = popProtectHistory(protect);
      if (!previous) return rejected(snapshotValue, event.type);
      // T-449 (owner): dismissing a task-resolution dialog (e.g. "No, I'll do
      // it later" on the change-password confirm) closes the WHOLE task-sheet
      // stack — Back lands on the To-Do list with the task still open, never
      // on the intermediate task-detail sheet.
      if (protect.location.name === 'taskResolution') {
        // T-458: the guided-answer / remove / escalate hops each push a
        // taskResolution entry — dismissing still closes the WHOLE stack.
        while (previous.location.name === 'taskDetail' || previous.location.name === 'taskResolution') {
          const deeper = popProtectHistory(previous);
          if (!deeper) break;
          previous = deeper;
        }
      }
      return enterState({ commandCenter: 'ready' }, { ...snapshotValue.context, protect: previous }, true, [`event:${event.type}`]);
    }

    case 'PROTECT_OPEN_TODO':
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, protectTodoLocation(runtime)), true, [`event:${event.type}`]);

    case 'PROTECT_SWITCH_TODO_TAB': {
      const nextRuntime = { ...runtime, todoTab: event.tab };
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
        ...protect,
        runtime: nextRuntime,
        location: protectTodoLocation(nextRuntime),
      }), true, [`event:${event.type}:${event.tab}`]);
    }

    case 'PROTECT_OPEN_TASK_DETAIL': {
      // A production press always carries the exact backend task ID. Falling
      // back to "first critical" made equal-severity cards nondeterministic
      // and could resolve a different finding than the one the user tapped.
      const selected = runtime.tasks.find((task) => task.id === event.taskId);
      if (!selected) return rejected(snapshotValue, `${event.type}:missingTask`);
      const variant = deriveTaskDetailVariant(selected);
      if (variant !== event.taskState) return rejected(snapshotValue, `${event.type}:taskStateMismatch`);
      // T-299: fire-and-forget local opened persistence (pure state stays sync).
      rememberProtectTaskOpenedId(selected.id);
      // A new OPEN task flow owns a fresh resolved-sheet flavor; reopening a
      // resolved sheet must keep the session's work/school flavor (T-458).
      const nextRuntime = selected.status === 'open'
        ? { ...markProtectTaskOpened(runtime, selected.id), deviceReviewResolvedVariant: null }
        : markProtectTaskOpened(runtime, selected.id);
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
          ...pushProtectHistory(protect),
          runtime: nextRuntime,
          location: { name: 'taskDetail', variant },
        }), true, [`event:${event.type}:${variant}`]),
        analyticsTrackEffect('protect_task_opened', { task_kind: protectTaskKind(selected) }),
      );
    }

    case 'PROTECT_SEE_FIX': {
      const selectedTask = runtime.selectedTaskId
        ? runtime.tasks.find((t) => t.id === runtime.selectedTaskId)
        : null;
      const isDeviceReview = selectedTask?.taskType === 'review_device_management';
      const fromCriticalDetail = protect.location.name === 'taskDetail' && protect.location.variant === 'critical';
      // T-449 (owner regression, warning Acme sheet): change_password is the
      // SAME two-sheet flow at every open severity — the warning-level leak
      // detail must enter the confirm dialog too, not silently reject.
      const fromPasswordLeakDetail = selectedTask?.taskType === 'change_password'
        && protect.location.name === 'taskDetail'
        && protect.location.variant !== 'resolved';
      // T-458 (task-6 flow): the question sheet must be reachable from the
      // open Monthly-check detail in EVERY open variant (the device task is
      // usually nonCritical), and again after the remove-branch Settings hop
      // so the loop closes through "See anything you didn't add?".
      const fromDeviceReviewDetail = isDeviceReview
        && protect.location.name === 'taskDetail'
        && protect.location.variant !== 'resolved';
      const fromDeviceReviewResolving = isDeviceReview
        && protect.location.name === 'taskResolution'
        && (protect.location.screen === 'resolving' || protect.location.screen === 'escalating');
      if (!fromCriticalDetail && !fromPasswordLeakDetail && !fromDeviceReviewDetail && !fromDeviceReviewResolving) {
        return rejected(snapshotValue, event.type);
      }
      // task-6 (review device management) skips generic resolving and goes straight to guided answer
      const resolutionScreen = isDeviceReview ? 'guidedAnswer' : 'resolving';
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'taskResolution', screen: resolutionScreen }), true, [`event:${event.type}`]);
    }

    case 'PROTECT_CONFIRM_RESOLVED': {
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      const effect = createProtectTaskResolutionEffect(snapshotValue.context, taskId, 'resolved');
      if (!effect) return rejected(snapshotValue, `${event.type}:missingBackendEffect`);
      return result(snapshotValue, [effect], true, [`event:${event.type}`]);
    }

    // archetype: redirectConfirm — user confirmed ("Yes, I've changed it") → resolve
    case 'PROTECT_TASK_CONFIRMED': {
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      const effect = createProtectTaskResolutionEffect(snapshotValue.context, taskId, 'resolved');
      if (!effect) return rejected(snapshotValue, `${event.type}:missingBackendEffect`);
      return result(snapshotValue, [effect], true, [`event:${event.type}`]);
    }

    // archetype: guidedAnswer — branch from task-6 OptionRowGroup answer
    case 'PROTECT_TASK_GUIDED_ANSWER': {
      // The question sheet has exactly two options (Figma 2911:39547);
      // "This is my work or school phone" lives on the escalate sheet (T-458).
      const fromQuestion = protect.location.name === 'taskResolution'
        && protect.location.screen === 'guidedAnswer'
        && event.branch !== 'workSchool';
      const fromEscalate = protect.location.name === 'taskResolution'
        && protect.location.screen === 'escalating'
        && event.branch === 'workSchool';
      if (!fromQuestion && !fromEscalate) return rejected(snapshotValue, event.type);
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      if (event.branch === 'familiar' || event.branch === 'workSchool') {
        // Both "looks familiar" and "work/school" resolve the task after backend persistence.
        const effect = createProtectTaskResolutionEffect(snapshotValue.context, taskId, 'resolved');
        if (!effect) return rejected(snapshotValue, `${event.type}:missingBackendEffect`);
        // The resolved sheet copy differs per branch (work/school pin) — the
        // flavor survives the resolution rehydrate via reconcileProtectTaskResolution.
        const flavoredContext = withProtect(snapshotValue.context, {
          ...protect,
          runtime: {
            ...runtime,
            deviceReviewResolvedVariant: event.branch === 'workSchool' ? 'workSchool' as const : null,
          },
        });
        return result(snapshot(snapshotValue.value, flavoredContext), [effect], true, [`event:${event.type}:${event.branch}`]);
      }
      // "Something I didn't add" → go to resolving step (remove/escalate sub-flow)
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'taskResolution', screen: 'resolving' }), true, [`event:${event.type}:${event.branch}`]);
    }

    // task-6 remove sheet: "It says my phone is “managed”" → escalate sheet.
    case 'PROTECT_TASK_ESCALATE': {
      const selectedTask = runtime.selectedTaskId
        ? runtime.tasks.find((t) => t.id === runtime.selectedTaskId)
        : null;
      const fromRemoveSheet = protect.location.name === 'taskResolution'
        && protect.location.screen === 'resolving'
        && selectedTask?.taskType === 'review_device_management';
      if (!fromRemoveSheet) return rejected(snapshotValue, event.type);
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'taskResolution', screen: 'escalating' }), true, [`event:${event.type}`]);
    }

    // Escalate / still-seeing sheets: "Contact Apple Support" is an external
    // hop. The surface opens the support URL; the machine closes the whole
    // task-sheet stack back to the origin surface (same T-449 rule as every
    // other external redirect) with the task still open, and records the ask.
    case 'PROTECT_TASK_CONTACT_SUPPORT': {
      const fromSupportSheet = protect.location.name === 'taskResolution'
        && (protect.location.screen === 'resolving' || protect.location.screen === 'escalating');
      if (!fromSupportSheet) return rejected(snapshotValue, event.type);
      let previous = popProtectHistory(protect);
      if (previous) {
        while (previous.location.name === 'taskDetail' || previous.location.name === 'taskResolution') {
          const deeper = popProtectHistory(previous);
          if (!deeper) break;
          previous = deeper;
        }
      }
      const backdrop = previous ?? {
        ...protect,
        location: protectTodoLocation(runtime),
        history: [],
      };
      return withPrependedEffect(
        enterState({ commandCenter: 'ready' }, { ...snapshotValue.context, protect: backdrop }, true, [`event:${event.type}`]),
        analyticsTrackEffect('screen_action', { screen_name: 'protect', element: 'contact_apple_support' }),
      );
    }

    // archetype: redirectRecheck / inAppProcess — close the sheet, arm pending
    // task action, and either request an immediate authenticated app-state
    // refresh (in-app recheck with no OS destination) or wait for the next
    // APP_FOREGROUNDED after an external Settings/App Store hop. Neither path
    // resolves/removes the task locally.
    case 'PROTECT_TASK_RECHECK': {
      const canStartFromTaskDetail = protect.location.name === 'taskDetail';
      const canStartFromResolving = protect.location.name === 'taskResolution' && protect.location.screen === 'resolving';
      if (!canStartFromTaskDetail && !canStartFromResolving) {
        return rejected(snapshotValue, `${event.type}:invalidLocation`);
      }
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      const task = runtime.tasks.find((candidate) => candidate.id === taskId);
      if (!task?.taskType) return rejected(snapshotValue, `${event.type}:missingTaskType`);
      const lifecycle = beginProtectTaskAction({
        taskId,
        taskType: task.taskType,
        authoritativeStatus: task.status,
        requestedAt: new Date().toISOString(),
      });
      const backdrop = popProtectHistory(protect) ?? {
        ...protect,
        location: protectTodoLocation(runtime),
        history: [],
      };
      const nextContext = {
        ...snapshotValue.context,
        protect: {
          ...backdrop,
          runtime: {
            ...backdrop.runtime,
            selectedTaskId: taskId,
            pendingTaskAction: lifecycle.pending,
          },
        },
      };
      const refreshPolicy = resolveProtectTaskRecheckRefreshPolicy(task.taskType);
      if (refreshPolicy === 'immediate') {
        if (!snapshotValue.context.session?.sessionToken) {
          return rejected(snapshotValue, `${event.type}:missingSession`);
        }
        return enterState('authenticatedLoading', nextContext, true, [`event:${event.type}:immediateRefresh`]);
      }
      return enterState({ commandCenter: 'ready' }, nextContext, true, [`event:${event.type}:awaitingResume`]);
    }

    case 'PROTECT_TASK_DESTINATION_UNAVAILABLE':
      return enterState({ commandCenter: 'ready' }, {
        ...snapshotValue.context,
        appStateMessage: event.reason,
        protect: {
          ...protect,
          runtime: {
            ...runtime,
            toast: 'taskActionUnverified',
          },
        },
      }, true, [`event:${event.type}`]);

    /**
     * T-449 (owner decision, 2026-09-01): «I've already handled it» on the
     * still-seeing sheet used to REMOVE the task, and removed tasks never
     * reach the Resolved tab — which left the authored handled sheet (Figma
     * 2904:32254) unreachable in the product. It now resolves the task so the
     * user can check back on what she already reviewed.
     *
     * The proof stays honest: `manual_user_confirmed` records that the USER
     * said so and Onyx verified nothing. The device-local handled marker keeps
     * the Resolved sheet on the dismissal copy instead of the verified-clean
     * copy, which would overclaim.
     */
    case 'PROTECT_TASK_HANDLED': {
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      const effect = createProtectTaskResolutionEffect(snapshotValue.context, taskId, 'resolved', {
        type: 'manual_user_confirmed',
      });
      if (!effect) return rejected(snapshotValue, `${event.type}:missingBackendEffect`);
      // Remembered BEFORE the effect settles: the backend response rehydrates
      // through `protectStateFrom`, which reads this set to stamp `handled`.
      rememberProtectTaskHandledId(taskId);
      return result(snapshotValue, [effect], true, [`event:${event.type}`]);
    }

    case 'PROTECT_REMOVE_TASK': {
      const taskId = runtime.selectedTaskId;
      if (!taskId) return rejected(snapshotValue, `${event.type}:missingTask`);
      const effect = createProtectTaskResolutionEffect(snapshotValue.context, taskId, 'removed');
      if (!effect) return rejected(snapshotValue, `${event.type}:missingBackendEffect`);
      return result(snapshotValue, [effect], true, [`event:${event.type}`]);
    }

    case 'PROTECT_TOAST_ELAPSED':
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, advanceProtectToast(protect)), true, [`event:${event.type}`]);

    case 'PROTECT_OPEN_BROWSING':
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, {
        name: 'browsing',
        screen: protectBrowsingScreenFromRuntime(runtime),
      }), true, [`event:${event.type}`]);

    case 'PROTECT_TURN_OFF_BROWSING': {
      if (protect.location.name === 'browsing' && protect.location.screen === 'turnOff') {
        const nextRuntime = setProtectBrowsingProtection(runtime, 'off');
        // T-386 (owner 2026-08-20, frame 983): confirming turn-off lands on
        // the orange "Turn on browsing protection" screen — the owner
        // explicitly calls that landing correct. T-422's newer «повертати на
        // протект» reading is parked as an open owner question; flipping this
        // to protectMainLocation(nextRuntime) is a one-line change if she
        // decides the overview should own the landing.
        const context = withProtect(snapshotValue.context, {
          ...protect,
          runtime: nextRuntime,
          location: { name: 'browsing', screen: 'off' },
        });
        const preferenceEffect = createBrowserProtectionPreferenceEffect(context, false);
        return {
          snapshot: snapshot({ commandCenter: 'ready' }, context),
          effects: preferenceEffect ? [preferenceEffect] : [],
          accepted: true,
          trace: [`event:${event.type}:disabled`],
        };
      }
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'browsing', screen: 'turnOff' }), true, [`event:${event.type}:confirm`]);
    }

    case 'PROTECT_OPEN_BROWSING_SETUP':
      // The cached app-state projection may contain the effective conjunction
      // (system AND app), so it cannot decide whether Safari is already active.
      // Ask the native bridge at the turn-on boundary; the proof result either
      // enables the app preference immediately or opens the Settings sheet.
      return {
        snapshot: snapshotValue,
        effects: [{
          type: 'checkExtensionActivation',
          input: {
            trigger: 'protect',
            intent: 'turnOn',
            sessionToken: snapshotValue.context.session?.sessionToken,
          },
        }],
        accepted: true,
        trace: [`event:${event.type}:freshSystemProof`],
      };

    case 'PROTECT_SAVE_BROWSING_FOR_LATER':
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, protectMainLocation(runtime)), true, [`event:${event.type}`]);

    case 'PROTECT_OPEN_BROWSER_SHEET':
      // Module/task entry points share the same fresh proof decision as the
      // Browsing screen CTA. An active system extension bypasses the sheet;
      // inactive proof opens it after the bridge result settles.
      return {
        snapshot: snapshotValue,
        effects: [{
          type: 'checkExtensionActivation',
          input: {
            trigger: 'protect',
            intent: 'turnOn',
            sessionToken: snapshotValue.context.session?.sessionToken,
          },
        }],
        accepted: true,
        trace: [`event:${event.type}:freshSystemProof`],
      };

    case 'PROTECT_ENABLE_BROWSER_PROTECTION': {
      // Do not resolve Browser Protection from the CTA alone. The effect must
      // return an active App Group/native proof before risk drops.
      return {
        snapshot: snapshotValue,
        effects: [{
          type: 'checkExtensionActivation',
          input: {
            trigger: 'protect',
            intent: 'user',
            sessionToken: snapshotValue.context.session?.sessionToken,
          },
        }],
        accepted: true,
        trace: [`event:${event.type}:check`],
      };
    }

    case 'PROTECT_DISMISS_SHEET':
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
        ...protect,
        runtime: dismissProtectOverlay(runtime),
      }), true, [`event:${event.type}`]);

    case 'PROTECT_OPEN_PHONE_SECURITY': {
      // Replaying the same signal on the already-visible scene stays an
      // idempotent no-op. Leaving and deliberately reopening Device re-arms the
      // proof so a transient backend/provider failure can actually be retried.
      if (protect.location.name === 'phoneSecurity') {
        return enterState({ commandCenter: 'ready' }, snapshotValue.context, true, [`event:${event.type}`]);
      }
      const nextRuntime = preparePhoneSecurityProofsForRefresh(runtime);
      return enterState({ commandCenter: 'ready' }, withProtectLocation(withProtectRuntime(snapshotValue.context, nextRuntime), {
        name: 'phoneSecurity',
        issues: nextRuntime.phoneChecks.some((check) => check.status === 'issue'),
      }), true, [`event:${event.type}`]);
    }

    case 'PROTECT_OPEN_LEAK_DETECTION':
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, protectLeakDetectionLocation(runtime)), true, [`event:${event.type}`]);

    // Feedback ("no way to trigger leak detection from the tab"): run the same
    // backend startScanForUser path the free tab uses (`runFreeScan` submits
    // POST /v1/protect/free-scan and polls to a refreshed app-state; the server
    // enforces cooldown/reuse). A toast confirms the request immediately.
    case 'PROTECT_RUN_LEAK_SCAN': {
      const session = snapshotValue.context.session;
      if (!session) return rejected(snapshotValue, `${event.type}:missingSession`);
      const context = withProtect({
        ...snapshotValue.context,
        appStateMessage: 'Checking for new leaks…',
      }, {
        ...protect,
        runtime: { ...runtime, toast: 'leakScanStarted', toastTimerArmed: true },
      });
      return {
        snapshot: snapshot({ commandCenter: 'ready' }, context),
        effects: [
          { type: 'runFreeScan', input: { sessionToken: session.sessionToken } },
          { type: 'protectToastTimer', input: {} },
        ],
        accepted: true,
        trace: [`event:${event.type}`],
      };
    }

    case 'PROTECT_SWITCH_LEAK_TAB': {
      const nextRuntime = { ...runtime, leakTab: event.tab };
      return enterState({ commandCenter: 'ready' }, withProtect(snapshotValue.context, {
        ...protect,
        runtime: nextRuntime,
        location: protectLeakDetectionLocation(nextRuntime),
      }), true, [`event:${event.type}:${event.tab}`]);
    }

    case 'PROTECT_OPEN_MONITORING_LIST':
      if (!runtime.monitoringCapabilities.monitoringListEnabled) {
        return rejected(snapshotValue, `${event.type}:featureDisabled`);
      }
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'monitoring', screen: 'list' }), true, [`event:${event.type}`]);

    case 'PROTECT_OPEN_MONITORING_TASK_DETAIL':
      if (!runtime.monitoringCapabilities.monitoringListEnabled) {
        return rejected(snapshotValue, `${event.type}:featureDisabled`);
      }
      return enterState({ commandCenter: 'ready' }, withProtectLocation(snapshotValue.context, { name: 'monitoring', screen: 'taskDetail' }), true, [`event:${event.type}`]);

    case 'PROTECT_ADD_MONITORING_EMAIL':
      // Email identifiers are already monitored by the account; the former
      // "Add another email" affordance is intentionally no longer exposed.
      return rejected(snapshotValue, `${event.type}:unsupported`);

    case 'PROTECT_MONITORING_EMAIL_CHANGED':
      return rejected(snapshotValue, `${event.type}:unsupported`);

    case 'PROTECT_SUBMIT_MONITORING_EMAIL': {
      return rejected(snapshotValue, `${event.type}:unsupported`);
    }

    case 'PROTECT_OPEN_TRAIN': {
      const normalized = event.location
        ? normalizeProtectedRoute(snapshotValue.context, event.location, 'user')
        : rootLocations.train;
      const location = normalized.area === 'train' ? normalized : rootLocations.train;
      return stay(snapshotValue, {
        ...snapshotValue.context,
        selectedTab: 'train',
        trainLocation: location,
      }, `event:${event.type}:${location.screen}`);
    }
  }
}

function transitionCommandCenter(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  if (event.type === 'REFRESH_APP_STATE') {
    return enterState('authenticatedLoading', snapshotValue.context, true, [`event:${event.type}`]);
  }

  // Returning from any authenticated background gap re-syncs app-state and
  // native device proof, regardless of the selected tab. Drill-in location is
  // preserved by assignLoadedAppState + preserveProtectDrillInAcrossAppStateRefresh.
  if (
    event.type === 'APP_FOREGROUNDED'
    && snapshotValue.matches({ commandCenter: 'ready' })
    && snapshotValue.context.session?.sessionToken
  ) {
    // T-449 (review, TestFlight 176): with the Settings instruction sheet open
    // the return itself must lead to a fresh activation proof. The sheet's own
    // listener may fire after this refresh has left `ready` (its enable event
    // is then rejected), so the machine remembers the return and re-asks on the
    // next `ready` entry — one proof, whichever listener wins.
    const sheetOpen = snapshotValue.context.protect.runtime.overlay === 'browserSheet';
    const context = sheetOpen
      ? withProtectRuntime(snapshotValue.context, { ...snapshotValue.context.protect.runtime, pendingForegroundRecheck: true })
      : snapshotValue.context;
    return {
      snapshot: snapshot('authenticatedLoading', context),
      effects: [{
        type: 'loadAppState',
        input: {
          sessionToken: snapshotValue.context.session.sessionToken,
          refreshExtension: true,
        },
      }],
      accepted: true,
      trace: [`event:${event.type}:authenticatedRefresh`],
    };
  }

  if (isProtectEvent(event)) {
    return transitionProtect(snapshotValue, event);
  }

  if (snapshotValue.matches({ commandCenter: 'ready' }) && event.type === 'SUBMIT_SCAN') {
    if (snapshotValue.context.scanSubject.trim().length > 0) {
      const context = applyUpdate(snapshotValue.context, update.assignScanSubmitting, event);
      return enterState({ commandCenter: 'scanning' }, context, true, [`event:${event.type}`]);
    }
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignScanError, { error: new Error('Enter an email or password to scan.') }), `event:${event.type}:missingSubject`);
  }

  return rejected(snapshotValue, event.type);
}

function transitionVisualReady(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  if (event.type === 'VISUAL_ROUTE_CHANGED') {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.assignVisualRoute, event), `event:${event.type}`);
  }

  if (
    event.type === 'REFRESH_APP_STATE' ||
    event.type === 'SUBMIT_SCAN' ||
    isProtectEvent(event) ||
    isForgotPasswordEvent(event) ||
    event.type === 'ONBOARDING_CONTINUE' ||
    event.type === 'ONBOARDING_RUN_DEEP_SCAN' ||
    event.type === 'ONBOARDING_REVIEW_FINDINGS' ||
    event.type === 'ONBOARDING_ADVANCE_SCREEN' ||
    event.type === 'ONBOARDING_BACK' ||
    event.type === 'ONBOARDING_CREATE_TASK' ||
    event.type === 'ONBOARDING_ANSWER_QUIZ' ||
    event.type === 'ONBOARDING_SET_UP_EXTENSION' ||
    event.type === 'ONBOARDING_SKIP_EXTENSION' ||
    event.type === 'ONBOARDING_SAVE_EXTENSION_FOR_LATER' ||
    event.type === 'ONBOARDING_CHECK_EXTENSION_ACTIVATION' ||
    event.type === 'ONBOARDING_EXTENSION_ACTIVATED' ||
    event.type === 'ONBOARDING_GO_TO_PROTECT'
  ) {
    return stay(snapshotValue, applyUpdate(snapshotValue.context, update.applyVisualScenario, event), `event:${event.type}`);
  }

  return rejected(snapshotValue, event.type);
}

function enterState(
  value: MobileAppStateValue,
  context: MobileAppContext,
  accepted: boolean,
  trace: string[],
  options: { suppressOnboardingTimer?: boolean; effects?: MobileAppEffectRequest[] } = {},
): MobileAppTransitionResult {
  const entryResult = (
    snapshotValue: MobileAppSnapshot,
    effects: MobileAppEffectRequest[],
    nextTrace: string[] = trace,
  ): MobileAppTransitionResult => withEntryAnalytics(result(snapshotValue, effects, accepted, nextTrace));

  if (value === 'booting') {
    if (context.visualMode) {
      const nextContext = applyUpdate(context, update.applyVisualScenario, { output: undefined });
      return entryResult(snapshot('visualReady', nextContext), [], [...trace, 'always:visualReady']);
    }
    return entryResult(snapshot('booting', context), [{ type: 'loadStoredSession', input: {} }]);
  }

  if (value === 'checkingHealth') {
    return entryResult(snapshot('checkingHealth', context), [{ type: 'checkProductHealth', input: {} }]);
  }

  if (value === 'authenticatedLoading') {
    return entryResult(snapshot('authenticatedLoading', context), [{ type: 'loadAppState', input: { sessionToken: update.requireStoredSession(context.session).sessionToken } }]);
  }

  if (value === 'clearingExpiredSession') {
    return entryResult(snapshot('clearingExpiredSession', context), [{ type: 'clearSession', input: {} }]);
  }

  if (value === 'clearingSession') {
    return entryResult(snapshot('clearingSession', context), [{ type: 'clearSession', input: { sessionToken: context.session?.sessionToken } }]);
  }

  if (isAuthenticatingState(value)) {
    if (value.authenticating === 'login') {
      return entryResult(snapshot(value, context), options.effects ?? [{ type: 'loginAccount', input: authCredentialsInput(context) }]);
    }
    if (value.authenticating === 'register') {
      return entryResult(snapshot(value, context), [{ type: 'registerAccount', input: authCredentialsInput(context) }]);
    }
    return entryResult(snapshot(value, context), [{ type: 'persistSession', input: { session: update.requirePendingSession(context.pendingSession) } }]);
  }

  if (isForgotPasswordState(value)) {
    if (value.forgotPassword === 'requestingReset') {
      return entryResult(snapshot(value, context), [{ type: 'requestPasswordReset', input: { email: context.forgotPassword.email.trim() } }]);
    }
    if (value.forgotPassword === 'resettingPassword') {
      return entryResult(snapshot(value, context), [{ type: 'resetPassword', input: { token: context.forgotPassword.resetToken ?? '', password: context.forgotPassword.newPassword } }]);
    }
    if (value.forgotPassword === 'checkInbox' && context.forgotPassword.resendCooldownSeconds > 0) {
      // The armed tick carries the live remaining seconds (FreeScanTimerInput
      // pattern) so the resend label can count down second by second (G6/T6).
      return entryResult(snapshot(value, context), [{
        type: 'forgotPasswordTimer',
        input: { phase: 'resendCooldown', secondsRemaining: context.forgotPassword.resendCooldownSeconds },
      }]);
    }
    if (value.forgotPassword === 'passwordChanged') {
      return entryResult(snapshot(value, context), [{ type: 'forgotPasswordTimer', input: { phase: 'successRedirect' } }]);
    }
  }

  if (isGetStartedState(value)) {
    if (value.getStarted === 'creatingAccount') {
      const exposure = resolveExposureLevel(context.getStarted.answers);
      return entryResult(
        snapshot(value, context),
        [
          {
            type: 'registerAccount',
            input: {
              email: context.getStarted.email.trim(),
              password: context.getStarted.password,
              ...(context.fSessionId ? { fSessionId: context.fSessionId } : {}),
              onboarding: {
                variant: 'get_started_free',
                riskStatus: exposure.level,
              },
            },
          },
        ],
      );
    }
    if (value.getStarted === 'analyzing') {
      return entryResult(snapshot(value, context), [{ type: 'getStartedTimer', input: { phase: 'analyzing' } }]);
    }
    if (value.getStarted === 'scan') {
      // The 2s scan phase is the theatrical floor governing this auto-advance
      // beat (G13); the network settle alone could resolve in 0ms for reused
      // scans and hard-jump to the result.
      return entryResult(snapshot(value, context), [
        { type: 'runFreeScan', input: { sessionToken: update.requireStoredSession(context.session).sessionToken } },
        { type: 'getStartedTimer', input: { phase: 'scan' } },
      ]);
    }
  }

  if (isFirstSessionState(value)) {
    if (value.firstSession === 'route') {
      // Resume at the start of the furthest backend chapter (§ resumeModel).
      const chapter = getOnboardingChapter(context.appState);
      // ReturningAccountBridge removed (owner 2026-07-14): the "Let's finish
      // setting up" acknowledgment duplicated RiskIntro. Standard returning
      // accounts with no first-session work resume straight at their funnel risk
      // level — chapter `risk_intro` → RiskIntro (see below) → Run deep scan.
      // Family-plan buyers (web funnel) get an InviteFamily step inserted before
      // the regular chapters (§ family-owner-first-session: Welcome → InviteFamily
      // → RiskIntro). It runs once. The runtime `familyInvitesHandled` flag covers
      // the in-session send/defer, but it re-initialises false on every app-state
      // reload — so we ALSO gate on the persistent backend chapter. InviteFamily
      // only belongs in the pre-deep-scan window (welcome / risk_intro); once the
      // owner has run the deep scan (deep_scan or later) the backend chapter
      // proves invites were already handled, so a reload after Scan/ScanResult
      // must never re-insert InviteFamily (Issue P).
      const membershipSurface = firstSessionSurfaceForMembership({
        onboardingVariant: context.appState?.onboardingVariant ?? 'standard',
        firstSessionStep: chapter,
        familyInvitesHandled: context.onboarding.familyInvitesHandled,
      });
      if (membershipSurface === 'inviteFamily') {
        return enterState({ firstSession: 'inviteFamily' }, context, accepted, [...trace, 'always:firstSession.inviteFamily']);
      }
      // Persist welcome → risk_intro before exposing RiskUnknownNewMember (or
      // RiskIntro). Invited members used to enter the scan from persisted
      // welcome, so deep_scan_completed CAS-missed and a 200 reroute looped.
      if (chapter === 'welcome') return enterState({ firstSession: 'advancing' }, withChapterEvent(context, 'risk_intro_seen'), accepted, [...trace, 'always:firstSession.welcomeAutoAdvance']);
      if (membershipSurface === 'riskUnknownNewMember') {
        return enterState({ firstSession: 'riskUnknownNewMember' }, context, accepted, [...trace, 'always:firstSession.riskUnknownNewMember']);
      }
      // Resuming mid-flow seeds a single back destination (the preceding
      // chapter's entry screen) so the first resumed screen keeps the Figma
      // chevron instead of hiding a dead control (owner 2026-08-05).
      const resumed = withResumeHistory(context, resumeBackHistory(chapter, context));
      if (chapter === 'risk_intro') return enterState({ firstSession: 'riskIntro' }, resumed, accepted, [...trace, 'always:firstSession.riskIntro']);
      if (chapter === 'deep_scan') return enterState({ firstSession: 'scanResult' }, resumed, accepted, [...trace, 'always:firstSession.scanResult']);
      if (chapter === 'leaks') {
        const leakScreen = initialLeakScreen(context);
        // Unproven scan: skip the whole leak chapter rather than assert
        // anything about the user's leaks (T-209 / pass-5 CRITICAL 2). Same
        // shape as the §9 device skip and the same checkpoint the chapter
        // would have posted from its last screen, so the backend step sequence
        // is unchanged and liveness is carried by the advance effect.
        if (leakScreen === 'skip') {
          return withPrependedEffect(
            enterState({ firstSession: 'advancing' }, withChapterEvent(context, 'spot_fake_completed'), accepted, [...trace, 'always:firstSession.leakSkippedUnproven']),
            analyticsTrackEffect('onboarding_step_completed', { step_name: 'leaks' }),
          );
        }
        return enterState({ firstSession: 'leak' }, withChapterScreen(resumed, leakScreen), accepted, [...trace, 'always:firstSession.leak']);
      }
      if (chapter === 'spot_fake') {
        // Resume mid-chapter screens after a checkpoint (intro→quiz parks
        // `chapterScreen: 'quiz'` while `advancing` keeps Intro painted busy).
        const resumeScreen = context.onboarding.chapterScreen;
        const screen = resumeScreen === 'quiz' || resumeScreen === 'reveal' || resumeScreen === 'result'
          ? resumeScreen
          : 'intro';
        return enterState({ firstSession: 'spotFake' }, withChapterScreen(resumed, screen), accepted, [...trace, 'always:firstSession.spotFake']);
      }
      if (chapter === 'browser') return enterState({ firstSession: 'browser' }, withChapterScreen(resumed, 'extensionIntro'), accepted, [...trace, 'always:firstSession.browser']);
      if (chapter === 'device') return enterState({ firstSession: 'device' }, withChapterScreen(resumed, 'intro'), accepted, [...trace, 'always:firstSession.device']);
      // T-567: the «Risk lowered» recap ALWAYS opens the postflow (owner
      // 2026-09-13: «должен быть последний экран, где мы саммари даем»). Its
      // claim is the whole demonstrative run — ghost at the start, handle where
      // the sections left it. No server pair is consulted here any more.
      return enterState(
        { firstSession: 'postFlow' },
        withChapterScreen(takePostflowRiskLoweredClaim(context), 'riskLowered'),
        accepted,
        [...trace, 'always:firstSession.postFlow.riskLowered'],
      );
    }

    if (value.firstSession === 'deepScan') {
      // Theatrical DeepScan (§1.4) is the separately user-launched effect
      // timer. A history restore deliberately does not arm a fresh timer;
      // the stale effect from the abandoned run is rejected by its state guard.
      return entryResult(
        snapshot(value, context),
        options.suppressOnboardingTimer ? [] : [{ type: 'onboardingTimer', input: { phase: 'deepScan' } }],
      );
    }

    // Former success/risk-drop/setup beats remain machine-owned timers. The
    // generation is part of the service input and changes on every entry,
    // including Back/history restoration, so only the current full dwell can
    // advance this state.
    const timedPhase = firstSessionTimerPhase(value, context.onboarding.chapterScreen, context);
    if (timedPhase) {
      let nextContext = withNextOnboardingTimerGeneration(context);
      // T-566: a beat reached again through Back (its history entry was
      // pushed after the claim was cleared) replays its section's stored
      // claim — the same step, never a new one, never an endpoint-less gauge.
      const beatSection = demonstrativeSectionForBeat(value, context.onboarding.chapterScreen);
      if (beatSection) nextContext = withSectionClaim(nextContext, beatSection);
      const effects: MobileAppEffectRequest[] = [{
        type: 'onboardingTimer',
        input: { phase: timedPhase, generation: nextContext.onboarding.onboardingTimerGeneration },
      }];
      // Leak saved is the chapter-owned persist beat. Re-arm on Back as well so
      // a later consecutive server pair can mint a fresh one-shot claim.
      const sessionToken = context.session?.sessionToken;
      if (value.firstSession === 'leak' && context.onboarding.chapterScreen === 'saved' && sessionToken) {
        nextContext = {
          ...nextContext,
          onboarding: { ...nextContext.onboarding, pendingChapterRiskPersist: true },
        };
        effects.unshift({ type: 'loadAppState', input: { sessionToken } });
      }
      return entryResult(snapshot(value, nextContext), effects);
    }

  if (value.firstSession === 'browser' && context.onboarding.chapterScreen === 'extensionSetup') {
      // While the user is setting up the extension we poll the activation port
      // (§8.2). The interpreter re-enters this screen until activation flips.
      return entryResult(snapshot(value, context), [{ type: 'checkExtensionActivation', input: { trigger: 'onboarding', sessionToken: context.session?.sessionToken } }]);
    }

    if (value.firstSession === 'advancing') {
      const event = requireChapterEvent(context);
      return entryResult(snapshot(value, context), [{ type: 'advanceFirstSession', input: { sessionToken: update.requireStoredSession(context.session).sessionToken, event } }]);
    }
  }

  if (isCommandCenterState(value) && value.commandCenter === 'ready') {
    let commandCenterContext = context;
    const effects: MobileAppEffectRequest[] = [];
    // T-449: single-flight — a `ready` re-entry while a timer is pending (sheet
    // dismiss, refresh settle) must not arm a second one that would end the
    // beat/toast early.
    if (shouldRunProtectToastTimer(commandCenterContext.protect) && !commandCenterContext.protect.runtime.toastTimerArmed) {
      effects.push({ type: 'protectToastTimer', input: {} });
      commandCenterContext = withProtectRuntime(commandCenterContext, { ...commandCenterContext.protect.runtime, toastTimerArmed: true });
    }
    if (
      commandCenterContext.protect.location.name === 'phoneSecurity' &&
      commandCenterContext.protect.runtime.phoneSecurityProofs.status === 'pending'
    ) {
      commandCenterContext = withProtect(commandCenterContext, {
        ...commandCenterContext.protect,
        runtime: markPhoneSecurityProofsChecking(commandCenterContext.protect.runtime),
      });
      effects.push({
        type: 'checkPhoneSecurityProofs',
        input: { sessionToken: commandCenterContext.session?.sessionToken },
      });
    }
    // T-449 (review, TestFlight 176): consume the foreground-return marker.
    // If the sheet is still open, ask for one fresh proof with the turn-on
    // intent (`user`); if the sheet's own listener already closed it, the
    // marker is dropped without a second probe.
    if (commandCenterContext.protect.runtime.pendingForegroundRecheck) {
      const stillOpen = commandCenterContext.protect.runtime.overlay === 'browserSheet';
      commandCenterContext = withProtectRuntime(commandCenterContext, { ...commandCenterContext.protect.runtime, pendingForegroundRecheck: false });
      if (stillOpen) {
        effects.push({
          type: 'checkExtensionActivation',
          input: { trigger: 'protect', intent: 'user', sessionToken: commandCenterContext.session?.sessionToken },
        });
        return entryResult(snapshot(value, commandCenterContext), effects);
      }
    }
    if (
      commandCenterContext.protect.runtime.browsing.protection === 'off' &&
      commandCenterContext.protect.runtime.overlay === 'none' &&
      commandCenterContext.extensionActivationProof === null
    ) {
      effects.push({
        type: 'checkExtensionActivation',
        input: {
          trigger: 'protect',
          intent: 'background',
          sessionToken: commandCenterContext.session?.sessionToken,
        },
      });
    }
    if (effects.length > 0) return entryResult(snapshot(value, commandCenterContext), effects);
  }

  if (isCommandCenterState(value) && value.commandCenter === 'scanning') {
    return entryResult(snapshot(value, context), [{ type: 'runScan', input: { subjectType: context.scanSubjectType, subject: context.scanSubject } }]);
  }

  return entryResult(snapshot(value, context), []);
}

function applyUpdate(
  context: MobileAppContext,
  updater: (args: { context: MobileAppContext; event: MobileAppEvent }) => ContextUpdate,
  event: UpdateEvent,
): MobileAppContext {
  return {
    ...context,
    ...updater({ context, event: event as MobileAppEvent }),
  };
}

function snapshot(value: MobileAppStateValue, context: MobileAppContext): MobileAppSnapshot {
  return {
    value,
    context,
    matches: (matcher) => stateMatches(value, matcher),
  };
}

function result(
  snapshotValue: MobileAppSnapshot,
  effects: MobileAppEffectRequest[],
  accepted: boolean,
  trace: string[],
): MobileAppTransitionResult {
  return { snapshot: snapshotValue, effects, accepted, trace };
}

/**
 * Post-accept screen-view seam, applied once per public transition (send /
 * settle / start): emits screen_viewed whenever the resolved screen name of
 * the final snapshot differs from the last non-null emitted name, regardless
 * of whether the branch used enterState, stay() or a raw result().
 * Null names do not emit and do not clear lastScreenViewedName: A→null→A is
 * not a new view of A (comparison is against the last non-null emitted name).
 */
function finalizeScreenView(transition: MobileAppTransitionResult): MobileAppTransitionResult {
  if (!transition.accepted) return transition;
  const screenName = screenNameForState(transition.snapshot.value, transition.snapshot.context);
  if (!screenName) return transition;
  if (screenName === transition.snapshot.context.lastScreenViewedName) return transition;
  return {
    ...transition,
    snapshot: snapshot(transition.snapshot.value, {
      ...transition.snapshot.context,
      lastScreenViewedName: screenName,
    }),
    effects: [
      analyticsTrackEffect('screen_viewed', { screen_name: screenName }),
      ...transition.effects,
    ],
  };
}

function withEntryAnalytics(transition: MobileAppTransitionResult): MobileAppTransitionResult {
  if (!transition.accepted) return transition;
  return withPrependedEffects(transition, entryAnalyticsEffects(transition.snapshot));
}

function entryAnalyticsEffects(snapshotValue: MobileAppSnapshot): MobileAppEffectRequest[] {
  const effects: MobileAppEffectRequest[] = [];
  if (isFirstSessionState(snapshotValue.value)) {
    if (snapshotValue.value.firstSession === 'riskIntro' || snapshotValue.value.firstSession === 'riskUnknownNewMember') {
      effects.push(analyticsTrackEffect('onboarding_started', {}));
    }
    if (snapshotValue.value.firstSession === 'scanResult') {
      effects.push(scanViewedAnalyticsEffect(snapshotValue.context.onboarding.deepScanRun.scanRunId));
    }
  }
  return effects;
}


function webToAppAutologinProgress(
  context: MobileAppContext,
  properties: Record<string, unknown> & { stage: string },
) {
  const openId = (typeof properties.open_id === 'string' && properties.open_id.trim())
    ? properties.open_id.trim()
    : (context.pendingAppsFlyerOpenId ?? undefined);
  return analyticsTrackEffect(
    'web_to_app_autologin_progress',
    {
      ...properties,
      ...(openId ? { open_id: openId } : {}),
    } as Parameters<typeof analyticsTrackEffect>[1],
  );
}

function analyticsTrackEffect<TEventName extends MobileAnalyticsEventName>(
  eventName: TEventName,
  properties: MobileAnalyticsProperties<TEventName>,
): MobileAnalyticsTrackInput<TEventName> & { type: 'analytics.track' } {
  return { type: 'analytics.track', eventName, properties };
}

function scanViewedAnalyticsEffect(scanRunId: string | null | undefined): MobileAppEffectRequest {
  return analyticsTrackEffect('scan_viewed', scanRunId ? { scan_run_id: scanRunId } : {});
}

function stay(snapshotValue: MobileAppSnapshot, context: MobileAppContext, trace: string): MobileAppTransitionResult {
  return result(snapshot(snapshotValue.value, context), [], true, [trace]);
}

/**
 * Complete a single-select answer dwell: only advance when the user is still on
 * the armed quiz location with a matching generation (stale re-select/Back safe).
 */
function settleGetStartedSelectionDwell(
  snapshotValue: MobileAppSnapshot,
  output: GetStartedTimerInput,
  tracePrefix: string,
): MobileAppTransitionResult {
  const getStarted = snapshotValue.context.getStarted;
  const fromLocation = output.fromLocation;
  if (!fromLocation || !isSingleSelectLocation(fromLocation)) {
    return stay(snapshotValue, snapshotValue.context, `${tracePrefix}:invalid`);
  }
  if (getStarted.location !== fromLocation) {
    return stay(snapshotValue, snapshotValue.context, `${tracePrefix}:staleLocation`);
  }
  if ((getStarted.selectionDwellGeneration ?? 0) !== (output.generation ?? 0)) {
    return stay(snapshotValue, snapshotValue.context, `${tracePrefix}:staleGeneration`);
  }
  const next = nextQuizLocation(fromLocation);
  const stepName = getStartedQuizStepName(fromLocation);
  const nextState = {
    ...getStarted,
    location: next,
    selectionDwellGeneration: undefined,
  };
  return withPrependedEffect(
    enterState(
      { getStarted: next },
      withGetStarted(pushGetStartedHistory(snapshotValue.context, fromLocation), nextState),
      true,
      [`${tracePrefix}:advance`],
    ),
    stepName
      ? analyticsTrackEffect('onboarding_step_completed', { step_name: stepName })
      : null,
  );
}

function withPrependedEffects(
  transition: MobileAppTransitionResult,
  effects: MobileAppEffectRequest[],
): MobileAppTransitionResult {
  if (effects.length === 0) return transition;
  return { ...transition, effects: [...effects, ...transition.effects] };
}

function withPrependedEffect(
  transition: MobileAppTransitionResult,
  effect: MobileAppEffectRequest | null,
): MobileAppTransitionResult {
  if (!effect) return transition;
  return { ...transition, effects: [effect, ...transition.effects] };
}

function rejected(snapshotValue: MobileAppSnapshot, eventType: string): MobileAppTransitionResult {
  return result(snapshotValue, [], true, [`rejected:${eventType}`]);
}

function hasCredentials(context: MobileAppContext) {
  return context.authEmail.trim().length > 0 && context.authPassword.length > 0;
}

function appsFlyerAutologinFailureKind(error: unknown): 'rejected' | 'unexpected' {
  if (error && typeof error === 'object' && 'status' in error && typeof error.status === 'number') {
    return 'rejected';
  }
  return 'unexpected';
}

function authCredentialsInput(context: MobileAppContext): LoginAccountInput {
  return {
    email: context.authEmail.trim(),
    password: context.authPassword,
    ...(context.fSessionId ? { fSessionId: context.fSessionId } : {}),
  };
}

const GET_STARTED_QUESTION_KEY_BY_LOCATION: Partial<Record<GetStartedRuntimeLocation, PersistGetStartedQuizAnswerInput['questionKey']>> = {
  q1: 'q1Motivations',
  q2: 'q2PhoneUse',
  q3: 'q3PayOnline',
  q4: 'q4Exposed',
  q5: 'q5TapLinks',
  q6: 'q6MoneyApps',
};

function createPersistGetStartedQuizAnswerEffect(
  context: MobileAppContext,
  location: GetStartedRuntimeLocation,
  state: MobileAppContext['getStarted'],
): MobileAppEffectRequest | null {
  if (!context.fSessionId) return null;
  const questionKey = GET_STARTED_QUESTION_KEY_BY_LOCATION[location];
  if (!questionKey) return null;
  const answer = state.answers[questionKey];
  if (answer == null) return null;
  return {
    type: 'persistGetStartedQuizAnswer',
    input: {
      fSessionId: context.fSessionId,
      questionKey,
      answer,
      answeredAt: new Date().toISOString(),
    },
  };
}

function createBrowserProtectionPreferenceEffect(
  context: MobileAppContext,
  enabled: boolean,
): Extract<MobileAppEffectRequest, { type: 'setBrowserProtectionAppEnabled' }> | null {
  if (!context.session) return null;
  return {
    type: 'setBrowserProtectionAppEnabled',
    input: {
      enabled,
      sessionToken: update.requireStoredSession(context.session).sessionToken,
    },
  };
}

function withForgotPassword(
  context: MobileAppContext,
  forgotPassword: MobileAppContext['forgotPassword'],
): MobileAppContext {
  return { ...context, forgotPassword };
}

function withFamilyInvite(
  context: MobileAppContext,
  familyInvite: MobileAppContext['familyInvite'],
): MobileAppContext {
  return { ...context, familyInvite };
}

function markFamilyInvitesHandled(context: MobileAppContext): MobileAppContext {
  return { ...context, onboarding: { ...context.onboarding, familyInvitesHandled: true } };
}

function markReturningAccountBridgeAcknowledged(context: MobileAppContext): MobileAppContext {
  return {
    ...context,
    onboarding: { ...context.onboarding, returningAccountBridgeAcknowledged: true },
  };
}

function withProtect(context: MobileAppContext, protect: ProtectRuntimeMachineState): MobileAppContext {
  return { ...context, protect };
}

function withExtensionActivationProof(
  context: MobileAppContext,
  extensionActivationProof: CheckExtensionActivationOutput['proof'],
): MobileAppContext {
  return { ...context, extensionActivationProof };
}

function extensionProofMessage(proof: CheckExtensionActivationOutput['proof']): string {
  if (proof.state === 'inactive') return 'Safari extension activation is not detected yet.';
  if (proof.state === 'unsupported') return 'Safari extension proof is unavailable in this build.';
  if (proof.state === 'error') return 'Safari extension proof could not be checked.';
  return 'Safari extension activation is not confirmed yet.';
}

function createProtectTaskResolutionEffect(
  context: MobileAppContext,
  taskId: string,
  outcome: ResolveProtectTaskInput['outcome'],
  proof: ResolveProtectTaskInput['proof'] = undefined,
): Extract<MobileAppEffectRequest, { type: 'resolveProtectTask' }> | null {
  if (!context.session) return null;
  const task = context.protect.runtime.tasks.find((candidate) => candidate.id === taskId);
  if (!task) return null;
  const resolvedProof = proof ?? protectResolutionProofForTask(context, task, outcome);
  return {
    type: 'resolveProtectTask',
    input: {
      sessionToken: update.requireStoredSession(context.session).sessionToken,
      taskId,
      outcome,
      actionModel: task.actionModel ?? defaultProtectTaskActionModel(task),
      proof: resolvedProof,
      idempotencyKey: protectResolutionIdempotencyKey(taskId, outcome, resolvedProof),
    },
  };
}

function reconcileProtectTaskResolution(
  context: MobileAppContext,
  output: ResolveProtectTaskOutput,
): MobileAppContext {
  const protect = update.withProtectRiskTrend(context.protect, update.protectStateFrom(output.appState));
  // T-299: keep opened local even after resolution rehydrate from backend.
  rememberProtectTaskOpenedId(output.input.taskId);
  const openedRuntime = markProtectTaskOpened(protect.runtime, output.input.taskId);
  const browserActivation = output.input.proof?.type === 'extension_activation';
  const toast: ProtectRuntimeMachineState['runtime']['toast'] = browserActivation
    ? 'browserProtectionEnabled'
    : output.input.outcome === 'removed'
    ? 'taskRemoved'
    : output.input.outcome === 'monitoring'
      ? 'monitoringAdded'
      : 'taskResolved';
  const runtime = { ...openedRuntime, toast };
  // T-275: risk-dropped scene only when two ordered server projections prove a drop.
  // Singular accessor only (canonicalRiskStatusFromAppState).
  const fromStatus = canonicalRiskStatusFromAppState(context.appState);
  const toStatus = canonicalRiskStatusFromAppState(output.appState);
  const fromEndpoint = fromStatus
    ? {
      status: fromStatus,
      handlePosition: context.protect.runtime.canonicalHandlePosition,
      score: context.protect.runtime.canonicalRiskScore ?? context.appState?.protect.risk.score,
    }
    : null;
  const toEndpoint = toStatus
    ? {
      status: toStatus,
      handlePosition: protect.runtime.canonicalHandlePosition,
      score: protect.runtime.canonicalRiskScore ?? output.appState.protect.risk.score,
    }
    : null;
  const provenDrop = fromEndpoint != null && toEndpoint != null && shouldShowRiskDropClaim(fromEndpoint, toEndpoint);
  const postDropScreen = output.input.outcome === 'removed' ? 'removed' : 'resolved';
  const resolutionScreen = provenDrop ? 'riskDropped' : postDropScreen;
  const provenRiskDrop = resolutionScreen === 'riskDropped' && fromEndpoint && toEndpoint
    ? {
      from: fromEndpoint.status,
      to: toEndpoint.status,
      fromHandle: fromEndpoint.handlePosition,
      toHandle: toEndpoint.handlePosition,
    }
    : null;
  return {
    ...context,
    appState: output.appState,
    appStateMessage: 'Protect task change saved.',
    protect: {
      ...protect,
      runtime: {
        ...runtime,
        // Exact product pair for ProtectRiskDroppedScene (all descending bands).
        provenRiskDrop,
        riskDropReturnScreen: provenDrop ? postDropScreen : null,
        // T-449 (owner): with a proven drop the outcome toast waits until the
        // Risk-dropped beat hands off — never concurrent with the scene.
        toast: provenDrop ? null : runtime.toast,
        riskDropReturnToast: provenDrop ? toast : null,
        // T-449 (owner): the settle returns to the surface the task was opened
        // FROM — the most recent non-sheet drill-in entry (overview or To-Do).
        // Extension activation lands on the overview; when it also proved a
        // level drop, the beat hands the user back there after the toast.
        resolutionReturnLocation: browserActivation
          ? (provenDrop ? protectMainLocation(runtime) : null)
          : sheetBackdropEntry<ProtectRuntimeLocation | null>({
            history: context.protect.history,
            isSheetSurface: (entry) => entry != null && (entry.name === 'taskDetail' || entry.name === 'taskResolution'),
            fallback: null,
          }),
        // T-458: the work/school resolved sheet keeps its pinned copy after the
        // backend rehydrate (the API stores no resolution reason).
        deviceReviewResolvedVariant: context.protect.runtime.deviceReviewResolvedVariant ?? null,
      },
      // The refreshed protect state ships history: [] — keep the stack the user
      // drilled through (below the to-do/task layer the toast flow re-enters)
      // so the back chevron stays live after the resolution settles (G5).
      // Extension activation: the beat renders over the overview it returns
      // to (same rule as the app-state settle — an empty history put To-Do
      // under the beat and the hand-off lost the toast; journey
      // browsing-turn-on-needs-safari-settings). Landing on main consumes it.
      history: browserActivation
        ? (provenDrop ? [protectMainLocation(runtime)] : [])
        : protectHistoryBelow(context.protect.history, ['todo', 'taskDetail', 'taskResolution']),
      // T-449 (owner, 2026-09-04, TestFlight 174: «не показався зелений екран
      // пониження ризику коли перейшла з Medium на Low» after turning the
      // extension on): the Risk-dropped beat claims a LEVEL change no matter
      // which resolution proved it — extension activation included. Without
      // a proven drop the activation still lands straight on the overview.
      location: browserActivation && !provenDrop
        ? protectMainLocation(runtime)
        : {
            name: 'taskResolution',
            screen: resolutionScreen,
          },
    },
    lastError: undefined,
  };
}

/**
 * Drill-in history up to (excluding) the first entry in `names`: the layer the
 * post-settle toast flow re-enters must not also sit on the back stack, while
 * everything beneath it (overview, Leak detection, …) stays poppable.
 */
function protectHistoryBelow(
  history: ProtectRuntimeLocation[],
  names: Array<ProtectRuntimeLocation['name']>,
): ProtectRuntimeLocation[] {
  const cut = history.findIndex((entry) => names.includes(entry.name));
  return cut === -1 ? history : history.slice(0, cut);
}

function createAddMonitoringEmailEffect(
  context: MobileAppContext,
): Extract<MobileAppEffectRequest, { type: 'addMonitoringEmail' }> | null {
  if (!context.session) return null;
  const email = (context.protect.monitoringEmailDraft || context.appState?.profile.email || '').trim();
  if (!email) return null;
  return {
    type: 'addMonitoringEmail',
    input: {
      email,
      sessionToken: update.requireStoredSession(context.session).sessionToken,
    },
  };
}

function reconcileMonitoringEmailAdded(
  context: MobileAppContext,
  appState: ProductAppState,
): MobileAppContext {
  const protect = update.withProtectRiskTrend(context.protect, update.protectStateFrom(appState));
  const runtime = { ...protect.runtime, toast: 'monitoringAdded' as const };
  return {
    ...context,
    appState,
    appStateMessage: 'Monitoring email saved.',
    protect: {
      ...protect,
      runtime,
      monitoringEmailDraft: '',
      // Keep the pre-drill stack so the Monitoring list the toast lands on
      // still pops back to Leak detection (G5).
      history: protectHistoryBelow(context.protect.history, ['monitoring']),
      location: { name: 'monitoring', screen: 'emailAdded' },
    },
    lastError: undefined,
  };
}

/**
 * A monitoring write is followed by an app-state read so the visible list is
 * backed by the persisted projection. That generic read rebuilds Protect at
 * its default route; retain only the explicit post-write emailAdded route,
 * toast, and drill-in history so refresh cannot erase the confirmation screen
 * or make the journey appear to pass from the stale add response alone.
 */
function restoreMonitoringEmailAddedAfterRefresh(
  context: MobileAppContext,
  previousContext: MobileAppContext,
): MobileAppContext {
  const previousProtect = previousContext.protect;
  if (previousProtect.location.name !== 'monitoring' || previousProtect.location.screen !== 'emailAdded') {
    return context;
  }

  return {
    ...context,
    appStateMessage: previousContext.appStateMessage,
    protect: {
      ...context.protect,
      history: previousProtect.history,
      location: previousProtect.location,
      monitoringEmailDraft: previousProtect.monitoringEmailDraft,
      runtime: {
        ...context.protect.runtime,
        toast: previousProtect.runtime.toast,
      },
    },
  };
}

function protectResolutionProofForTask(
  context: MobileAppContext,
  task: ProtectRuntimeTask,
  outcome: ResolveProtectTaskInput['outcome'],
): ResolveProtectTaskInput['proof'] {
  if (outcome === 'removed') return undefined;
  if (task.taskType === 'turn_on_browser_protection') {
    return context.extensionActivationProof?.state === 'active'
      ? extensionActivationResolutionProof(context.extensionActivationProof)
      : undefined;
  }
  if (task.taskType === 'monitor_leak' || outcome === 'monitoring') {
    return {
      type: 'monitoring_enrolled',
      provider: 'onyx-mobile',
      referenceId: task.finding?.findingId ?? task.id,
      observedAt: new Date().toISOString(),
    };
  }
  return {
    type: 'manual_user_confirmed',
    referenceId: task.id,
    observedAt: new Date().toISOString(),
  };
}

function extensionActivationResolutionProof(
  proof: CheckExtensionActivationOutput['proof'],
): ResolveProtectTaskInput['proof'] {
  if (proof.state !== 'active') return undefined;
  return {
    type: 'extension_activation',
    provider: proof.source,
    referenceId: proof.lastHeartbeatAt ?? proof.checkedAt,
    observedAt: proof.checkedAt,
    details: {
      appVersion: proof.appVersion,
      appBuild: proof.appBuild,
      extensionVersion: proof.extensionVersion,
      extensionBuild: proof.extensionBuild,
      lastHeartbeatAt: proof.lastHeartbeatAt,
    },
  };
}

function protectTaskResolvedAnalyticsEffect(
  context: MobileAppContext,
  input: ResolveProtectTaskInput,
): MobileAppEffectRequest | null {
  if (input.outcome === 'removed') return null;
  const task = context.protect.runtime.tasks.find((candidate) => candidate.id === input.taskId);
  return analyticsTrackEffect('protect_task_resolved', task ? { task_kind: protectTaskKind(task) } : {});
}

function protectTaskKind(task: ProtectRuntimeTask): string {
  return task.taskType ?? task.kind;
}

function phoneSecurityOutcome(snapshotValue: PhoneSecurityProofSnapshot): string {
  if (snapshotValue.phoneChecks.some((check) => check.status === 'issue')) return 'issues_found';
  if ((snapshotValue.timedOutProbes?.length ?? 0) > 0) return 'timed_out';
  if (snapshotValue.phoneChecks.some((check) => (
    check.status === 'unknown' || check.status === 'unsupported'
  ))) return 'unknown';
  return 'clear';
}

function phoneSecurityCompletedAnalytics(
  snapshotValue: PhoneSecurityProofSnapshot,
): MobileAppEffectRequest {
  const outcome = phoneSecurityOutcome(snapshotValue);
  const probe = snapshotValue.timedOutProbes?.[0];
  return analyticsTrackEffect('device_check_completed', {
    outcome,
    ...(probe ? { probe } : {}),
  });
}

function isPhoneSecurityProofsTimeoutError(error: unknown): boolean {
  return error instanceof Error && error.message === CHECK_PHONE_SECURITY_PROOFS_TIMEOUT_MESSAGE;
}

function defaultProtectTaskActionModel(task: ProtectRuntimeTask): ResolveProtectTaskInput['actionModel'] {
  switch (task.taskType) {
    case 'turn_on_browser_protection':
      return 'redirect_auto_recheck';
    case 'review_device_management':
      return 'redirect_guided_answer';
    case 'monitor_leak':
      return 'instant_in_app';
    case 'change_password':
    case 'update_ios':
    case 'enable_passcode':
    case 'update_onyx':
    case 'check_suspicious_activity':
    default:
      return 'redirect_manual_confirm';
  }
}

function protectResolutionIdempotencyKey(
  taskId: string,
  outcome: ResolveProtectTaskInput['outcome'],
  proof: ResolveProtectTaskInput['proof'],
): string {
  return `protect-task:${taskId}:${outcome}:${proof?.type ?? 'no-proof'}`.slice(0, 160);
}

function formatProtectResolutionError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message.includes('status 422')) return 'Onyx could not verify that protection change yet.';
  if (message.includes('status 409')) return 'This Protect task changed. Refresh and try again.';
  return 'We could not save that Protect task change. Try again in a moment.';
}

function withProtectFree(context: MobileAppContext, protectFree: MobileAppContext['protectFree']): MobileAppContext {
  return { ...context, protectFree };
}

function transitionProtectFree(snapshotValue: MobileAppSnapshot, event: MobileAppEvent): MobileAppTransitionResult {
  const { context } = snapshotValue;
  const protectFree = context.protectFree;

  switch (event.type) {
    case 'PROTECT_FREE_RUN_SCAN': {
      if (!canRunFreeScan(protectFree) || !context.session) {
        return rejected(snapshotValue, `event:${event.type}`);
      }
      const next = applyUpdate(context, update.assignProtectFreeScanStarted, event);
      const sessionToken = update.requireStoredSession(context.session).sessionToken;
      return result(
        snapshot(snapshotValue.value, next),
        [{ type: 'runFreeScan', input: { sessionToken } }],
        true,
        [`event:${event.type}`],
      );
    }
    case 'PROTECT_FREE_COOLDOWN_TICK': {
      const next = withProtectFree(context, {
        ...protectFree,
        cooldownSecondsRemaining: Math.max(0, protectFree.cooldownSecondsRemaining - 1),
      });
      return stay(snapshotValue, next, `event:${event.type}`);
    }
    case 'PROTECT_FREE_SCAN_SETTLED': {
      return stay(snapshotValue, withProtectFree(context, { ...protectFree, scanning: false }), `event:${event.type}`);
    }
    default:
      return rejected(snapshotValue, `event:${event.type}`);
  }
}

function withGetStarted(
  context: MobileAppContext,
  getStarted: MobileAppContext['getStarted'],
): MobileAppContext {
  return { ...context, getStarted };
}

/**
 * Record the GetStarted rest screen being left so GET_STARTED_BACK can restore
 * it. Transient beats (analyzing/creatingAccount/scan) and the post-account
 * screens are never pushed: they expose no back affordance and must not be
 * back targets. De-duplicated against the top entry.
 */
function pushGetStartedHistory(
  context: MobileAppContext,
  location: GetStartedRuntimeLocation,
): MobileAppContext {
  const history = context.getStartedHistory;
  if (history[history.length - 1] === location) return context;
  return { ...context, getStartedHistory: [...history, location], getStartedForwardHistory: [] };
}

function pushHistoryEntry(history: GetStartedRuntimeLocation[], location: GetStartedRuntimeLocation): GetStartedRuntimeLocation[] {
  return history[history.length - 1] === location ? history : [...history, location];
}

function withProtectRuntime(context: MobileAppContext, runtime: ProtectRuntimeMachineState['runtime']): MobileAppContext {
  return withProtect(context, { ...context.protect, runtime });
}

function withProtectLocation(context: MobileAppContext, location: ProtectRuntimeLocation): MobileAppContext {
  if (protectLocationsEqual(context.protect.location, location)) return context;
  return withProtect(context, {
    ...pushProtectHistory(context.protect),
    location,
  });
}

function protectLocationsEqual(a: ProtectRuntimeLocation, b: ProtectRuntimeLocation): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function pushProtectHistory(protect: ProtectRuntimeMachineState): ProtectRuntimeMachineState {
  const top = protect.history[protect.history.length - 1];
  if (top && protectLocationsEqual(top, protect.location)) return protect;
  return { ...protect, history: [...protect.history, protect.location] };
}

/**
 * Owner (2026-08-20), on both turn-on branches: «Вместо этого оранжевого экрана
 * появляется зеленый экран и тост». A turn-on raised from the Browsing screen —
 * whether the extension was already active (enable straight away) or had to be
 * switched on in Settings first (sheet, then return) — resolves on that same
 * screen, now painting its enabled state. Turn-ons raised from the overview
 * task card still resolve on the overview that raised them.
 */
function protectLocationAfterBrowserEnable(
  protect: ProtectRuntimeMachineState,
  runtime: ProtectRuntimeMachineState['runtime'],
): ProtectRuntimeLocation {
  return protect.location.name === 'browsing'
    ? { name: 'browsing', screen: protectBrowsingScreenFromRuntime(runtime) }
    : protectMainLocation(runtime);
}

/**
 * T-385: history records where the user stood, never what was true there.
 *
 * Every state-dependent Protect location is a projection of the runtime at push
 * time, so replaying a stored entry verbatim repaints a frame the runtime has
 * already contradicted — turning browsing protection off and pressing Back
 * restored the pushed `browsing:on` entry, so the user saw the green
 * "Activated" dashboard while the live status slot on the same screen read
 * "Not active". Re-derive each entry from the current runtime instead, using
 * exactly the projections the forward navigation uses.
 */
function reprojectProtectHistoryLocation(
  location: ProtectRuntimeLocation,
  runtime: ProtectRuntimeMachineState['runtime'],
): ProtectRuntimeLocation {
  switch (location.name) {
    case 'browsing':
      return { name: 'browsing', screen: protectBrowsingScreenFromRuntime(runtime) };
    case 'todo':
      return protectTodoLocation(runtime);
    case 'leakDetection':
      return protectLeakDetectionLocation(runtime);
    default:
      return location;
  }
}

function popProtectHistory(protect: ProtectRuntimeMachineState): ProtectRuntimeMachineState | null {
  if (protect.history.length === 0) return null;
  const leavingRiskDropped = protect.location.name === 'taskResolution'
    && protect.location.screen === 'riskDropped';

  // A re-derived entry can collapse onto the screen we are already showing
  // (Back from `browsing:off` would "return" to the same `browsing:off`). Those
  // entries are no longer a place the user can go back to, so drop them and
  // keep walking: Back after a state change lands on the first screen that
  // still means something — for the browsing flow, the Protect tab.
  let history = protect.history;
  let location: ProtectRuntimeLocation | null = null;
  while (history.length > 0) {
    const candidate = reprojectProtectHistoryLocation(history[history.length - 1], protect.runtime);
    history = history.slice(0, -1);
    if (!protectLocationsEqual(candidate, protect.location)) {
      location = candidate;
      break;
    }
  }
  if (!location) return null;

  return {
    ...protect,
    location,
    history,
    // T-275: one-shot claim pair — clear on every leave from riskDropped, including Back.
    // T-449 (review): Back also abandons the beat's hand-off. Drop the stashed
    // outcome toast and release the single-flight timer flag, otherwise the
    // next toast raised on the returned surface could never arm its own timer
    // and the beat's pending timer would clear it early.
    runtime: leavingRiskDropped
      ? { ...protect.runtime, provenRiskDrop: null, riskDropReturnScreen: null, riskDropReturnToast: null, toast: null, toastTimerArmed: false }
      : protect.runtime,
  };
}

function advanceProtectToast(protect: ProtectRuntimeMachineState): ProtectRuntimeMachineState {
  const runtime = clearProtectToast(protect.runtime);
  if (protect.location.name === 'taskResolution') {
    if (protect.location.screen === 'riskDropped') {
      const returnScreen = protect.runtime.riskDropReturnScreen ?? 'resolved';
      return {
        ...protect,
        runtime: {
          ...runtime,
          provenRiskDrop: null,
          riskDropReturnScreen: null,
          // T-449 (owner): the outcome toast appears AFTER Risk dropped — arm
          // the stashed toast exactly when the beat hands off to returnScreen.
          toast: protect.runtime.riskDropReturnToast ?? null,
          riskDropReturnToast: null,
        },
        location: { name: 'taskResolution', screen: returnScreen },
      };
    }
    if (protect.location.screen === 'removed' || protect.location.screen === 'resolved') {
      // T-449 (owner): return to the surface the task was opened FROM. The
      // captured origin is re-derived against the refreshed runtime so counts
      // and tabs stay truthful; To-Do remains the fallback for deep links.
      const origin = protect.runtime.resolutionReturnLocation ?? null;
      // Review W6-1: tasks are live-openable from the overview, To-Do, Leak
      // detection AND Phone security — every captured origin returns to its
      // own surface, not just `main`.
      const returnLocation = origin?.name === 'main'
        ? protectMainLocation(runtime)
        : origin?.name === 'leakDetection'
          ? protectLeakDetectionLocation(runtime)
          : origin?.name === 'phoneSecurity'
            ? { name: 'phoneSecurity' as const, issues: runtime.phoneChecks.some((check) => check.status === 'issue') }
            : protectTodoLocation(runtime);
      return {
        ...protect,
        runtime: { ...runtime, resolutionReturnLocation: null },
        // Landing ON the origin must also consume its history entry, or Back
        // from the returned surface would re-reveal the same surface.
        history: origin ? protectHistoryBelow(protect.history, [origin.name]) : protect.history,
        location: returnLocation,
      };
    }
  }
  if (protect.location.name === 'monitoring' && protect.location.screen === 'emailAdded') {
    return { ...protect, runtime, location: { name: 'monitoring', screen: 'list' } };
  }
  return { ...protect, runtime };
}

function disarmProtectToastTimer(protect: ProtectRuntimeMachineState): ProtectRuntimeMachineState {
  return protect.runtime.toastTimerArmed ? { ...protect, runtime: { ...protect.runtime, toastTimerArmed: false } } : protect;
}

function shouldRunProtectToastTimer(protect: ProtectRuntimeMachineState): boolean {
  if (protect.location.name === 'taskResolution' && (protect.location.screen === 'riskDropped' || protect.location.screen === 'resolved' || protect.location.screen === 'removed')) return true;
  if (protect.location.name === 'monitoring' && protect.location.screen === 'emailAdded') return true;
  // T-449 (owner: «тост має бути видно, коли шіт закривається»): while a task
  // sheet owns the screen the toast is HELD, not timed — its lifetime starts
  // when the sheet closes and the next `ready` entry arms this timer again.
  if (isProtectTaskSheetLocation(protect.location)) return false;
  return protect.runtime.toast !== null;
}

function freeScanCooldownEffects(context: MobileAppContext): MobileAppEffectRequest[] {
  // Prefer armFreeScanCooldown when the caller can adopt the bumped generation.
  // This helper remains for call sites that already hold the armed context.
  const secondsRemaining = context.protectFree.cooldownSecondsRemaining;
  const generation = context.protectFree.cooldownGeneration ?? 0;
  return secondsRemaining > 0
    ? [{ type: 'freeScanTimer', input: { secondsRemaining, generation } }]
    : [];
}

/**
 * Adopt free-scan app-state fields and arm exactly one cooldown timer chain.
 * Bumps `cooldownGeneration` so any earlier in-flight freeScanTimer is stale.
 */
function armFreeScanCooldown(
  context: MobileAppContext,
  loadedFree: MobileAppContext['protectFree'],
): { context: MobileAppContext; effects: MobileAppEffectRequest[] } {
  const secondsRemaining = loadedFree.cooldownSecondsRemaining;
  if (secondsRemaining <= 0) {
    return {
      context: withProtectFree(context, { ...loadedFree, cooldownGeneration: loadedFree.cooldownGeneration ?? 0 }),
      effects: [],
    };
  }
  const generation = (context.protectFree.cooldownGeneration ?? 0) + 1;
  const protectFree = { ...loadedFree, cooldownSecondsRemaining: secondsRemaining, cooldownGeneration: generation };
  return {
    context: withProtectFree(context, protectFree),
    effects: [{ type: 'freeScanTimer', input: { secondsRemaining, generation } }],
  };
}

// ---------------------------------------------------------------------------
// DeepScan real-scan gate (task 11)
// ---------------------------------------------------------------------------

/**
 * Hard ceiling for the DeepScan status polls armed after the replay settles
 * (~60s at the live 2s interval, mirroring productFreeScanMaxWaitMs). When the
 * budget drains the flow advances anyway with current findings, so an offline
 * or slow backend can never hang the loader.
 */
export const FIRST_SESSION_SCAN_MAX_POLLS = 30;

const TERMINAL_SCAN_RUN_STATES: ReadonlySet<FirstSessionScanPollOutput['state']> = new Set([
  'completed',
  'completed_partial',
  'failed',
  'expired',
]);

/**
 * The terminal states that actually CARRY a scan result. `failed` / `expired`
 * end the run without one, so they settle the loader (never hang the flow) but
 * grant no attestation — see `FirstSessionDeepScanRunState.attestation`.
 */
const RESULT_BEARING_SCAN_RUN_STATES: ReadonlySet<FirstSessionScanPollOutput['state']> = new Set([
  'completed',
  'completed_partial',
]);

/**
 * Did the scan-replay reply prove that a run DELIVERED a result? (T-209 /
 * pass-5 CRITICAL 1.)
 *
 * This used to read `scanRun === null || scanRun.status !== 'failed'`, i.e. it
 * treated the ABSENCE of the `firstSessionScanRun` marker as success. That is
 * precisely the failure shape: `POST /v1/first-session/scan-replay` answers
 * HTTP 200 with plain app state and no marker when scan orchestration is not
 * wired OR when `startScanForUser` throws (`services/product-api/src/routes/
 * appState.ts`, pinned by `app.test.ts` "keeps the plain app-state replay
 * contract when scan orchestration is unavailable" / "still returns replayed
 * app-state when the scan trigger itself fails"). The same shape is what a
 * legacy app-state server sends, so no marker is ambiguous by construction and
 * can never be a delivery. A legacy server still reaches an honest result
 * through content or a resumed-session completion record — see
 * `resolveFirstSessionScanOutcome`.
 *
 * `reused` is the one non-live, non-failed marker status the contract admits
 * (`firstSessionScanRunSchema`): the orchestrator joined an already fully
 * covered run whose findings are in the app state it just replayed. The switch
 * is exhaustive with a `never` check, so a new status is a compile error here
 * rather than a silent grant.
 */
function replayDeliveredResult(
  scanRun: { scanRunId: string; status: FirstSessionScanRunStatus } | null,
): boolean {
  if (scanRun === null) return false;
  const status = scanRun.status;
  switch (status) {
    case 'reused':
      return true;
    case 'queued':
    case 'running':
    case 'waiting':
    case 'failed':
      return false;
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

function normalizeFirstSessionScanReplay(output: RunFirstSessionScanOutput): {
  appState: ProductAppState;
  scanRun: { scanRunId: string; status: FirstSessionScanRunStatus } | null;
} {
  if (typeof output === 'object' && output !== null && 'appState' in output) {
    return { appState: output.appState, scanRun: output.scanRun };
  }
  return { appState: output, scanRun: null };
}

function withDeepScanRun(context: MobileAppContext, patch: Partial<FirstSessionDeepScanRunState>): MobileAppContext {
  return {
    ...context,
    onboarding: {
      ...context.onboarding,
      deepScanRun: { ...context.onboarding.deepScanRun, ...patch },
    },
  };
}

function startFirstSessionScanAfterDeviceProof(
  value: MobileAppStateValue,
  inputContext: MobileAppContext,
  trace: string,
): MobileAppTransitionResult {
  const context = withDeepScanRun(inputContext, { status: 'pending' });
  return {
    snapshot: snapshot(value, context),
    effects: [{
      type: 'runFirstSessionScan',
      input: { sessionToken: update.requireStoredSession(context.session).sessionToken },
    }],
    accepted: true,
    trace: [trace],
  };
}

function firstSessionScanPollEffect(context: MobileAppContext, scanRunId: string): MobileAppEffectRequest {
  return {
    type: 'firstSessionScanPoll',
    input: { sessionToken: update.requireStoredSession(context.session).sessionToken, scanRunId },
  };
}

/** Leave the DeepScan loader: queue the deep_scan_completed backend checkpoint. */
function advanceDeepScanCompleted(context: MobileAppContext, trace: string[]): MobileAppTransitionResult {
  return withPrependedEffect(
    enterState({ firstSession: 'advancing' }, withChapterEvent(context, 'deep_scan_completed'), true, trace),
    analyticsTrackEffect('onboarding_step_completed', { step_name: 'deep_scan' }),
  );
}

const FIRST_SESSION_ADVANCE_RECOVERY_MESSAGE =
  'This first-session step could not be saved. Refresh to continue from your latest progress.';

/**
 * Clear the queued event and either reconcile once or fail closed. Never
 * re-POST the same checkpoint from this recovery.
 */
function recoverFirstSessionAdvance(
  snapshotValue: MobileAppSnapshot,
  _error: unknown,
  trace: string,
): MobileAppTransitionResult {
  const recoveryCount = (snapshotValue.context.onboarding.firstSessionAdvanceRecoveryCount ?? 0) + 1;
  const message = FIRST_SESSION_ADVANCE_RECOVERY_MESSAGE;
  const context: MobileAppContext = {
    ...snapshotValue.context,
    onboarding: {
      ...snapshotValue.context.onboarding,
      pendingChapterEvent: null,
      firstSessionAdvanceRecoveryCount: recoveryCount,
    },
    appStateMessage: message,
    lastError: message,
  };
  if (recoveryCount > FIRST_SESSION_ADVANCE_RECOVERY_LIMIT) {
    return enterState('degraded', context, true, [`${trace}:conflictBound`]);
  }
  return enterState('authenticatedLoading', context, true, [`${trace}:reconcile`]);
}

/** Queue the backend chapter checkpoint that the `advancing` state will persist. */
function withChapterEvent(context: MobileAppContext, event: ProductFirstSessionEvent): MobileAppContext {
  return {
    ...context,
    onboarding: { ...context.onboarding, pendingChapterEvent: event },
  };
}

function requireChapterEvent(context: MobileAppContext): ProductFirstSessionEvent {
  const event = context.onboarding.pendingChapterEvent;
  if (!event) {
    throw new Error('Onboarding advancing state requires a queued chapter event.');
  }
  return event;
}

/** Set the active intra-chapter screen for the flow tail. */
function withChapterScreen(context: MobileAppContext, screen: OnboardingChapterScreen): MobileAppContext {
  return {
    ...context,
    onboarding: { ...context.onboarding, chapterScreen: screen },
  };
}

function withNextOnboardingTimerGeneration(context: MobileAppContext): MobileAppContext {
  return {
    ...context,
    onboarding: {
      ...context.onboarding,
      onboardingTimerGeneration: context.onboarding.onboardingTimerGeneration + 1,
    },
  };
}

/**
 * Back destination for a session that RESUMES mid-flow (§ resumeModel).
 *
 * A resumed run starts with an empty back-history stack, so the first screen it
 * renders carried no back control even where Figma draws one — the header
 * chevron only appears when there is somewhere to go (Issue R). Owner
 * 2026-08-05, on the resumed no-leak screen: "тут наприклад треба кнопка
 * назад", with the decision to fix the product rather than the lab scenario.
 *
 * The seeded destination is always the ENTRY screen of the preceding chapter —
 * exactly the state this same router produces when resuming at that chapter.
 * Chapter entries are static screens with a forward CTA, never a timer-owned
 * beat (saved / risk-drop / monitoring-set), so Back cannot land the user on a
 * screen that immediately bounces them forward again.
 *
 * Welcome is deliberately never a destination: it is the UNauthenticated entry
 * and its "I already have an account" CTA logs the user out (T-103).
 */
function resumeBackHistory(chapter: OnboardingChapter, context: MobileAppContext): OnboardingHistoryEntry[] {
  switch (chapter) {
    case 'deep_scan':
      return [{ firstSession: 'riskIntro', chapterScreen: null }];
    case 'leaks':
      return [{ firstSession: 'scanResult', chapterScreen: null }];
    case 'spot_fake': {
      // T-209: an unproven scan skips the whole leak chapter, so Back out of
      // Spot the Fake must return to the scan result rather than to a leak
      // screen this run never showed.
      const leakScreen = initialLeakScreen(context);
      return leakScreen === 'skip'
        ? [{ firstSession: 'scanResult', chapterScreen: null }]
        : [{ firstSession: 'leak', chapterScreen: leakScreen }];
    }
    case 'browser':
      // The spot-the-fake RESULT, not its intro: on a no-leak run Intro B is an
      // automatic timer beat (§7), so Back would land there and immediately
      // bounce the user forward again.
      return [{ firstSession: 'spotFake', chapterScreen: 'result' }];
    case 'device':
      return [{ firstSession: 'browser', chapterScreen: 'extensionIntro' }];
    // welcome / risk_intro resume at the flow entry (nothing before them that
    // Back may return to); post_flow is the closing chapter.
    default:
      return [];
  }
}

/**
 * Seed the resume back-history, but never over a stack the session already
 * walked: re-routing inside a live session must keep the real visited path.
 */
function withResumeHistory(context: MobileAppContext, history: OnboardingHistoryEntry[]): MobileAppContext {
  if (history.length === 0 || context.onboarding.history.length > 0) return context;
  return { ...context, onboarding: { ...context.onboarding, history } };
}

/**
 * Push the currently visible onboarding step onto the back-history stack before
 * navigating forward. Skipped for transient runtime states (route/advancing)
 * which are never user-visible, and de-duplicated so repeated entries to the
 * same screen do not stack.
 */
function pushHistory(context: MobileAppContext, firstSession: string): MobileAppContext {
  if (firstSession === 'route' || firstSession === 'advancing') return context;
  const entry: OnboardingHistoryEntry = { firstSession, chapterScreen: context.onboarding.chapterScreen };
  const history = context.onboarding.history;
  const top = history[history.length - 1];
  if (top && top.firstSession === entry.firstSession && top.chapterScreen === entry.chapterScreen) {
    return context;
  }
  return { ...context, onboarding: { ...context.onboarding, history: [...history, entry] } };
}

/** Pop the most recent visited step from the back-history stack. */
function popHistory(context: MobileAppContext): { entry: OnboardingHistoryEntry; context: MobileAppContext } | null {
  const history = context.onboarding.history;
  if (history.length === 0) return null;
  const entry = history[history.length - 1];
  return {
    entry,
    context: {
      ...context,
      onboarding: { ...context.onboarding, history: history.slice(0, -1), chapterScreen: entry.chapterScreen },
    },
  };
}

/** Record the quiz answer (§8.1) while progressing the spot-fake chapter. */
function withSpotFakeAnswer(context: MobileAppContext, correct: boolean): MobileAppContext {
  return {
    ...context,
    onboarding: { ...context.onboarding, chapterScreen: 'reveal', spotFakeCorrect: correct },
  };
}

/** Append a created Protect task id (deduplicated) without leaving the chapter. */
function withCreatedTask(context: MobileAppContext, taskId: string): MobileAppContext {
  if (context.onboarding.createdTaskIds.includes(taskId)) return context;
  return {
    ...context,
    onboarding: { ...context.onboarding, createdTaskIds: [...context.onboarding.createdTaskIds, taskId] },
  };
}

/**
 * Entry screen of the leak chapter, chosen from replayed funnel results (§4):
 * a critical password leak opens the actionable fix flow; only non-critical
 * leaks open the monitoring flow; no leaks shows the success screen.
 *
 * HOW MANY critical leaks there are does not change this sequence — the
 * multi-leak branch of the behaviour model rejoins at `saved`, so the machine
 * still walks leaked -> fix -> saved -> riskDrop and only the two card-bearing
 * beats render a different catalog screen. That count-dependent choice lives in
 * `leakStateIdFor` (screens/onboarding-first-session/onboardingRuntimeView.ts),
 * which reads it from the same sealed scan outcome as this function.
 */
function initialLeakScreen(context: MobileAppContext): OnboardingChapterScreen | 'skip' {
  const counts = scanOutcomeCounts(firstSessionScanOutcomeFor(context));
  // No result was obtained, so there is nothing honest to say about leaks.
  // `noLeakFound` renders the committed "We didn't find your information in
  // known breaches today" claim, which is the overclaim CLAUDE.md forbids, and
  // every other screen in the chapter asserts a leak. So the chapter is
  // skipped exactly the way §9 skips the device chapter when the funnel
  // reported no device issue (T-209 / pass-5 CRITICAL 2).
  if (!counts) return 'skip';
  if (counts.criticalLeakCount > 0) return 'leaked';
  if (counts.monitorableLeakCount > 0) return 'monitorIntro';
  return 'noLeakFound';
}

/** Whether non-critical leaks still need the monitoring flow after the fix flow (§4). */
function hasPendingMonitoring(context: MobileAppContext): boolean {
  return (scanOutcomeCounts(firstSessionScanOutcomeFor(context))?.monitorableLeakCount ?? 0) > 0;
}

function stateMatches(value: MobileAppStateValue, matcher: MobileAppStateMatcher): boolean {
  if (typeof matcher === 'string') {
    if (typeof value === 'string') return value === matcher;
    if ('authenticating' in value) return matcher === 'authenticating';
    if ('forgotPassword' in value) return matcher === 'forgotPassword';
    if ('getStarted' in value) return matcher === 'getStarted';
    if ('firstSession' in value) return matcher === 'firstSession';
    return matcher === 'commandCenter';
  }

  if (typeof value === 'string') return false;
  if ('authenticating' in matcher) return 'authenticating' in value && value.authenticating === matcher.authenticating;
  if ('forgotPassword' in matcher) return 'forgotPassword' in value && value.forgotPassword === matcher.forgotPassword;
  if ('getStarted' in matcher) return 'getStarted' in value && value.getStarted === matcher.getStarted;
  if ('firstSession' in matcher) return 'firstSession' in value && value.firstSession === matcher.firstSession;
  return 'commandCenter' in value && 'commandCenter' in matcher && value.commandCenter === matcher.commandCenter;
}

function isAuthenticatingState(value: MobileAppStateValue): value is Extract<MobileAppNestedStateValue, { authenticating: string }> {
  return typeof value !== 'string' && 'authenticating' in value;
}

function isForgotPasswordState(value: MobileAppStateValue): value is Extract<MobileAppNestedStateValue, { forgotPassword: string }> {
  return typeof value !== 'string' && 'forgotPassword' in value;
}

function isGetStartedState(value: MobileAppStateValue): value is Extract<MobileAppNestedStateValue, { getStarted: string }> {
  return typeof value !== 'string' && 'getStarted' in value;
}

function isFirstSessionState(value: MobileAppStateValue): value is Extract<MobileAppNestedStateValue, { firstSession: string }> {
  return typeof value !== 'string' && 'firstSession' in value;
}

function isCommandCenterState(value: MobileAppStateValue): value is Extract<MobileAppNestedStateValue, { commandCenter: string }> {
  return typeof value !== 'string' && 'commandCenter' in value;
}
