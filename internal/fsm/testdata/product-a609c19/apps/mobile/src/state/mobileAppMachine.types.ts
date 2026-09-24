import type {
  MobileScanStatus,
  MobileScanSubjectType,
  ProductAppState,
  ProductFirstSessionEvent,
  ProductFunnelQuizAnswerRequest,
  ProductProtectTaskResolutionRequest,
  ProductRiskStatus,
  ProductScanResults,
  ProductSession,
} from '../api';
import type { OnboardingDemonstrativeRisk } from './onboardingDemonstrativeRisk';
import type { TabKey } from '../screens/types';
import type { FirstSessionScanAttestation, SealedFunnelScanResults } from './firstSessionScanOutcome';
import type { FamilyInviteRuntimeState } from '../screens/family-invite/familyInviteRuntimeState';
import type { ProtectFreeRuntimeState } from '../screens/protect/free/protectFreeRuntimeState';
import type { ForgotPasswordRuntimeState, ForgotPasswordTimerPhase } from '../screens/forgot-password/forgotPasswordRuntimeState';
import type { GetStartedQuizAnswers } from '../screens/get-started/getStartedExposureModel';
import type { GetStartedRuntimeLocation, GetStartedRuntimeState, GetStartedTimerPhase } from '../screens/get-started/getStartedRuntimeState';
import type { ProtectRuntimeMachineState } from './protectRuntimeState';
import type { ForgotPasswordScreenStateId } from '../stories/forgotPasswordScreenCatalog';
import type {
  GetStartedFreeScreenStateId,
  GetStartedScreenStateId,
} from '../screens/get-started/getStartedFamilyIdentity';
import type { ProtectFreeScreenStateId } from '../stories/protectFreeScreenCatalog';
import type { OnboardingFirstSessionScreenStateId } from '../stories/onboardingFirstSessionScreenCatalog';
import type { ProtectSteadyStateScreenStateId } from '../stories/protectSteadyStateScreenCatalog';
import type { ProtectTaskScreenStateId } from '../stories/protectTaskScreenCatalog';
import type { ProfileScreenStateId } from '../stories/profileScreenCatalog';
import type { TrainScreenStateId } from '../stories/trainScreenCatalog';
import type { ProtectLiveVisualState } from '../screens/protect/live/ProtectLiveVisualHost';
import type { StoredProductSession } from '../session';
import type { ExtensionActivationProof } from '../effects/extensionActivationService';
import type { ProfileLocation, ProtectedCanonicalLocation, TrainLocation, CanonicalLocation } from './appLocations';
import type { TrainRuntimeMutationKind, TrainRuntimeSyncState } from './trainRuntimeSync';

export type VisualScenario = 'calm' | 'alert' | 'onboarding';
export type VisualAuthRoute = 'signIn' | 'createAccount';
export type VisualOnboardingStep = 'welcome' | 'deviceScan';
export type VisualDeviceScanVariant = 'idle' | 'scanning' | 'success' | 'failed' | 'permissionDenied';

export type VisualRoute = {
  scenario?: VisualScenario;
  tab?: TabKey;
  diagnostics?: boolean;
  auth?: VisualAuthRoute;
  onboardingStep?: VisualOnboardingStep;
  deviceScanVariant?: VisualDeviceScanVariant;
  onboardingFirstSessionState?: OnboardingFirstSessionScreenStateId;
  protectSteadyState?: ProtectSteadyStateScreenStateId;
  getStartedState?: GetStartedScreenStateId;
  getStartedFreeState?: GetStartedFreeScreenStateId;
  protectFreeState?: ProtectFreeScreenStateId;
  protectFreeShellState?: ProtectFreeScreenStateId;
  protectLiveState?: ProtectLiveVisualState;
  protectTaskState?: ProtectTaskScreenStateId;
  profileState?: ProfileScreenStateId;
  trainState?: TrainScreenStateId;
  forgotPasswordState?: ForgotPasswordScreenStateId;
};

/**
 * Boundary port for visual (screenshot fixture) mode. The production machine
 * only knows this interface; the fixture implementation is injected from the
 * app entry point so fixture data never ships inside the machine module.
 */
export type VisualStatePort = {
  session: StoredProductSession;
  email: string;
  getAppState: (scenario: VisualScenario, onboardingStep?: VisualOnboardingStep) => ProductAppState;
  getScanStatus: (scenario: VisualScenario, deviceScanVariant?: VisualDeviceScanVariant) => MobileScanStatus | null;
  getScanMessage?: (deviceScanVariant?: VisualDeviceScanVariant) => string;
  parseRoute: (url: string | null | undefined) => VisualRoute;
};

/**
 * Pre-auth funnel entry seed.
 *
 * The Get Started free funnel has NO persisted checkpoint (it runs before an
 * account exists), so unlike the first-session chapters — which resume from
 * `appState.firstSessionStep` via `withFirstSessionStep` — there was no way to
 * open the app on a later funnel beat. Everything below `welcome` was therefore
 * only reachable by walking ~15 taps, which is why the create-account screen
 * was reviewable only through the static Storybook `stateId` playground
 * (owner 2026-08-06: «воно має бути у сценаріях а не тут»).
 *
 * `location` is where the pre-auth entry gate opens the funnel; `history` is
 * the Back stack it opens with, so a seeded mid-funnel entry still has the same
 * Back affordance the walked flow would have given it.
 *
 * `answers` is the quiz state the walked funnel would have recorded on the way
 * to `location`. The exposure Result and its atmosphere are ANSWER-derived
 * (resolveExposureResult / T-234 riskStatusFromOverrides), so a mid-funnel seed
 * without answers renders the empty-quiz Moderate fallback — a state the walked
 * flow can never show on this step. Optional: a seed at `welcome` has no
 * answers yet, exactly like the real entry.
 */
export type GetStartedEntrySeed = {
  location: GetStartedRuntimeLocation;
  history?: readonly GetStartedRuntimeLocation[];
  answers?: GetStartedQuizAnswers;
};

export type MobileAppInput = {
  visualMode?: boolean;
  visual?: VisualStatePort;
  initialVisualScenario?: VisualScenario;
  initialVisualTab?: TabKey;
  /** Optional funnel entry seed; absent means the canonical `welcome` entry. */
  initialGetStarted?: GetStartedEntrySeed;
  /**
   * Scenario-harness-only AppsFlyer autologin token. Production boots never set
   * this; browser/Lab mounts use it to stage `APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED`
   * without the iOS SDK bridge so the real machine can request
   * `exchangeAppsFlyerAutologin`.
   */
  scenarioAppsFlyerAutologinToken?: string;
};

/**
 * `success` / `successWithBody` / `riskDrop` / `preparingProfile` are the
 * control-less auto-advance beats capped by the shared budget (T-213);
 * `successWithBody` is the reading tier for screens carrying body copy. `riskLowered` is the post-flow recap beat
 * — it carries a Continue button and a confetti choreography, so it is a
 * separate phase and keeps its own presentation dwell.
 */
export type OnboardingTimerPhase = 'deepScan' | 'success' | 'successWithBody' | 'riskDrop' | 'riskLowered' | 'monitorSetup' | 'preparingProfile' | 'spotFakeIntro' | 'spotFakeReveal';

/**
 * Intra-chapter screen progression for the flow tail (slice C). The backend
 * only records one checkpoint per chapter, so the finer screen sequence inside
 * a chapter (leak fix/saved/riskDrop, quiz, browser nudge, device skip, etc.)
 * is tracked client-side here and mirrors the approved Behavior Model.
 */
export type OnboardingLeakScreen =
  | 'leaked'
  | 'fix'
  | 'saved'
  | 'riskDrop'
  | 'monitorIntro'
  | 'leaksToMonitor'
  | 'monitorDetails'
  | 'monitoringSet'
  | 'monitorRiskDrop'
  | 'noLeakFound';

export type OnboardingSpotFakeScreen = 'intro' | 'quiz' | 'reveal' | 'result';

export type OnboardingBrowserScreen =
  | 'extensionIntro'
  | 'extensionSetup'
  | 'extensionNudge'
  | 'extensionOn'
  | 'riskDrop'
  | 'savedForLater';

export type OnboardingDeviceScreen = 'intro' | 'outdatedOs' | 'updatedOs' | 'saved' | 'riskDrop';

// T-253: `allSet` retired — PreparingProfile now ends the chapter itself.
export type OnboardingPostFlowScreen = 'riskLowered' | 'preparingProfile';

export type OnboardingChapterScreen =
  | OnboardingLeakScreen
  | OnboardingSpotFakeScreen
  | OnboardingBrowserScreen
  | OnboardingDeviceScreen
  | OnboardingPostFlowScreen;

/**
 * One visited onboarding step, captured so the Back control can restore the
 * exact previous screen the user saw — across chapter boundaries, without
 * rewinding the backend checkpoint. `firstSession` is the nested runtime value
 * (welcome | riskIntro | scanResult | leak | ...) and `chapterScreen` the
 * intra-chapter screen (null outside the tail chapters).
 */
export type OnboardingHistoryEntry = {
  firstSession: string;
  chapterScreen: OnboardingChapterScreen | null;
};

/**
 * Real-scan gate for the DeepScan loader (task 11 — "ideally backend data").
 * `pending` while the scan-replay request is in flight, `polling` while a live
 * backend scan run is being polled, `settled` once the scan is terminal (or
 * failed / poll budget exhausted — the honest offline path). The loader leaves
 * DeepScan only when the theatrical floor elapsed AND the run is settled.
 */
export type FirstSessionDeepScanRunState = {
  status: 'idle' | 'awaitingProof' | 'pending' | 'polling' | 'settled';
  scanRunId: string | null;
  /** True once the onboardingTimer deepScan floor elapsed. */
  floorElapsed: boolean;
  /** Remaining status polls before the hard ceiling advances the flow anyway. */
  pollsRemaining: number;
  /**
   * SCAN ATTESTATION (T-209 / pass-5 CRITICAL 1) — what this session's run(s)
   * established about result acquisition. Written to `delivered` only on
   * branches that read a positive, result-bearing terminal signal off the
   * wire; every other outcome leaves it at `attempted`.
   *
   * `status: 'settled'` cannot answer this — it is deliberately also set on the
   * offline graceful path (`runFirstSessionScan` error) and on the poll
   * ceiling, i.e. exactly the cases where nothing was learned. Nor can a
   * boolean: the previous `resultProven` flag could not distinguish "no attempt
   * in this session" (a restored session, where a server completion record may
   * legitimately stand in) from "attempted and failed" (where it may not), so
   * cached backend history re-proved a run that had just errored.
   *
   * Read only through `resolveFirstSessionScanOutcome`.
   */
  attestation: FirstSessionScanAttestation;
};

/**
 * Runtime view of the approved onboarding Behavior Model. The backend
 * checkpoint (firstSessionStep) plus replayed funnel data (riskStatus /
 * scanResults) drive guard-based chapter routing. Tasks created during the
 * flow accumulate here so the Protect tab can render them after handoff.
 */
export type OnboardingRuntimeState = {
  /** T-275: null when app state has not yet projected current risk. */
  riskStatus: ProductRiskStatus | null;
  /**
   * T-275: first observed server risk for this onboarding session (audit only).
   * Not used alone as drop proof.
   */
  sessionStartRiskStatus?: ProductRiskStatus | null;
  /**
   * T-275: last successful server-projected risk. Consecutive ordered pairs
   * from this field produce one-shot pendingRiskDrop claims.
   */
  lastObservedServerRiskStatus?: ProductRiskStatus | null;
  /** Canonical handle that travelled with lastObservedServerRiskStatus. */
  lastObservedServerRiskHandle?: number | null;
  /**
   * T-275: monotonic counter for successful server risk observations.
   * Mints correlation ids so drop claims bind to the response that created them.
   */
  riskObservationSeq?: number;
  /**
   * T-275: unconsumed consecutive descending server pair, correlated to the
   * minting app-state response. Consumed at most once with matching correlation.
   */
  pendingRiskDrop?: {
    from: ProductRiskStatus;
    to: ProductRiskStatus;
    fromHandle?: number;
    toHandle?: number;
    correlationId: string;
  } | null;
  /**
   * T-275 residue: the chapter-owned persist still binds this key, but since
   * T-566 nothing in onboarding consumes `pendingRiskDrop` — the beats are
   * demonstrative. Kept so the Protect-side observation chain is unchanged.
   */
  riskDropClaimCorrelationId?: string | null;
  /**
   * True while a leak-chapter save persist (in-place loadAppState) is in
   * flight. That response is the chapter-owned observation allowed to bind
   * riskDropClaimCorrelationId. Boot loads must not set this.
   */
  pendingChapterRiskPersist?: boolean;
  /**
   * Claim currently owned by a mounted drop beat: in onboarding the section's
   * demonstrative step (T-566) or the postflow run (T-567); in Protect the
   * T-275 server-proven pair. Cleared when leaving the claim screen.
   */
  activeRiskDropClaim?: {
    from: ProductRiskStatus;
    to: ProductRiskStatus;
    fromHandle?: number;
    toHandle?: number;
    correlationId: string;
  } | null;
  /**
   * T-566/T-567: onboarding-only demonstrative risk counter (owner 2026-09-13:
   * the first session lowers the risk demonstratively after every completed
   * section, no server proof). `start` is the recap ghost, `current` the
   * handle the next beat / the recap paints. Null until the first section
   * completes; never consulted outside the first session.
   */
  demonstrativeRisk?: OnboardingDemonstrativeRisk | null;
  /**
   * Replayed funnel results, SEALED (T-209 / pass-5 CRITICAL 1+2). The buckets
   * are unreadable by design: every consumer goes through
   * `resolveFirstSessionScanOutcome`, which needs `deepScanRun.attestation`, so
   * a new consumer cannot re-derive "clean" from an empty array.
   */
  scanResults: SealedFunnelScanResults;
  createdTaskIds: string[];
  /** Backend chapter event queued for the `advancing` state to persist. */
  pendingChapterEvent: ProductFirstSessionEvent | null;
  /**
   * Consecutive failed checkpoint persists. One reconcile is allowed; the next
   * failure is a recoverable degraded screen so the same event cannot loop.
   */
  firstSessionAdvanceRecoveryCount?: number;
  /** Current screen inside the active tail chapter (null outside the tail). */
  chapterScreen: OnboardingChapterScreen | null;
  /** Whether the user answered the spot-the-fake quiz correctly (§8.1). */
  spotFakeCorrect: boolean | null;
  /**
   * Whether the family-owner InviteFamily step has been sent or deferred.
   * Only the `family_owner` onboarding variant inserts that step before the
   * regular chapters; once handled the router falls through to normal routing.
   */
  familyInvitesHandled: boolean;
  /**
   * Local-only acknowledgment for standard returning accounts whose backend
   * first_session_step is still incomplete. Once acknowledged, routing resumes
   * at the backend checkpoint without mutating product app-state.
   */
  returningAccountBridgeAcknowledged: boolean;
  /**
   * Linear stack of visited steps (oldest first). Each forward navigation
   * pushes the screen the user was on; ONBOARDING_BACK pops the top and
   * restores it, so Back always returns to the exact previous screen.
   */
  history: OnboardingHistoryEntry[];
  /** Live backend scan run gating the DeepScan loader (task 11). */
  deepScanRun: FirstSessionDeepScanRunState;
  /** Monotonic identity for the currently armed former content-beat timer. */
  onboardingTimerGeneration: number;
};

export type MobileAppContext = {
  visualMode: boolean;
  visual: VisualStatePort | null;
  session: StoredProductSession | null;
  appState: ProductAppState | null;
  authMessage: string;
  /**
   * User-visible auth failure for the sign-in screen (feedback: a failed login
   * gave zero feedback). Non-null only after a failed login/registration or a
   * missing-credentials submit; cleared when the user edits credentials or a
   * new attempt starts, so it never lingers as a stale banner.
   */
  authError: string | null;
  appStateMessage: string;
  authEmail: string;
  authPassword: string;
  /** Auth presentation is a canonical location, not a visual-only fixture. */
  authRoute: VisualAuthRoute;
  fSessionId: string | null;
  /** In-memory only; its nested `token` field is redacted from machine telemetry. */
  pendingAppsFlyerAutologinRetry: {
    token: string;
    retryAttempt: 0 | 1;
    /** Originating open_id for attempt outcomes across warm boundaries. */
    openId?: string;
  } | null;
  /**
   * Safe opaque open correlation for in-flight AppsFlyer autologin attempt
   * outcomes (exchange/persist/skip/login_completed). Survives warm app_opened.
   */
  pendingAppsFlyerOpenId: string | null;
  pendingWeb2AppLoginMethod: 'autologin' | 'manual_fallback' | null;
  selectedTab: TabKey;
  /** Canonical drill-in locations that used to be App/Profile component state. */
  trainLocation: TrainLocation;
  profileLocation: ProfileLocation;
  /** Validated internal destination kept only until the next successful auth. */
  pendingReturnTo: ProtectedCanonicalLocation | null;
  visualScenario: VisualScenario;
  pendingSession: ProductSession | null;
  scanSubjectType: MobileScanSubjectType;
  scanSubject: string;
  scanStatus: MobileScanStatus | null;
  scanBusy: boolean;
  scanMessage: string;
  onboarding: OnboardingRuntimeState;
  protect: ProtectRuntimeMachineState;
  extensionActivationProof: ExtensionActivationProof | null;
  forgotPassword: ForgotPasswordRuntimeState;
  getStarted: GetStartedRuntimeState;
  /**
   * Visited GetStarted rest screens (oldest first). Forward funnel transitions
   * push the screen being left; GET_STARTED_BACK pops the top so Back always
   * returns to the previous step instead of dumping the user on Login (T1/G2).
   */
  getStartedHistory: GetStartedRuntimeLocation[];
  /** Rest stack populated only by GET_STARTED_BACK; forward is enabled only while non-empty. */
  getStartedForwardHistory: GetStartedRuntimeLocation[];
  /**
   * Funnel entry seed carried from {@link MobileAppInput.initialGetStarted}.
   * `GET_STARTED_START` is what actually opens the funnel (cold-start entry
   * gate, "Get started" on Login, post-deletion re-entry), so the seed has to
   * survive on context until that event fires — otherwise the gate would reset
   * the seeded location back to `welcome`.
   */
  getStartedEntry?: GetStartedEntrySeed;
  familyInvite: FamilyInviteRuntimeState;
  protectFree: ProtectFreeRuntimeState;
  lastError?: string;
  /**
   * Last non-null screen name for which `screen_viewed` was emitted.
   * Null-name states do not clear this, so A→null→A is not a new view of A.
   */
  lastScreenViewedName: string | null;
  /**
   * Machine-owned T-208 mutation freshness. Successful start/command/quiz ACKs
   * mark Train dirty so Protect/Profile cannot consume a stale ProductAppState.
   */
  trainRuntimeSync: TrainRuntimeSyncState;
};

/**
 * UI initiator for a dispatched machine event. Optional: programmatic,
 * effect, and legacy sends omit it. The interpreter ignores this field.
 */
export type MachineEventSource = {
  testID: string;
};

/** Allowlisted stable, non-PII properties for Train user-action events. */
export type TrainRuntimeActionPayload = {
  courseId?: string;
  lessonId?: string;
  nodeId?: string;
  optionId?: string;
  questionNodeId?: string;
  certificateId?: string;
  mutationId?: string;
  attemptId?: string;
  destination?: 'catalog' | 'course' | 'lesson' | 'quiz' | 'certificates' | 'certificate';
  attempt?: number;
  questionIndex?: number;
};

export const trainRuntimeActionTypes = [
  'TRAIN_OPEN_COURSES',
  'TRAIN_OPEN_COURSE',
  'TRAIN_OPEN_LESSON',
  'TRAIN_OPEN_CERTIFICATES',
  'TRAIN_OPEN_CERTIFICATE',
  'TRAIN_OPEN_CERTIFICATE_ENTRY',
  'TRAIN_OPEN_QUIZ',
  'TRAIN_BACK_HOME',
  'TRAIN_BACK_COURSE',
  'TRAIN_BACK_LESSON_STEP',
  'TRAIN_BACK_QUIZ_QUESTION',
  'TRAIN_START_COURSE',
  'TRAIN_KC_SELECTED',
  'TRAIN_KC_EXPLANATION_REVEALED',
  'TRAIN_RECORD_CHOICE',
  'TRAIN_COMPLETE_NODE',
  'TRAIN_COMPLETE_TODO',
  'TRAIN_SKIP_TODO',
  'TRAIN_COMPLETE_LESSON',
  'TRAIN_OPEN_NEXT_LESSON_OR_COURSE',
  'TRAIN_START_CERTIFICATE_QUIZ',
  'TRAIN_SELECT_CERTIFICATE_QUIZ_ANSWER',
  'TRAIN_CONTINUE_CERTIFICATE_QUIZ',
  'TRAIN_SUBMIT_CERTIFICATE_QUIZ',
  'TRAIN_RETRY_CERTIFICATE_QUIZ',
  'TRAIN_RETRY_LESSON',
  'TRAIN_REPLAY_CONFETTI',
  'TRAIN_OPEN_NEXT_COURSE',
] as const;

export type TrainRuntimeActionType = (typeof trainRuntimeActionTypes)[number];

export type TrainRuntimeActionEvent = {
  type: TrainRuntimeActionType;
} & TrainRuntimeActionPayload;

const trainRuntimeActionTypeSet = new Set<string>(trainRuntimeActionTypes);

export function isTrainRuntimeActionEvent(event: { type: string }): event is TrainRuntimeActionEvent {
  return trainRuntimeActionTypeSet.has(event.type);
}

type MobileAppEventVariant =
  | { type: 'BOOT' }
  | { type: 'AUTH_EMAIL_CHANGED'; email: string }
  | { type: 'AUTH_PASSWORD_CHANGED'; password: string }
  | { type: 'FUNNEL_SESSION_RECEIVED'; fSessionId: string }
  | { type: 'SUBMIT_LOGIN' }
  | { type: 'SUBMIT_REGISTER' }
  | { type: 'AUTH_SUCCESS'; session: StoredProductSession }
  | { type: 'APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED'; token: string; openId?: string }
  | { type: 'APPSFLYER_AUTOLOGIN_NETWORK_AVAILABLE' }
  | { type: 'APPSFLYER_AUTOLOGIN_SESSION_READY'; session: StoredProductSession }
  | { type: 'REFRESH_APP_STATE' }
  | { type: 'LOGOUT' }
  | { type: 'NAVIGATE'; tab: TabKey }
  /** A guarded request from the URL transport or a user-visible drill-in. */
  | { type: 'ROUTE_REQUESTED'; location: CanonicalLocation; returnTo?: ProtectedCanonicalLocation | null; source: 'browser-initial' | 'browser-pop' | 'user' | 'auth-complete' }
  /** Machine-owned Train progression; internal lesson beats are not URL intents. */
  | { type: 'TRAIN_LOCATION_CHANGED'; location: TrainLocation }
  | TrainRuntimeActionEvent
  | { type: 'TRAIN_RUNTIME_MUTATION_STARTED'; mutationId: string; kind: TrainRuntimeMutationKind }
  | { type: 'TRAIN_RUNTIME_MUTATION_SETTLED'; mutationId: string; kind: TrainRuntimeMutationKind; success: boolean }
  | { type: 'VISUAL_ROUTE_CHANGED'; url: string | null }
  | { type: 'SCAN_SUBJECT_TYPE_CHANGED'; subjectType: MobileScanSubjectType }
  | { type: 'SCAN_SUBJECT_CHANGED'; subject: string }
  | { type: 'SUBMIT_SCAN' }
  | { type: 'ONBOARDING_CONTINUE' }
  | { type: 'ONBOARDING_ACKNOWLEDGE_RETURNING' }
  | { type: 'ONBOARDING_RUN_DEEP_SCAN' }
  | { type: 'ONBOARDING_REVIEW_FINDINGS' }
  | { type: 'ONBOARDING_TIMER_ELAPSED'; phase: OnboardingTimerPhase }
  // Flow tail (slice C). Advance the current chapter screen / answer the quiz /
  // resolve a chapter; the interpreter mirrors the approved Behavior Model.
  | { type: 'ONBOARDING_ADVANCE_SCREEN' }
  | { type: 'ONBOARDING_BACK' }
  | { type: 'ONBOARDING_CREATE_TASK'; taskId: string }
  | { type: 'ONBOARDING_ANSWER_QUIZ'; correct: boolean }
  | { type: 'ONBOARDING_REPLAY_STAMP_ATTENTION' }
  | { type: 'ONBOARDING_SET_UP_EXTENSION' }
  | { type: 'ONBOARDING_SKIP_EXTENSION' }
  | { type: 'ONBOARDING_SAVE_EXTENSION_FOR_LATER' }
  | { type: 'ONBOARDING_CHECK_EXTENSION_ACTIVATION' }
  | { type: 'ONBOARDING_EXTENSION_ACTIVATED' }
  | { type: 'ONBOARDING_GO_TO_PROTECT' }
  | { type: 'PROTECT_BACK' }
  | { type: 'PROTECT_OPEN_TODO' }
  | { type: 'PROTECT_SWITCH_TODO_TAB'; tab: 'active' | 'resolved' }
  | { type: 'PROTECT_OPEN_TASK_DETAIL'; taskState: 'critical' | 'nonCritical' | 'resolved'; taskId: string }
  | { type: 'PROTECT_SEE_FIX' }
  | { type: 'PROTECT_CONFIRM_RESOLVED' }
  | { type: 'PROTECT_REMOVE_TASK' }
  /** Show "Have you changed it?" confirm step (archetype: redirectConfirm) */
  /** User confirmed manual action (e.g. "Yes, I've changed it") → resolve */
  | { type: 'PROTECT_TASK_CONFIRMED' }
  /** Branch answer from task-6 guided-answer OptionRowGroup */
  | { type: 'PROTECT_TASK_GUIDED_ANSWER'; branch: 'familiar' | 'unfamiliar' | 'workSchool' }
  /** Foreground re-check after a redirect (archetype: redirectRecheck / inAppProcess) */
  | { type: 'PROTECT_TASK_RECHECK' }
  /** Device-review remove sheet: "It says my phone is managed" opens the escalate sheet (task 6). */
  | { type: 'PROTECT_TASK_ESCALATE' }
  /** T-449: «I've already handled it» — resolve as user-dismissed, unverified. */
  | { type: 'PROTECT_TASK_HANDLED' }
  /** Escalate / still-seeing sheets: user asked for Apple Support (external hop; task stays open). */
  | { type: 'PROTECT_TASK_CONTACT_SUPPORT' }
  /** A public OS destination was unavailable; keep the task open and explain why. */
  | { type: 'PROTECT_TASK_DESTINATION_UNAVAILABLE'; reason: string }
  | { type: 'PROTECT_TOAST_ELAPSED' }
  | { type: 'PROTECT_OPEN_BROWSING' }
  | { type: 'PROTECT_TURN_OFF_BROWSING' }
  | { type: 'PROTECT_OPEN_BROWSING_SETUP' }
  | { type: 'PROTECT_SAVE_BROWSING_FOR_LATER' }
  | { type: 'PROTECT_OPEN_BROWSER_SHEET' }
  | { type: 'PROTECT_ENABLE_BROWSER_PROTECTION' }
  | { type: 'PROTECT_DISMISS_SHEET' }
  | { type: 'PROTECT_OPEN_PHONE_SECURITY' }
  | { type: 'PROTECT_OPEN_LEAK_DETECTION' }
  /** Run a fresh leak scan for the account from the Leak detection screen (server enforces cooldown/reuse). */
  | { type: 'PROTECT_RUN_LEAK_SCAN' }
  | { type: 'PROTECT_SWITCH_LEAK_TAB'; tab: 'all' | 'monitoring' }
  | { type: 'PROTECT_OPEN_MONITORING_LIST' }
  | { type: 'PROTECT_OPEN_MONITORING_TASK_DETAIL' }
  | { type: 'PROTECT_ADD_MONITORING_EMAIL' }
  | { type: 'PROTECT_MONITORING_EMAIL_CHANGED'; email: string }
  | { type: 'PROTECT_SUBMIT_MONITORING_EMAIL' }
  /** Protect learning CTA opens the canonical course or resumes its lesson. */
  | { type: 'PROTECT_OPEN_TRAIN'; location?: Extract<TrainLocation, { screen: 'course' | 'lesson' }> }
  | { type: 'FORGOT_PASSWORD_START' }
  | { type: 'FORGOT_PASSWORD_BACK_TO_LOGIN' }
  | { type: 'FORGOT_PASSWORD_EMAIL_CHANGED'; email: string }
  | { type: 'FORGOT_PASSWORD_SUBMIT_EMAIL' }
  | { type: 'FORGOT_PASSWORD_RESEND_EMAIL' }
  | { type: 'FORGOT_PASSWORD_OPEN_RESET'; token: string }
  | { type: 'FORGOT_PASSWORD_NEW_PASSWORD_CHANGED'; password: string }
  | { type: 'FORGOT_PASSWORD_CONFIRM_PASSWORD_CHANGED'; password: string }
  | { type: 'FORGOT_PASSWORD_SUBMIT_NEW_PASSWORD' }
  | { type: 'GET_STARTED_START' }
  | { type: 'GET_STARTED_TRY_LOGIN' }
  | { type: 'GET_STARTED_SIGN_IN' }
  | { type: 'GET_STARTED_CONTINUE' }
  | { type: 'GET_STARTED_BACK' }
  | { type: 'GET_STARTED_FORWARD' }
  | { type: 'GET_STARTED_SELECT_CHOICE'; label: string }
  | { type: 'GET_STARTED_EMAIL_CHANGED'; email: string }
  | { type: 'GET_STARTED_PASSWORD_CHANGED'; password: string }
  | { type: 'GET_STARTED_CREATE_ACCOUNT' }
  | { type: 'GET_STARTED_RUN_SCAN' }
  | { type: 'GET_STARTED_FINISH' }
  | { type: 'FAMILY_INVITE_MEMBER_CHANGED'; index: 0 | 1; email: string }
  | { type: 'FAMILY_INVITE_SEND' }
  | { type: 'FAMILY_INVITE_DEFER' }
  | { type: 'FAMILY_INVITE_SUCCESS_CONTINUE' }
  | { type: 'PROTECT_FREE_RUN_SCAN' }
  | { type: 'PROTECT_FREE_SCAN_SETTLED' }
  // Dead code (no sender since freeScanTimer took over the cooldown loop);
  // kept only because guardrail event catalogues pin the full union. Remove
  // together with its interpreter branch and the guardrail catalogue rows.
  | { type: 'PROTECT_FREE_COOLDOWN_TICK' }
  | { type: 'APP_FOREGROUNDED' }
  | { type: 'RETRY' };

export type MobileAppEvent = MobileAppEventVariant & {
  _source?: MachineEventSource;
};

export type MobileAppEventType = MobileAppEvent['type'];

/**
 * Runtime registry of every event type the machine accepts. Kept in lockstep
 * with the `MobileAppEvent` union by the bidirectional compile-time pins below:
 * adding a union member without listing it here (or vice versa) fails typecheck.
 * Guardrail tests use this as the introspectable source of truth for signals.
 */
export const mobileAppEventTypes = [
  'BOOT',
  'AUTH_EMAIL_CHANGED',
  'AUTH_PASSWORD_CHANGED',
  'FUNNEL_SESSION_RECEIVED',
  'SUBMIT_LOGIN',
  'SUBMIT_REGISTER',
  'AUTH_SUCCESS',
  'APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED',
  'APPSFLYER_AUTOLOGIN_NETWORK_AVAILABLE',
  'APPSFLYER_AUTOLOGIN_SESSION_READY',
  'REFRESH_APP_STATE',
  'LOGOUT',
  'NAVIGATE',
  'ROUTE_REQUESTED',
  'TRAIN_LOCATION_CHANGED',
  ...trainRuntimeActionTypes,
  'TRAIN_RUNTIME_MUTATION_STARTED',
  'TRAIN_RUNTIME_MUTATION_SETTLED',
  'VISUAL_ROUTE_CHANGED',
  'SCAN_SUBJECT_TYPE_CHANGED',
  'SCAN_SUBJECT_CHANGED',
  'SUBMIT_SCAN',
  'ONBOARDING_CONTINUE',
  'ONBOARDING_ACKNOWLEDGE_RETURNING',
  'ONBOARDING_RUN_DEEP_SCAN',
  'ONBOARDING_REVIEW_FINDINGS',
  'ONBOARDING_TIMER_ELAPSED',
  'ONBOARDING_ADVANCE_SCREEN',
  'ONBOARDING_BACK',
  'ONBOARDING_CREATE_TASK',
  'ONBOARDING_ANSWER_QUIZ',
  'ONBOARDING_REPLAY_STAMP_ATTENTION',
  'ONBOARDING_SET_UP_EXTENSION',
  'ONBOARDING_SKIP_EXTENSION',
  'ONBOARDING_SAVE_EXTENSION_FOR_LATER',
  'ONBOARDING_CHECK_EXTENSION_ACTIVATION',
  'ONBOARDING_EXTENSION_ACTIVATED',
  'ONBOARDING_GO_TO_PROTECT',
  'PROTECT_BACK',
  'PROTECT_OPEN_TODO',
  'PROTECT_SWITCH_TODO_TAB',
  'PROTECT_OPEN_TASK_DETAIL',
  'PROTECT_SEE_FIX',
  'PROTECT_CONFIRM_RESOLVED',
  'PROTECT_REMOVE_TASK',
  'PROTECT_TASK_CONFIRMED',
  'PROTECT_TASK_GUIDED_ANSWER',
  'PROTECT_TASK_RECHECK',
  'PROTECT_TASK_ESCALATE',
  'PROTECT_TASK_HANDLED',
  'PROTECT_TASK_CONTACT_SUPPORT',
  'PROTECT_TASK_DESTINATION_UNAVAILABLE',
  'PROTECT_TOAST_ELAPSED',
  'PROTECT_OPEN_BROWSING',
  'PROTECT_TURN_OFF_BROWSING',
  'PROTECT_OPEN_BROWSING_SETUP',
  'PROTECT_SAVE_BROWSING_FOR_LATER',
  'PROTECT_OPEN_BROWSER_SHEET',
  'PROTECT_ENABLE_BROWSER_PROTECTION',
  'PROTECT_DISMISS_SHEET',
  'PROTECT_OPEN_PHONE_SECURITY',
  'PROTECT_OPEN_LEAK_DETECTION',
  'PROTECT_RUN_LEAK_SCAN',
  'PROTECT_SWITCH_LEAK_TAB',
  'PROTECT_OPEN_MONITORING_LIST',
  'PROTECT_OPEN_MONITORING_TASK_DETAIL',
  'PROTECT_ADD_MONITORING_EMAIL',
  'PROTECT_MONITORING_EMAIL_CHANGED',
  'PROTECT_SUBMIT_MONITORING_EMAIL',
  'PROTECT_OPEN_TRAIN',
  'FORGOT_PASSWORD_START',
  'FORGOT_PASSWORD_BACK_TO_LOGIN',
  'FORGOT_PASSWORD_EMAIL_CHANGED',
  'FORGOT_PASSWORD_SUBMIT_EMAIL',
  'FORGOT_PASSWORD_RESEND_EMAIL',
  'FORGOT_PASSWORD_OPEN_RESET',
  'FORGOT_PASSWORD_NEW_PASSWORD_CHANGED',
  'FORGOT_PASSWORD_CONFIRM_PASSWORD_CHANGED',
  'FORGOT_PASSWORD_SUBMIT_NEW_PASSWORD',
  'GET_STARTED_START',
  'GET_STARTED_TRY_LOGIN',
  'GET_STARTED_SIGN_IN',
  'GET_STARTED_CONTINUE',
  'GET_STARTED_BACK',
  'GET_STARTED_FORWARD',
  'GET_STARTED_SELECT_CHOICE',
  'GET_STARTED_EMAIL_CHANGED',
  'GET_STARTED_PASSWORD_CHANGED',
  'GET_STARTED_CREATE_ACCOUNT',
  'GET_STARTED_RUN_SCAN',
  'GET_STARTED_FINISH',
  'FAMILY_INVITE_MEMBER_CHANGED',
  'FAMILY_INVITE_SEND',
  'FAMILY_INVITE_DEFER',
  'FAMILY_INVITE_SUCCESS_CONTINUE',
  'PROTECT_FREE_RUN_SCAN',
  'PROTECT_FREE_SCAN_SETTLED',
  'PROTECT_FREE_COOLDOWN_TICK',
  'APP_FOREGROUNDED',
  'RETRY',
] as const satisfies readonly MobileAppEventType[];

// Bidirectional drift pin: every union member must be listed above. If a new
// MobileAppEvent type is added without appending it to `mobileAppEventTypes`,
// this assignment fails to typecheck.
type _MissingFromRegistry = Exclude<MobileAppEventType, (typeof mobileAppEventTypes)[number]>;
const _registryIsExhaustive: _MissingFromRegistry extends never ? true : _MissingFromRegistry = true;
void _registryIsExhaustive;

export type LoginAccountInput = {
  email: string;
  password: string;
  fSessionId?: string;
};

export type RegisterAccountOnboarding = {
  variant: 'get_started_free';
  riskStatus: ProductAppState['riskStatus'];
  scanResults?: ProductAppState['scanResults'];
};

export type RegisterAccountInput = LoginAccountInput & {
  onboarding?: RegisterAccountOnboarding;
};

export type PersistSessionInput = {
  session: ProductSession;
};

export type ClearSessionInput = {
  sessionToken?: string | null;
};

export type LoadAppStateInput = {
  sessionToken: string;
  /** True only for an authenticated foreground resume boundary. */
  refreshExtension?: boolean;
};

export type AdvanceFirstSessionInput = {
  sessionToken: string;
  event: ProductFirstSessionEvent;
};

export type OnboardingTimerInput = {
  phase: OnboardingTimerPhase;
  /** Content-beat timer run identity; omitted only for the separate DeepScan timer. */
  generation?: number;
};

export type CheckExtensionActivationInput = {
  trigger: 'onboarding' | 'protect';
  sessionToken?: string;
  /** Background proof refreshes must not turn the product preference back on. */
  intent?: 'background' | 'user' | 'turnOn';
};
export type CheckExtensionActivationOutput = {
  trigger: CheckExtensionActivationInput['trigger'];
  intent?: CheckExtensionActivationInput['intent'];
  proof: ExtensionActivationProof;
};

export type SetBrowserProtectionAppEnabledInput = {
  enabled: boolean;
  sessionToken: string;
};

export type SetBrowserProtectionAppEnabledOutput = {
  input: SetBrowserProtectionAppEnabledInput;
  appState: ProductAppState;
};

export type ResolveProtectTaskInput = ProductProtectTaskResolutionRequest & {
  sessionToken: string;
};

export type ResolveProtectTaskOutput = {
  input: ResolveProtectTaskInput;
  appState: ProductAppState;
};

export type AddMonitoringEmailInput = {
  email: string;
  sessionToken: string;
};

export type ProtectToastTimerInput = Record<string, never>;

/**
 * Countdown ticks mirror FreeScanTimerInput: the resend cooldown carries the
 * live remaining seconds so the label can tick 00:30 → 00:29 → … (G6/T6); a
 * bare phase made per-second updates structurally inexpressible.
 */
export type ForgotPasswordTimerInput =
  | { phase: Extract<ForgotPasswordTimerPhase, 'resendCooldown'>; secondsRemaining: number }
  | { phase: Extract<ForgotPasswordTimerPhase, 'successRedirect'> };

export type RequestPasswordResetInput = {
  email: string;
};

export type ResetPasswordInput = {
  token: string;
  password: string;
};

export type GetStartedTimerInput = {
  phase: GetStartedTimerPhase;
  /** Quiz location that armed selectionDwell (required for that phase). */
  fromLocation?: GetStartedRuntimeLocation;
  /** Matches getStarted.selectionDwellGeneration for stale-timer rejection. */
  generation?: number;
};

export type PersistGetStartedQuizAnswerInput = ProductFunnelQuizAnswerRequest;

export type RunScanInput = {
  subjectType: MobileScanSubjectType;
  subject: string;
};

export type RunFirstSessionScanInput = {
  sessionToken: string;
};

export type FirstSessionScanRunStatus = 'queued' | 'running' | 'reused' | 'waiting' | 'failed';

/**
 * Replay resolution. The plain app-state shape is the legacy contract (backend
 * without scan orchestration wired); the envelope carries the backend scan run
 * the machine polls via `firstSessionScanPoll`.
 */
export type RunFirstSessionScanOutput =
  | ProductAppState
  | {
      appState: ProductAppState;
      scanRun: { scanRunId: string; status: FirstSessionScanRunStatus } | null;
    };

export type FirstSessionScanPollInput = {
  sessionToken: string;
  scanRunId: string;
};

export type FirstSessionScanPollOutput = {
  scanRunId: string;
  state: 'requested' | 'queued' | 'running' | 'completed' | 'completed_partial' | 'failed' | 'expired';
  /**
   * App-state re-fetched after the scan turned terminal (the status poll
   * imports findings server-side), so the result screen branches on findings
   * from THIS scan. Null on non-terminal ticks and failed/expired terminals.
   */
  appState: ProductAppState | null;
};

export type SendFamilyInvitesInput = {
  sessionToken: string;
  emails: string[];
};

export type SendFamilyInvitesOutput = {
  invites: Array<{ email: string; status: 'pending' | 'active' | 'revoked' }>;
};

export type FreeScanTimerInput = {
  secondsRemaining: number;
  /**
   * Epoch for the armed cooldown chain. Superseded arms (e.g. runFreeScan then
   * loadAppState both start a countdown) must not re-arm in parallel — dual
   * freeScanTimer chains starve post-scan UI settle in virtual-time journeys.
   */
  generation?: number;
};

export type RunFreeScanInput = {
  sessionToken: string;
};

export type RequestAccountDeletionInput = {
  reason?: string;
  confirmation: 'DELETE';
  sessionToken: string;
};

export type RequestSubscriptionCancellationInput = {
  reason: import('@onyx/contracts').ProductCancellationReason;
  otherReason?: string;
  idempotencyKey?: string;
  sessionToken: string;
};
