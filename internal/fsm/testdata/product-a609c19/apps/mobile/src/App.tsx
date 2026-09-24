import { useEffect, useMemo, useRef, useState } from 'react';
import { useFonts } from 'expo-font';
import * as Linking from 'expo-linking';
import * as Network from 'expo-network';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import {
  AppState,
  Pressable,
  Platform,
  ScrollView,
  StatusBar,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { colors, fonts, spacing, typography } from './theme';
import { appFontAssets } from './appFontAssets';
import { ReturningAccountBridge } from './components/design-gaps/ReturningAccountBridge';
import { AppRecoveryScreen } from './components/design-gaps/AppRecoveryScreen';
import { figmaScreenTokens } from './components/figma/tokens/figmaScreenTokens';
import { ForgotPasswordReviewScreen } from './screens/forgot-password/ForgotPasswordReviewScreen';
import { ForgotPasswordBackgroundHost } from './screens/forgot-password/forgotPasswordBackgroundMorph';
import { resolveForgotPasswordRuntimeView, resolveForgotPasswordSlotIntent, type ForgotPasswordRuntimeIntent } from './screens/forgot-password/forgotPasswordRuntimeView';
import { forgotPasswordIntentToEvent } from './screens/forgot-password/forgotPasswordRuntimeEvents';
import { GetStartedScreen } from './screens/get-started/GetStartedScreen';
import { GetStartedFreeScreen } from './screens/get-started/GetStartedFreeScreen';
import { GetStartedFreeBackgroundHost } from './screens/get-started/getStartedFreeBackgroundMorph';
import { riskStatusFromOverrides } from './components/figma/risk/riskVisualStatus';
import {
  canonicalRiskStatusFromAppState,
} from './state/canonicalRisk';
import { productRiskToIndicatorStatus } from './state/riskStatusMapping';
import { resolveGetStartedScanResultRiskChannels } from './state/getStartedRiskRenderModel';
import {
  resolveGetStartedRuntimeView,
  resolveGetStartedSlotIntent,
  type GetStartedRuntimeIntent,
} from './screens/get-started/getStartedRuntimeView';
import { getStartedIntentToEvent } from './screens/get-started/getStartedRuntimeEvents';
import {
  resolveFamilyInviteRuntimeView,
  resolveFamilyInviteSlotIntent,
  resolveFamilyInviteSuccessRuntimeView,
} from './screens/family-invite/familyInviteRuntimeView';
import { familyInviteIntentToEvent } from './screens/family-invite/familyInviteRuntimeEvents';
import { resolveIosUpdateDaysBehind } from './screens/onboarding-first-session/iosUpdateDaysBehind';
import { FirstSessionScreenComposer } from './appSurfaces/firstSessionAppSurface';
import { OnboardingBackgroundHost } from './screens/onboarding-first-session/onboardingBackgroundMorph';
import { resolveFirstSessionScanOutcomeFor, resolveOnboardingPostflowRecap, resolveOnboardingProgress, resolveOnboardingProjectedStateId, resolveOnboardingRuntimeView } from './screens/onboarding-first-session/onboardingRuntimeView';
import { scanOutcomeMonitorableLeakIds } from './state/firstSessionScanOutcome';
import { firstSessionScanPaintsFunnelFirstCheck, resolveTrainEmailProvider } from '@onyx/contracts';
import { noLeaksPostflowDeepScanRows } from './screens/figma-projected/postflowRecapProjection';
import { resetProtectAppSurfaceSession, useProtectAppSurface } from './appSurfaces/protectAppSurface';
import { useVisualCaptureAppSurface } from './appSurfaces/visualCaptureAppSurface';
import { protectTaskSheetUnderlayLocation } from './screens/protect/live/ProtectSurface';
import { MainAppShell } from './shell/MainAppShell';
import { resolveShellRouteChrome } from './shell/shellRouteChrome';
import { SurfaceFrame } from './shell/layout/SurfaceFrame';
import { resolveActiveShellTab, resolveShellTabs, resolveSurfaceKey } from './shell/shellSurface';
import { AppStartupLoader as SharedAppStartupLoader } from './screens/loader/AppStartupLoader';
import { ProfileScreen } from './screens/ProfileScreen';
import type { ProductAppState } from './api';
import { TrainRuntimeScreen } from './screens/train/TrainRuntimeScreen';
import { trainRuntimeSignalToEvent } from './screens/train/trainRuntimeEvents';
import { TrainScreenTransitionFrame } from './screens/train/TrainScreenTransitionFrame';
import { DeveloperDiagnosticsScreen } from './screens/DeveloperDiagnosticsScreen';
import { CreateAccountScreen } from './screens/CreateAccountScreen';
import { FigmaSignInViewport } from './screens/FigmaSignInScreen';
import { getInitialVisualScenario, getInitialVisualTab, isVisualModeEnabled } from './fixtures/visualRuntimeMode';
import { createVisualStatePort } from './routing/visualCaptureRoute';
import type { TabKey } from './screens/types';
import type { MobileAppInput } from './state/mobileAppMachine.types';
import { mobileAppLiveServices } from './effects/mobileAppLiveServices';
import type { MobileAppServices } from './effects/mobileAppServices';
import { createMobileAppRuntime } from './effects/mobileAppRuntime';
import { openSupportTicket } from './effects/openSupportTicket';
import type { MachineTransitionRecord } from './analytics/machineEventTracking';
import { getCurrentOpenId } from './analytics/openCorrelation';
import { TrackingDiagnosticReceiptHost } from './analytics/TrackingDiagnosticReceiptHost';
import { isTrackingDiagnosticReceiptEnabled } from './analytics/trackingDiagnosticReceipt';
import { getMobileAnalyticsTracker, trackMachineTransition } from './analytics/tracker';
import { withMachineEventSource } from './state/machineEventSource';
import { projectedInteractionTestID } from './state/projectedEventSource';
import { readFunnelSessionIdFromUrl } from './deeplinks/funnelSessionLink';
import {
  createColdLiveDeepLinkCoordinator,
  initialProviderSettlementUrl,
  type ColdLiveDeepLinkCoordinator,
  type ColdLiveDeepLinkDelivery,
} from './deeplinks/coldLiveDeepLinkCoordinator';
import {
  createMountedDeepLinkDeliveryDeduper,
  type MountedDeepLinkDeliveryDeduper,
} from './deeplinks/mountedDeepLinkDeliveryDeduper';
import { readPasswordResetTokenFromUrl } from './deeplinks/passwordResetLink';
import {
  classifyAppsFlyerAutologinUrl,
  createAppsFlyerAutologinBridge,
  isAppsFlyerAutologinEnabled,
  type AppsFlyerSdkBridge,
} from './appsflyerAutologin';
import {
  selectAppState,
  selectAppStateMessage,
  selectAuthEmail,
  selectAuthError,
  selectAuthMessage,
  selectAuthPassword,
  selectAuthRoute,
  selectCanAccessAppShell,
  selectFirstSessionStep,
  selectIsAppStateLoading,
  selectIsAuthenticating,
  selectIsBooting,
  selectIsFirstSession,
  selectSelectedTab,
  selectTrainLocation,
  selectProfileLocation,
  selectPendingReturnTo,
  selectSession,
  selectSnapshot,
  useMobileAppSelector,
} from './state/mobileAppSelectors';
import { FigmaScreenScaler } from './screens/figma-projected/FigmaScreenScaler';
import {
  deriveDeepScanRowsFromAppState,
  deriveOnboardingLeakPreviewsFromAppState,
  deriveOnboardingMonitorLeaksFromAppState,
  deriveOnboardingTaskPreviewFromAppState,
  deriveOnboardingTaskPreviewsFromAppState,
} from './screens/figma-projected/onboardingLeakData';
import { deepScanBrowserProtectionRows } from './screens/figma-projected/deepScanBrowserProtectionRows';
import { deepScanLeakSummaryForCard } from './screens/onboarding-first-session/deepScanLiveSummary';
import { onboardingDeepScanDurationMs, onboardingTimerDurationMs } from './effects/onboardingTimerDurations';
import type { ProjectedInteraction } from './screens/figma-projected/projectedInteraction';
import { UiClockProvider } from './runtime/uiClockContext';
import type { UiClockInput } from './runtime/uiClock';
import type { MobileAppRuntimeEventObserver } from './effects/mobileAppRuntime';
import { PricingContentProvider, type PricingContentMap } from './pricing/PricingContentContext';
import { buildExpiredTrainView } from './screens/profile/subscriptionFlowsView';
import { TrainExpiredScreen } from './screens/train/TrainExpiredScreen';
import { useBrowserRouteCoordinator } from './routing/useBrowserRouteCoordinator';
import { formatCanonicalUrl, parseCanonicalRoute } from './routing/canonicalRouteCodec';
import { findCanonicalRouteDescriptor, type CanonicalLocation } from './state/appLocations';
import { isSubscriptionBlocked } from './state/authorization';
import { NativeNavigationGestureHost, useNativeNavigationGesture } from './routing/NativeNavigationGestureHost';
import { navigationGesturePolicyFor } from './routing/nativeNavigationGesturePolicy';
import { projectCanonicalLocation } from './routing/locationProjection';
import { createNativeScenarioLifecycleAcknowledgement, createNativeScenarioLifecycleEventHandler, nativeScenarioLifecycleWitnessTestId, type NativeScenarioLifecycleRequest } from './runtime/nativeScenarioLifecycle';
import { clearNativeSceneInitialURL, getNativeSceneInitialURL, useNativeSceneURL } from './runtime/nativeSceneURL';
import { parseNativeScenarioLifecycleUrl } from './runtime/nativeScenarioLifecycle';
import type { MobileAppSnapshot } from './behavior/mobileAppInterpreter';
import type { MachineEventSource, MobileAppEvent } from './state/mobileAppMachine.types';

/**
 * Shared empty identity for the per-finding reveal records, so "nothing is
 * revealed" is one stable object rather than a new one per render.
 */
const EMPTY_REVEAL_RECORD: Readonly<Record<string, string>> = Object.freeze({});

/** Drop one key without mutating (and without allocating when it is absent). */
function withoutRevealKey(record: Readonly<Record<string, string>>, key: string): Readonly<Record<string, string>> {
  if (record[key] === undefined) return record;
  const next = { ...record };
  delete next[key];
  return Object.keys(next).length === 0 ? EMPTY_REVEAL_RECORD : next;
}


export type AppProps = {
  services?: MobileAppServices;
  uiClock?: UiClockInput;
  presentationClock?: UiClockInput;
  runtimeEventObserver?: MobileAppRuntimeEventObserver;
  onMachineTransition?: (record: MachineTransitionRecord) => void;
  /**
   * Optional runtime input override for scenario harness / focused mounts.
   * Profile `initialVisualTab` (e.g. train) is applied here so journeys do not
   * depend on a shell tab that projected Train surfaces may hide.
   */
  input?: MobileAppInput;
};

export default function App({
  services = mobileAppLiveServices,
  uiClock,
  presentationClock,
  runtimeEventObserver,
  onMachineTransition,
  input,
}: AppProps = {}) {
  const visualMode = input?.visualMode ?? isVisualModeEnabled();
  const [nativeLifecycle, setNativeLifecycle] = useState<NativeScenarioLifecycleRequest | null>(null);
  const nativeLifecycleUrl = Linking.useLinkingURL();
  const nativeSceneLifecycleUrl = useNativeSceneURL();
  const acknowledgeNativeLifecycle = useMemo(
    () => createNativeScenarioLifecycleAcknowledgement(
      setNativeLifecycle,
      () => {
        Linking.clearInitialURL();
        clearNativeSceneInitialURL();
      },
    ),
    [],
  );

  useEffect(() => {
    if (visualMode || Platform.OS === 'web' || typeof __DEV__ === 'undefined' || !__DEV__ || process.env.EXPO_PUBLIC_ONYX_REVIEWER_RUNTIME_SCENARIO !== '1') return undefined;

    acknowledgeNativeLifecycle(nativeLifecycleUrl);
    acknowledgeNativeLifecycle(nativeSceneLifecycleUrl);
    return undefined;
  }, [acknowledgeNativeLifecycle, nativeLifecycleUrl, nativeSceneLifecycleUrl, visualMode]);

  const lifecycleKey = nativeLifecycle === null
    ? 'native-lifecycle-initial'
    : `${nativeLifecycle.action}:${nativeLifecycle.token}`;
  const lifecycleWitness = nativeLifecycle === null ? null : (
    <View
      accessible
      accessibilityLabel={nativeScenarioLifecycleWitnessTestId(nativeLifecycle.action, nativeLifecycle.token)}
      pointerEvents="none"
      style={styles.nativeLifecycleWitness}
      testID={nativeScenarioLifecycleWitnessTestId(nativeLifecycle.action, nativeLifecycle.token)}
    />
  );

  // SafeAreaProvider only *measures* the real device insets (used by the
  // projected header safe-area clamp — feedback: top back/skip controls under
  // the Dynamic Island). The shell stays full-bleed: nothing here insets the
  // Figma frames.
  return (
    <UiClockProvider key={lifecycleKey} clock={uiClock} presentationClock={presentationClock}>
      <SafeAreaProvider>
        <NativeNavigationGestureHost>
          <OnyxApp
            acknowledgeNativeLifecycle={acknowledgeNativeLifecycle}
            services={services}
            runtimeEventObserver={runtimeEventObserver}
            onMachineTransition={onMachineTransition}
            input={input}
          />
          {lifecycleWitness}
        </NativeNavigationGestureHost>
      </SafeAreaProvider>
    </UiClockProvider>
  );
}

function OnyxApp({
  acknowledgeNativeLifecycle,
  services,
  runtimeEventObserver,
  onMachineTransition: extraMachineTransition,
  input: inputOverride,
}: {
  acknowledgeNativeLifecycle: (url: string) => void;
  services: MobileAppServices;
  runtimeEventObserver?: MobileAppRuntimeEventObserver;
  onMachineTransition?: (record: MachineTransitionRecord) => void;
  input?: MobileAppInput;
}) {
  const visualMode = inputOverride?.visualMode ?? isVisualModeEnabled();
  const [fontsLoaded] = useFonts(appFontAssets);
  const mobileAppActor = useMemo(() => createMobileAppRuntime({
    input: {
      visualMode,
      visual: visualMode
        ? (inputOverride?.visual ?? createVisualStatePort())
        : inputOverride?.visual,
      initialVisualScenario: inputOverride?.initialVisualScenario ?? getInitialVisualScenario(),
      initialVisualTab: inputOverride?.initialVisualTab ?? getInitialVisualTab(),
      // Pre-auth funnel entry seed. Only scenario/harness mounts supply it; a
      // production boot leaves it undefined and keeps the `welcome` entry.
      ...(inputOverride?.initialGetStarted ? { initialGetStarted: inputOverride.initialGetStarted } : {}),
    },
    services,
    eventObserver: runtimeEventObserver,
    onMachineTransition: (record) => {
      trackMachineTransition(record);
      extraMachineTransition?.(record);
    },
  }), [extraMachineTransition, inputOverride, runtimeEventObserver, services, visualMode]);
  const send = mobileAppActor.send;
  const sendUi = (event: MobileAppEvent, source?: MachineEventSource | string) => {
    const testID = typeof source === 'string' ? source : source?.testID;
    send(withMachineEventSource(event, testID));
  };
  const canRunAppsFlyerAutologin = !visualMode
    && Platform.OS === 'ios'
    && Boolean(services.exchangeAppsFlyerAutologin)
    && isAppsFlyerAutologinEnabled(process.env.EXPO_PUBLIC_APPSFLYER_AUTOLOGIN_KILL_SWITCH);
  // StrictMode can re-run the startup effect for the same actor; stage the
  // harness token at most once per actor identity (new actors may stage again).
  const harnessAutologinActorRef = useRef<typeof mobileAppActor | null>(null);

  useEffect(() => {
    // T-328: every APP PROCESS start wipes the Phone-security walk session —
    // a new build/cold start must never replay the previous session's ticks.
    resetProtectAppSurfaceSession();
    mobileAppActor.start();
    // Scenario-harness-only seam: stage an AppsFlyer autologin token into the
    // real machine during boot. Production never sets this input; the iOS SDK
    // bridge remains the live token source. Do not Journey-inject the event.
    const harnessToken = inputOverride?.scenarioAppsFlyerAutologinToken?.trim();
    if (!visualMode && harnessToken && harnessAutologinActorRef.current !== mobileAppActor) {
      harnessAutologinActorRef.current = mobileAppActor;
      mobileAppActor.send({ type: 'APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED', token: harnessToken });
    }
  }, [inputOverride?.scenarioAppsFlyerAutologinToken, mobileAppActor, visualMode]);

  useEffect(() => {
    if (!canRunAppsFlyerAutologin) return undefined;

    let sdk: AppsFlyerSdkBridge | null = null;
    try {
      // Keep the native-only module out of web and visual runtimes. Missing
      // native configuration is intentionally a no-op so manual login stays
      // the complete fallback path.
      const appsFlyerModule = require('react-native-appsflyer') as {
        AppsFlyer?: AppsFlyerSdkBridge;
        default?: AppsFlyerSdkBridge;
      };
      sdk = appsFlyerModule.AppsFlyer ?? appsFlyerModule.default ?? null;
    } catch {
      return undefined;
    }

    if (!sdk) return undefined;
    return createAppsFlyerAutologinBridge({
      sdk,
      devKey: process.env.EXPO_PUBLIC_APPSFLYER_DEV_KEY,
      onToken: (token) => {
        mobileAppActor.send({ type: 'APPSFLYER_AUTOLOGIN_TOKEN_RECEIVED', token });
      },
      onFunnelSessionId: (fSessionId) => {
        getMobileAnalyticsTracker().setFSessionId(fSessionId);
        mobileAppActor.send({ type: 'FUNNEL_SESSION_RECEIVED', fSessionId });
      },
      onProgress: (progress) => {
        getMobileAnalyticsTracker().track('web_to_app_autologin_progress', progress);
      },
    });
  }, [canRunAppsFlyerAutologin, mobileAppActor]);

  useEffect(() => {
    if (!canRunAppsFlyerAutologin) return undefined;

    const networkSubscription = Network.addNetworkStateListener(({ isInternetReachable }) => {
      // This effect is iOS-only; expo-network defines iOS reachability as the
      // same platform-owned signal as connectivity.
      if (isInternetReachable !== true) return;
      if (mobileAppActor.getSnapshot().context.pendingAppsFlyerAutologinRetry?.retryAttempt !== 0) return;
      mobileAppActor.send({ type: 'APPSFLYER_AUTOLOGIN_NETWORK_AVAILABLE' });
    });

    return () => networkSubscription.remove();
  }, [canRunAppsFlyerAutologin, mobileAppActor]);
  const selectedTab = useMobileAppSelector(mobileAppActor, selectSelectedTab);
  const booting = useMobileAppSelector(mobileAppActor, selectIsBooting);
  const appState = useMobileAppSelector(mobileAppActor, selectAppState);
  const hasLoadedAppState = appState != null;
  const appStateMessage = useMobileAppSelector(mobileAppActor, selectAppStateMessage);
  const authMessage = useMobileAppSelector(mobileAppActor, selectAuthMessage);
  const authError = useMobileAppSelector(mobileAppActor, selectAuthError);
  const appStateLoading = useMobileAppSelector(mobileAppActor, selectIsAppStateLoading);
  const productSession = useMobileAppSelector(mobileAppActor, selectSession);
  const [pricingContent, setPricingContent] = useState<PricingContentMap>({});
  const canShowAppShell = useMobileAppSelector(mobileAppActor, selectCanAccessAppShell);
  const authEmail = useMobileAppSelector(mobileAppActor, selectAuthEmail);
  const authPassword = useMobileAppSelector(mobileAppActor, selectAuthPassword);
  const authRoute = useMobileAppSelector(mobileAppActor, selectAuthRoute);
  const trainView = useMobileAppSelector(mobileAppActor, selectTrainLocation);
  const profileLocation = useMobileAppSelector(mobileAppActor, selectProfileLocation);
  const pendingReturnTo = useMobileAppSelector(mobileAppActor, selectPendingReturnTo);
  const productApiBusy = useMobileAppSelector(mobileAppActor, selectIsAuthenticating);
  const isFirstSession = useMobileAppSelector(mobileAppActor, selectIsFirstSession);
  const firstSessionStep = useMobileAppSelector(mobileAppActor, selectFirstSessionStep);
  const snapshot = useMobileAppSelector(mobileAppActor, selectSnapshot);

  const gestureRouteId = navigationGestureRouteId(snapshot);
  const gesturePolicy = navigationGesturePolicyFor({
    routeId: gestureRouteId,
    hasBackEntry: navigationGestureHasBackEntry(snapshot),
    hasForwardEntry: snapshot.matches('getStarted') && snapshot.context.getStartedForwardHistory.length > 0,
  });
  const gestureBackEvent = navigationGestureBackEvent(snapshot);
  const gestureForwardEvent: MobileAppEvent | null = snapshot.matches('getStarted')
    && snapshot.context.getStartedForwardHistory.length > 0
    ? { type: 'GET_STARTED_FORWARD' }
    : null;
  useNativeNavigationGesture({
    routeId: gestureRouteId,
    back: gesturePolicy.back,
    forward: gesturePolicy.forward,
    // Profile owns local draft/modal transitions; its child registration is
    // the active one. Visual/story captures remain deterministic.
    enabled: !visualMode
      && !(snapshot.context.selectedTab === 'profile' && profileLocation.screen !== 'diagnostics')
      && snapshot.context.selectedTab !== 'train'
      && Boolean(gestureBackEvent || gestureForwardEvent),
    onBack: gestureBackEvent ? () => send(gestureBackEvent) : undefined,
    onForward: gestureForwardEvent ? () => send(gestureForwardEvent) : undefined,
  });

  useEffect(() => {
    const sessionToken = productSession?.sessionToken;
    if (visualMode || !sessionToken || !services.loadPricingContent) {
      setPricingContent({});
      return;
    }

    let mounted = true;
    void services.loadPricingContent({ sessionToken })
      .then((content) => {
        if (mounted) setPricingContent(content);
      })
      .catch(() => {
        // Pricing is additive profile content. Keep authenticated subscription
        // facts visible if the optional content endpoint is unavailable.
        if (mounted) setPricingContent({});
      });

    return () => {
      mounted = false;
    };
  }, [productSession?.sessionToken, services, visualMode]);
  const protectAppSurface = useProtectAppSurface({
    appState,
    appStateLoading,
    snapshot,
    send,
    sendUi,
    services,
    productSession,
    visualMode,
  });
  const visualCapture = useVisualCaptureAppSurface({
    visualMode,
    send,
    protectAppSurface,
  });
  const diagnosticsOpen = visualMode ? visualCapture.visualDiagnosticsOpen : profileLocation.screen === 'diagnostics';
  const visualAuthRoute = visualCapture.visualAuthRoute;
  /**
   * Per-visit reveal gates for the reset-password pair (T-217 class fix). These
   * are VIEW state, not machine state: "the user has left this field" is about
   * the current visit to the form, so re-entering change-password starts silent
   * again. Kept out of the state machine for the same reason create-account's
   * `submitAttempted` is — it must never persist or replay.
   */
  const [resetPasswordBlurred, setResetPasswordBlurred] = useState({ newPassword: false, confirmPassword: false });
  /** Same per-visit exit gates for the two InviteFamily member seats. */
  const [memberEmailExited, setMemberEmailExited] = useState<[boolean, boolean]>([false, false]);
  // Re-tap counter for the spot-fake-reveal stamp attention pulse (AF): bumping it
  // replays the "it's a Fake" stamp animation when the user re-taps a choice.
  const [stampAttention, setStampAttention] = useState(0);

  /**
   * Reveal state is PER FINDING, keyed by findingId — never one value per
   * screen (T-238). A multi-leak screen renders one card per open critical
   * credential finding, and the owner could not open the password on card 2 or
   * card 3 because the single `{ findingId, value }` slot only ever described
   * `leakPreviews[0]`. Records rather than a single slot also mean a revealed
   * card can never paint its plaintext onto a sibling card.
   */
  const [revealedOnboardingLeakPreviews, setRevealedOnboardingLeakPreviews] = useState<Readonly<Record<string, string>>>(EMPTY_REVEAL_RECORD);
  const [onboardingRevealErrors, setOnboardingRevealErrors] = useState<Readonly<Record<string, string>>>(EMPTY_REVEAL_RECORD);
  /**
   * Reveal safety: feedback capture is blocked while ANY card is revealed, so
   * this is DERIVED from the revealed record rather than tracked separately —
   * per-card reveal cannot desynchronise a gate it is computed from.
   */
  const onboardingRevealActive = Object.keys(revealedOnboardingLeakPreviews).length > 0;
  // Mounted disclosure boundary shared by Protect and onboarding reveals.
  // Journey screenshot capture reads this marker from the live DOM rather than
  // inferring sensitivity from step names or journey definitions.
  const sensitiveRevealActive = protectAppSurface.protectRevealActive || onboardingRevealActive;
  // Supersession is two-level: the epoch is bumped by every onboarding state
  // change (all in-flight reveals are abandoned with the screen), and each
  // finding carries its own generation bumped by its own tap/hide — so
  // revealing card 2 never drops card 1's in-flight result.
  const onboardingRevealEpoch = useRef(0);
  const onboardingRevealGenerations = useRef(new Map<string, number>());
  // Lifecycle-owned deep-link delivery gate (not analytics open-id owned).
  // Lazy: avoid allocating a Set-backed deduper on every render.
  const deepLinkDeliveryDeduperRef = useRef<MountedDeepLinkDeliveryDeduper | null>(null);
  const coldLiveDeepLinkCoordinatorRef = useRef<ColdLiveDeepLinkCoordinator | null>(null);
  const applyColdLiveDeepLinkDeliveriesRef = useRef<
    (deliveries: readonly ColdLiveDeepLinkDelivery[]) => void
  >(() => {});
  const showingAuthScreen =
    snapshot.matches('unauthenticated') ||
    snapshot.matches({ authenticating: 'login' }) ||
    snapshot.matches({ authenticating: 'register' }) ||
    visualAuthRoute !== null;
  const showingFirstSession = !showingAuthScreen && (isFirstSession || (visualMode && firstSessionStep !== 'complete'));
  const showingAccountStartup =
    snapshot.matches({ authenticating: 'persist' }) ||
    (canShowAppShell && !hasLoadedAppState);

  const onboardingStateId = showingFirstSession
    ? snapshot.matches({ firstSession: 'inviteFamily' })
      ? 'invite-family'
      : snapshot.matches({ firstSession: 'inviteFamilySuccess' })
        ? 'invite-family-success'
        : snapshot.matches({ firstSession: 'riskUnknownNewMember' })
          ? 'risk-unknown-new-member'
        : resolveOnboardingRuntimeView(snapshot)?.stateId ?? null
    : null;

  // Leaving the reset form ends the visit: the next arrival must open silent
  // rather than replay the previous visit's revealed errors.
  const onResetPasswordForm = snapshot.context.forgotPassword.location === 'changePassword'
    || snapshot.context.forgotPassword.location === 'resettingPassword';
  useEffect(() => {
    if (onResetPasswordForm) return;
    setResetPasswordBlurred({ newPassword: false, confirmPassword: false });
  }, [onResetPasswordForm]);

  useEffect(() => {
    // A plaintext value is scoped to one first-session screen. Even when the
    // next chapter refers to the same finding, navigating is a new disclosure
    // boundary and requires a fresh authenticated reveal.
    setRevealedOnboardingLeakPreviews(EMPTY_REVEAL_RECORD);
  }, [onboardingStateId, showingFirstSession]);

  // A reveal error belongs to one tap on one screen. Clear it on every
  // onboarding state change so a retry prompt cannot appear on a screen where
  // nothing was tried (T-211/L2), and so a late response cannot resurrect a
  // previous screen's disclosure state.
  useEffect(() => {
    // Supersede every reveal still in flight so a late result cannot restore
    // the error (or plaintext) the screen change just cleared.
    onboardingRevealEpoch.current += 1;
    onboardingRevealGenerations.current = new Map();
    setOnboardingRevealErrors(EMPTY_REVEAL_RECORD);
  }, [onboardingStateId, showingFirstSession]);

  const screenProps = useMemo(() => ({
    appState,
    appStateLoading,
    appStateMessage,
    onRefreshAppState: refreshProductState,
    onNavigate: handleSelectTab,
  }), [appState, appStateLoading, appStateMessage]);

  useEffect(() => {
    if (visualMode) return;

    let initialAppsFlyerLinkTracked = false;
    let disposed = false;
    const deepLinkDeliveryDeduper = (
      deepLinkDeliveryDeduperRef.current ??= createMountedDeepLinkDeliveryDeduper()
    );
    // Buffer live observeDeepLinks until BOTH async initial providers settle so
    // cold ownership stays authoritative (no timer). Exact-URL Set still dedupes.
    // AppState first inactive/background force-closes hung collection.
    const coldLiveCoordinator = (
      coldLiveDeepLinkCoordinatorRef.current ??= createColdLiveDeepLinkCoordinator()
    );

    function applyProductionDeepLink(
      url: string | null,
      isColdStart: boolean,
      openId?: string,
    ) {
      // Exact URL Set for the current lifecycle window: duplicate native
      // deliveries must not re-stamp funnel join or re-emit link_opened.
      // Boundary reset is AppState-owned (not analytics open-id), so autologin
      // stays correct when tracking is absent/misconfigured.
      if (url) {
        if (!deepLinkDeliveryDeduper.accept(url)) return;
      }

      const classified = url ? classifyAppsFlyerAutologinUrl(url) : null;
      if (classified && (!isColdStart || !initialAppsFlyerLinkTracked)) {
        if (classified.f_session_id) {
          getMobileAnalyticsTracker().setFSessionId(classified.f_session_id);
          send({ type: 'FUNNEL_SESSION_RECEIVED', fSessionId: classified.f_session_id });
        }
        getMobileAnalyticsTracker().track('web_to_app_autologin_progress', {
          stage: 'link_opened',
          is_cold_start: isColdStart,
          flow_matched: classified.flow_matched,
          // Capture-time open_id (after offerAppsFlyerLinkingUrl) so deferred
          // flush stays correlated with open_classified on the same open.
          ...(typeof openId === 'string' && openId.trim().length > 0
            ? { open_id: openId }
            : {}),
        });
        if (isColdStart) initialAppsFlyerLinkTracked = true;
      }

      const resetToken = readPasswordResetTokenFromUrl(url);
      if (resetToken) {
        send({ type: 'FORGOT_PASSWORD_OPEN_RESET', token: resetToken });
        return;
      }

      const fSessionId = readFunnelSessionIdFromUrl(url);
      if (fSessionId) {
        getMobileAnalyticsTracker().setFSessionId(fSessionId);
        send({ type: 'FUNNEL_SESSION_RECEIVED', fSessionId });
      }
    }

    function applyColdLiveDeliveries(
      deliveries: readonly ColdLiveDeepLinkDelivery[],
    ) {
      for (const delivery of deliveries) {
        applyProductionDeepLink(
          delivery.url,
          delivery.isColdStart,
          delivery.openId,
        );
      }
    }
    applyColdLiveDeepLinkDeliveriesRef.current = applyColdLiveDeliveries;

    if (Platform.OS !== 'web') {
      const handleNativeLiveDeepLink = createNativeScenarioLifecycleEventHandler(
        acknowledgeNativeLifecycle,
        (url) => {
          // Service already ran offerAppsFlyerLinkingUrl before this listener.
          // Capture open ownership now so deferred flush keeps stage agreement.
          applyColdLiveDeliveries(coldLiveCoordinator.onLiveUrl(url, {
            openId: getCurrentOpenId(),
          }));
        },
      );

      // Install live promptly; URLs buffer until both initials settle.
      const unsubscribe = services.observeDeepLinks(handleNativeLiveDeepLink);

      const settleInitial = (
        slot: 'service' | 'nativeScene',
        url: string | null,
      ) => {
        if (disposed) return;
        // Scenario lifecycle URLs: acknowledge (and clear via acknowledgement)
        // but settle null so they never enter production deep-link parsing.
        if (url) acknowledgeNativeLifecycle(url);
        // Service/native resolver already classified before this promise resolves;
        // capture open_id now so deferred cold flush does not inherit warm id.
        applyColdLiveDeliveries(
          coldLiveCoordinator.onInitialProviderSettled(
            slot,
            initialProviderSettlementUrl(url),
            { openId: getCurrentOpenId() },
          ),
        );
      };

      void services.getInitialDeepLink().then(
        (url) => settleInitial('service', url),
        () => settleInitial('service', null),
      );
      void getNativeSceneInitialURL().then(
        (url) => {
          if (disposed) return;
          // Ordinary product URLs only — scenario URLs clear via acknowledgement.
          if (url && parseNativeScenarioLifecycleUrl(url) === undefined) {
            clearNativeSceneInitialURL();
          }
          settleInitial('nativeScene', url);
        },
        () => settleInitial('nativeScene', null),
      );

      return () => {
        disposed = true;
        coldLiveCoordinator.dispose();
        if (coldLiveDeepLinkCoordinatorRef.current === coldLiveCoordinator) {
          coldLiveDeepLinkCoordinatorRef.current = null;
        }
        applyColdLiveDeepLinkDeliveriesRef.current = () => {};
        unsubscribe();
      };
    }

    return services.observeDeepLinks((url) => {
      if (Platform.OS !== 'web') applyProductionDeepLink(url, false);
    });
  }, [acknowledgeNativeLifecycle, send, services, visualMode]);

  // The coordinator is extracted only as an internal hook so its browser
  // transaction semantics are tested through the exact App-owned mechanism.
  useBrowserRouteCoordinator({
    snapshot,
    send,
    visualMode,
    booting,
    appStateLoading,
    hasLoadedAppState,
    productSession,
    pendingReturnTo,
  });

  // Foreground auto-detect (feedback: activating the Safari extension in
  // Settings should be picked up automatically). Whenever the app returns to
  // the foreground we notify the machine; the interpreter re-runs the
  // extension activation proof check while the browser onboarding chapter is
  // active and ignores the event everywhere else.
  //
  // Session lifecycle for product analytics: tracker.onAppStateChange flushes
  // on background and touches the session on active. On web, react-native-web's
  // AppState already maps document visibilitychange → 'active'/'background',
  // so we intentionally keep a single subscription (no forked web path).
  useEffect(() => {
    if (visualMode) return;

    return services.observeAppLifecycle((state) => {
      // Exact ownership order under live services (notifyAppsFlyerLifecycle already
      // ran): tracker lifecycle first so warm app_opened is minted on active before
      // deferred link_opened flush; Set clear; then coordinator emit.
      void getMobileAnalyticsTracker().onAppStateChange(state);
      (deepLinkDeliveryDeduperRef.current ??= createMountedDeepLinkDeliveryDeduper())
        .onLifecycleState(state);
      const coordinator = coldLiveDeepLinkCoordinatorRef.current;
      if (coordinator) {
        applyColdLiveDeepLinkDeliveriesRef.current(coordinator.onLifecycleState(state));
      }
      if (state !== 'active') {
        // Plaintext is memory-only and must not survive a native background or
        // inactive transition. Supersede any late response before clearing the
        // per-finding records and feedback gate.
        onboardingRevealEpoch.current += 1;
        onboardingRevealGenerations.current = new Map();
        setRevealedOnboardingLeakPreviews(EMPTY_REVEAL_RECORD);
        setOnboardingRevealErrors(EMPTY_REVEAL_RECORD);
        protectAppSurface.clearProtectRevealActive();
      }
      if (state === 'active') {
        send({ type: 'APP_FOREGROUNDED' });
      }
    });
  }, [protectAppSurface.clearProtectRevealActive, send, services, visualMode]);

  // Account deletion handoff (profile-flow.md §5.4): deletion is permanent +
  // immediate — after the delete request the app logs out and lands on the
  // Welcome funnel entry, NOT Login (the account no longer exists). A plain
  // logout must keep landing on Login (the entry gate above is one-shot), so
  // deletion arms its own re-entry that fires once the session is cleared.
  const welcomeAfterDeletionRef = useRef(false);
  useEffect(() => {
    if (visualMode || !welcomeAfterDeletionRef.current) return;
    if (!snapshot.matches('unauthenticated')) return;
    welcomeAfterDeletionRef.current = false;
    send({ type: 'GET_STARTED_START' });
  }, [send, snapshot, visualMode]);

  function handleAccountDeleted() {
    getMobileAnalyticsTracker().reset();
    welcomeAfterDeletionRef.current = true;
    logout();
  }

  function handleSelectTab(tab: TabKey) {
    // Every tab's canonical drill-in is now machine-owned. `NAVIGATE` preserves
    // the existing native tab semantics, including active-tab -> root resets.
    send({ type: 'NAVIGATE', tab });
  }

  function requestCanonicalRoute(location: CanonicalLocation) {
    send({ type: 'ROUTE_REQUESTED', location, source: 'user' });
  }

  function handleTrainRuntimeNavigate(path: string) {
    const parsed = parseCanonicalRoute(path);
    if (!parsed || parsed.location.area !== 'train') return;
    requestCanonicalRoute(parsed.location);
  }

  function refreshProductState() {
    send({ type: 'REFRESH_APP_STATE' });
  }

  function loginProductAccount() {
    send({ type: 'SUBMIT_LOGIN' });
  }

  function registerProductAccount() {
    send({ type: 'SUBMIT_REGISTER' });
  }

  function logout() {
    send({ type: 'LOGOUT' });
  }

  /** Bump (and return) the supersession generation of ONE finding's reveal. */
  function nextOnboardingRevealGeneration(findingId: string) {
    const generation = (onboardingRevealGenerations.current.get(findingId) ?? 0) + 1;
    onboardingRevealGenerations.current.set(findingId, generation);
    return generation;
  }

  async function revealOnboardingLeakPreview(findingId: string) {
    if (!productSession) return;
    // A reveal in flight belongs to the tap that started it. Without this
    // guard a response landing after the user moved on repopulates state the
    // screen change just cleared: a late failure re-lights "Couldn't reveal -
    // try again." on password-fix (T-211/L2), and a late success writes
    // plaintext back after the card is gone.
    //
    // The generation is per finding and the epoch is per screen (T-238), so a
    // tap on card 2 supersedes only card 2's own in-flight reveal while a
    // screen change still supersedes all of them.
    const epoch = onboardingRevealEpoch.current;
    const generation = nextOnboardingRevealGeneration(findingId);
    const superseded = () => epoch !== onboardingRevealEpoch.current
      || onboardingRevealGenerations.current.get(findingId) !== generation;
    setOnboardingRevealErrors((current) => withoutRevealKey(current, findingId));
    try {
      const response = await services.revealProtectFinding({
        findingId,
        sessionToken: productSession.sessionToken,
      });
      const field = response.revealedFields.find((item) => item.fieldType === 'password' && item.plaintext.trim().length > 0)
        ?? response.revealedFields.find((item) => item.plaintext.trim().length > 0)
        ?? response.revealedFields[0];
      if (!field) throw new Error('Reveal returned no fields.');
      if (superseded()) return;
      setRevealedOnboardingLeakPreviews((current) => ({ ...current, [findingId]: field.plaintext }));
    } catch {
      if (superseded()) return;
      setRevealedOnboardingLeakPreviews((current) => withoutRevealKey(current, findingId));
      setOnboardingRevealErrors((current) => ({ ...current, [findingId]: "Couldn't reveal - try again." }));
    }
  }

  async function toggleOnboardingLeakPreview(findingId: string): Promise<void> {
    if (revealedOnboardingLeakPreviews[findingId] !== undefined) {
      // Hiding also supersedes this finding's reveal if one is still in flight.
      nextOnboardingRevealGeneration(findingId);
      setRevealedOnboardingLeakPreviews((current) => withoutRevealKey(current, findingId));
      setOnboardingRevealErrors((current) => withoutRevealKey(current, findingId));
      return;
    }
    await revealOnboardingLeakPreview(findingId);
  }

  function renderInviteFamilyScreen() {
    // Per-visit reveal gates for the two member seats — VIEW state, like
    // create-account's, because "the user has left this field" is about this
    // visit to the form and must never persist or replay.
    const view = resolveFamilyInviteRuntimeView({
      ...snapshot.context.familyInvite,
      memberEmailExited,
    });
    const interaction: ProjectedInteraction = {
      memberEmails: view.memberEmails,
      memberEmailHints: view.memberEmailHints,
      familyInviteSendError: view.sendError,
      onChangeMemberEmail: (index, email, source) => sendUi({ type: 'FAMILY_INVITE_MEMBER_CHANGED', index, email }, source),
      onBlurMemberEmail: (index) => setMemberEmailExited((prev) => {
        if (prev[index]) return prev;
        const next: [boolean, boolean] = [...prev];
        next[index] = true;
        return next;
      }),
      textOverrides: view.textOverrides,
      primaryDisabled: view.primaryIntent == null,
      onPrimary: view.primaryIntent
        ? (source) => sendUi(familyInviteIntentToEvent(view.primaryIntent!), source)
        : undefined,
      onSecondary: (source) => sendUi(familyInviteIntentToEvent(view.secondaryIntent), source),
      onBack: (source) => sendUi(familyInviteIntentToEvent(view.backIntent), source ?? 'progress-header-back'),
      onPressSlot: (target) => {
        const intent = resolveFamilyInviteSlotIntent(target);
        if (intent) sendUi(familyInviteIntentToEvent(intent), projectedInteractionTestID(target));
      },
    };
    return (
      <FigmaScreenScaler>
        <GetStartedScreen stateId="invite-family" interaction={interaction} />
      </FigmaScreenScaler>
    );
  }

  function renderInviteFamilySuccessScreen() {
    // Success confirmation after `sendFamilyInvites` settled (owner 2026-08-07).
    // Copy branches on HOW MANY seats were actually invited (`sentEmails`,
    // captured at send time); the StickyButton Continue is the only way forward.
    const view = resolveFamilyInviteSuccessRuntimeView(snapshot.context.familyInvite);
    const interaction: ProjectedInteraction = {
      textOverrides: view.textOverrides,
      onPrimary: (source) => sendUi(familyInviteIntentToEvent(view.continueIntent), source),
    };
    return (
      <FigmaScreenScaler>
        <GetStartedScreen stateId="invite-family-success" interaction={interaction} />
      </FigmaScreenScaler>
    );
  }

  function renderFirstSessionScreen() {
    if (snapshot.matches({ firstSession: 'returningBridge' })) {
      return (
        <FigmaScreenScaler>
          <ReturningAccountBridge onContinue={() => send({ type: 'ONBOARDING_ACKNOWLEDGE_RETURNING' })} />
        </FigmaScreenScaler>
      );
    }
    if (snapshot.matches({ firstSession: 'inviteFamily' })) {
      return renderInviteFamilyScreen();
    }
    if (snapshot.matches({ firstSession: 'inviteFamilySuccess' })) {
      return renderInviteFamilySuccessScreen();
    }
    const view = resolveOnboardingRuntimeView(snapshot);
    if (!view) {
      return (
        <FigmaScreenScaler>
          <AppStartupLoader testID="app-first-session-loader" />
        </FigmaScreenScaler>
      );
    }

    const { primaryEvent, secondaryEvent, backEvent } = view;
    const leakPreviews = deriveOnboardingLeakPreviewsFromAppState(snapshot.context.appState);
    // Legacy singular slot: the LAST-RESORT fallback for a card whose ordinal
    // cannot be resolved from the screen structure. It is NOT the card data —
    // every rendered card indexes `leakPreviews` by its own ordinal, and its
    // reveal is keyed by its own findingId (T-238).
    const leakPreview = leakPreviews?.[0];
    // One task preview per open critical credential finding, in the same order
    // as leakPreviews, so every card of a multi-fix carousel names ITS leak
    // (T-238 F3). taskPreview stays the first entry for single-card screens.
    const taskPreviews = deriveOnboardingTaskPreviewsFromAppState(snapshot.context.appState);
    const taskPreview = deriveOnboardingTaskPreviewFromAppState(snapshot.context.appState);
    const monitorLeaks = deriveOnboardingMonitorLeaksFromAppState(snapshot.context.appState);
    // Live post-flow recap: browser/device outcomes from the machine context.
    // On the post-flow no-leaks path the dedicated recap zero-state keeps the
    // established "No leaks found / Monitoring" caption (T-145 must not collapse
    // this into the result-screen Confirmed row). Result screens leave
    // postflowRecap undefined and use the honest deriveDeepScanRows path,
    // which always includes a neutral Phone security row when proof is missing.
    const postflowRecap = resolveOnboardingPostflowRecap(snapshot) ?? undefined;
    // T-209 / D7: the deep-scan row captions depend on WHERE in the flow we are,
    // not only on app state — the backend already holds an open task for the
    // seeded leak while the user is still on the pre-action result screen.
    // `resolveOnboardingPostflowRecap` is non-null exactly inside the post-flow
    // chapter, i.e. after the leak/browser/device chapters have run, so it is
    // the honest discriminator: recap => post-action, otherwise scan result.
    //
    // T-209 / pass-5 CRITICAL 1+2: an empty scan result is BOTH the
    // null-app-state default and a genuinely clean scan, and the machine has
    // supported paths (`runFirstSessionScan` fails; the product API answers 200
    // with no scan-run marker when orchestration is down) that reach this
    // screen having learned nothing. `resolveFirstSessionScanOutcomeFor` is the
    // single discriminator, and the raw buckets are sealed so nothing here can
    // second-guess it.
    const scanOutcome = resolveFirstSessionScanOutcomeFor(snapshot);
    const deepScanRows = deriveDeepScanRowsFromAppState(snapshot.context.appState, {
      stage: postflowRecap ? 'postflow-recap' : 'scan-result',
      scanOutcome,
    });
    const postflowDeepScanRows = postflowRecap && postflowRecap.noLeaksFound
      ? noLeaksPostflowDeepScanRows()
      : deepScanRows;
    // Reveal state is handed over PER FINDING (T-238). Every rendered leak card
    // resolves its own preview by ordinal and then looks up ITS findingId in
    // these records, so card 2 and card 3 are revealable on their own and no
    // card can paint a sibling's plaintext. Nothing here may collapse to
    // `leakPreviews[0]`.
    const revealedLeakPreviewValues = revealedOnboardingLeakPreviews;
    const revealLeakPreviewErrors = onboardingRevealErrors;
    // T-275: live account risk only via singular accessor (never appState.protect.risk.*).
    // Onboarding estimated/pre-auth risk remains a non-current fallback when app-state is absent.
    const canonicalAccountRisk = canonicalRiskStatusFromAppState(snapshot.context.appState)
      ?? snapshot.context.onboarding.riskStatus
      ?? null;
    // T-275: one conversion owner — never inline ProductRisk→indicator maps.
    const canonicalIndicator = canonicalAccountRisk
      ? productRiskToIndicatorStatus(canonicalAccountRisk)
      : undefined;
    // T-275: only the one-shot consumed active claim (chapter/postflow) supplies
    // drop endpoints — never session-start alone, never a reusable historical pair.
    const activeDrop = snapshot.context.onboarding.activeRiskDropClaim;
    const dropEndpoints = activeDrop
      ? {
          from: productRiskToIndicatorStatus(activeDrop.from),
          to: productRiskToIndicatorStatus(activeDrop.to),
          fromHandle: activeDrop.fromHandle,
          toHandle: activeDrop.toHandle,
        }
      : null;
    const interaction: ProjectedInteraction = {
      hideFunnelFirstCheck: !firstSessionScanPaintsFunnelFirstCheck(
        snapshot.context.appState?.onboardingVariant ?? 'standard',
      ),
      accountRiskStatus: canonicalIndicator,
      riskDropFromStatus: dropEndpoints?.from,
      riskDropToStatus: dropEndpoints?.to,
      riskDropFromHandle: dropEndpoints?.fromHandle,
      riskDropToHandle: dropEndpoints?.toHandle,
      onPrimary: primaryEvent ? (source) => sendUi(primaryEvent, source) : undefined,
      // While the chapter checkpoint is in flight the destination screen is
      // already mounted with no events. Paint the CTA as loading rather than
      // as an unexplained grey (feedback D6).
      primaryBusy: view.primaryBusy,
      onSecondary: secondaryEvent ? (source) => sendUi(secondaryEvent, source) : undefined,
      onBack: backEvent ? (source) => sendUi(backEvent, source ?? 'progress-header-back') : undefined,
      backBusy: view.backBusy,
      // Dynamic header progress (segmented bar) driven by the DeepScan findings.
      progress: resolveOnboardingProgress(snapshot) ?? undefined,
      // Device-outdated days are backend-derived whenever the mounted runtime
      // has a known value. The authored "83" remains the fixture fallback for
      // deterministic screens without runtime app-state data.
      textOverrides: view.textOverrides,
      // Dynamic monitor copy/list driven by backend Protect findings. Until the
      // app state loads, the raw scan-result ids are passed instead and the
      // projection renders honest type-derived rows (never fabricated sample
      // identifiers — G7 dataSensitivity).
      monitorLeaks: monitorLeaks ?? scanOutcomeMonitorableLeakIds(scanOutcome),
      // Monitor breach-list trailing per Figma (§6): MonitorDetails (and the
      // multi-monitor-details Storybook variant) show setup progress-point
      // circles; LeaksToMonitor (and any other monitor list) has no trailing.
      // Without progress-point, ListCard TEXT expands to full Content 261 vs
      // pin 229 (text-layout yellow/fail class).
      monitorListTrailing:
        view.stateId === 'monitor-details' || view.stateId === 'multi-monitor-details'
          ? 'progress-point'
          : 'none',
      // Same constant the onboardingTimer effect uses for this beat, so the
      // rows can never finish ahead of (or behind) the machine transition.
      monitorSetupDurationMs:
        view.stateId === 'monitor-details' || view.stateId === 'multi-monitor-details'
          ? onboardingTimerDurationMs('monitorSetup')
          : undefined,
      // Tactile card tap-highlight is only for the post-flow recap cards; other
      // screens (deepscan-result) keep cards at natural height with full borders.
      cardTapHighlight: view.stateId.startsWith('postflow-'),
      leakPreview,
      leakPreviews,
      taskPreview,
      taskPreviews,
      onRevealLeakPreviewFinding: productSession
        ? (findingId: string) => toggleOnboardingLeakPreview(findingId)
        : undefined,
      revealedLeakPreviewValues,
      revealLeakPreviewErrors,
      deepScanRows: postflowDeepScanRows,
      deepScanBrowserProtectionRows: deepScanBrowserProtectionRows({
        proof: snapshot.context.extensionActivationProof,
        appState: snapshot.context.appState,
      }),
      // Live DeepScan card summary counts (feedback: static "5 leaks total ·
      // 1 critical" regardless of the real scan) + the theatrical window from
      // the same constant the onboardingTimer effect uses.
      //
      // Owner R1 2026-08-04: the card must show the real FUNNEL scan result.
      // Backend Protect findings alone left it with nothing to say during the
      // first session — app state still loading, or loaded with no findings
      // while the funnel's own result sat in `scanResults` — so the row fell
      // back to a placeholder that duplicated the card's caption. The funnel
      // result is read in handoff order: the machine's replayed
      // `onboarding.scanResults`, then the session payload that carried it
      // (available before app state resolves at all).
      deepScanLeakSummary: deepScanLeakSummaryForCard({
        appState: snapshot.context.appState,
        scanOutcome,
      }),
      deepScanTotalDurationMs: onboardingDeepScanDurationMs(),
      postflowRecap,
      // Spot-the-fake choices: Fake advances on reveal; Safe replays the stamp.
      onAnswerQuiz: (option, source) => {
        if (view.stateId === 'spot-fake-reveal' && option === 'safe') {
          setStampAttention((n) => n + 1);
          return;
        }
        if (view.stateId === 'spot-fake-reveal' && option === 'fake') {
          if (primaryEvent) sendUi(primaryEvent, source);
          return;
        }
        const event = option === 'fake' ? primaryEvent : secondaryEvent;
        if (event) sendUi(event, source);
      },
      // The chosen option, so the reveal keeps that button in its pressed fill
      // and the answer tap's press ramp is completed instead of cut (the
      // colour-blink report, 2026-08-07). `spotFakeCorrect` records whether the
      // answer was right, and Fake IS the right answer on this quiz, so
      // correct=true means Fake was chosen and correct=false means Safe.
      chosenQuizOption: snapshot.context.onboarding.spotFakeCorrect == null
        ? undefined
        : (snapshot.context.onboarding.spotFakeCorrect ? 'fake' as const : 'safe' as const),
      // Replay key for the reveal stamp attention animation.
      stampAttentionKey: view.stateId === 'spot-fake-reveal' ? stampAttention : undefined,
      // T-423: the device-outdated numeral counts to the canonical backend gap.
      // Older payloads fall back to the open update_ios task timestamps; null
      // stays fail-closed instead of painting the authored Figma constant.
      deviceOutdatedDaysBehind: resolveIosUpdateDaysBehind(appState),
    };
    // T-237: the pin to project. For every screen whose Figma frame has no
    // baked-in modal sheet this IS view.stateId; for the sheet-over-screen
    // states it is the committed pin whose backdrop is the screen the user
    // actually came from. The hosted background follows the SAME pin — it
    // supplies that pin's suppressed OnboardingBackground slot, so reading it
    // from the logical state would paint the nudge glow behind intro content.
    const projectedStateId = resolveOnboardingProjectedStateId(snapshot) ?? view.stateId;
    return (
      <FigmaScreenScaler
        fullBleedBackground={<OnboardingBackgroundHost stateId={projectedStateId} />}
      >
        <View style={styles.hostedScreenFrame}>
          <FirstSessionScreenComposer
            stateId={view.stateId}
            projectedStateId={projectedStateId}
            interaction={interaction}
            hostedBackground
          />
        </View>
      </FigmaScreenScaler>
    );
  }

  function renderForgotPasswordScreen() {
    // Merge the per-visit blur gates into the state handed to the resolver, the
    // same shape GetStartedFreeScreen uses. Off the reset form the gates read
    // false, so a later visit opens silent.
    const forgotPassword = {
      ...snapshot.context.forgotPassword,
      newPasswordBlurred: onResetPasswordForm && resetPasswordBlurred.newPassword,
      confirmPasswordBlurred: onResetPasswordForm && resetPasswordBlurred.confirmPassword,
    };
    const view = resolveForgotPasswordRuntimeView(forgotPassword);
    const sendForgotPasswordIntent = (intent: ForgotPasswordRuntimeIntent | null, source?: MachineEventSource | string) => {
      if (intent) sendUi(forgotPasswordIntentToEvent(intent), source);
    };
    const interaction: ProjectedInteraction = {
      email: forgotPassword.email,
      onChangeEmail: (email, source) => sendUi({ type: 'FORGOT_PASSWORD_EMAIL_CHANGED', email }, source),
      newPassword: forgotPassword.newPassword,
      confirmPassword: forgotPassword.confirmPassword,
      onChangeNewPassword: (password, source) => sendUi({ type: 'FORGOT_PASSWORD_NEW_PASSWORD_CHANGED', password }, source),
      onChangeConfirmPassword: (password, source) => sendUi({ type: 'FORGOT_PASSWORD_CONFIRM_PASSWORD_CHANGED', password }, source),
      onBlurNewPassword: () => setResetPasswordBlurred((prev) => (prev.newPassword ? prev : { ...prev, newPassword: true })),
      onBlurConfirmPassword: () => setResetPasswordBlurred((prev) => (prev.confirmPassword ? prev : { ...prev, confirmPassword: true })),
      textOverrides: view.textOverrides,
      inputHelpers: view.inputHelpers,
      onPrimary: (source) => sendForgotPasswordIntent(view.primaryIntent, source),
      onSecondary: (source) => sendForgotPasswordIntent(view.secondaryIntent, source),
      secondaryDisabled: view.secondaryDisabled,
      onBack: (source) => sendForgotPasswordIntent(view.backIntent, source ?? 'progress-header-back'),
      onPressSlot: (target) => sendForgotPasswordIntent(resolveForgotPasswordSlotIntent(view, target), projectedInteractionTestID(target)),
      onPressText: (target) => sendForgotPasswordIntent(resolveForgotPasswordSlotIntent(view, target), projectedInteractionTestID(target)),
    };

    // Same hosted layering as the first session: the shared morph host renders
    // both background layers under the screen; the screen suppresses its local
    // background slots so adjacent steps morph instead of hard-swapping.
    return (
      <FigmaScreenScaler
        fullBleedBackground={<ForgotPasswordBackgroundHost stateId={view.stateId} />}
      >
        <View style={styles.hostedScreenFrame}>
          <ForgotPasswordReviewScreen stateId={view.stateId} interaction={interaction} runtimeState={forgotPassword} hostedBackground />
        </View>
      </FigmaScreenScaler>
    );
  }

  function renderGetStartedScreen() {
    const getStarted = snapshot.context.getStarted;
    const view = resolveGetStartedRuntimeView(getStarted);
    const sendGetStartedIntent = (intent: GetStartedRuntimeIntent | null, source?: MachineEventSource | string) => {
      if (intent) sendUi(getStartedIntentToEvent(intent), source);
    };
    const primaryIntent = view.primaryIntent;
    const secondaryIntent = view.secondaryIntent;
    const backIntent = view.backIntent;
    const interaction: ProjectedInteraction = {
      email: getStarted.email,
      onChangeEmail: (email, source) => sendUi({ type: 'GET_STARTED_EMAIL_CHANGED', email }, source),
      password: getStarted.password,
      onChangePassword: (password, source) => sendUi({ type: 'GET_STARTED_PASSWORD_CHANGED', password }, source),
      // Create-account's password field is a real runtime Input. Keep its
      // local visibility affordance available without adding a machine event;
      // the field owns masking state while App owns the controlled value.
      passwordVisibilityToggle: true,
      riskOverrides: view.riskOverrides,
      textOverrides: view.textOverrides,
      inputHelpers: view.inputHelpers,
      // T-453: existing-account response — the helper link exits to sign-in.
      existingAccountHelper: view.accountExistsHelper
        ? { onTryLogin: () => sendGetStartedIntent({ type: 'getStarted.tryLogin' }) }
        : undefined,
      selectedOptionLabels: view.selectedChoiceLabels,
      onPrimary: primaryIntent ? (source) => sendGetStartedIntent(primaryIntent, source) : undefined,
      onSecondary: secondaryIntent ? (source) => sendGetStartedIntent(secondaryIntent, source) : undefined,
      onBack: backIntent ? (source) => sendGetStartedIntent(backIntent, source ?? 'progress-header-back') : undefined,
      onPressSlot: (target) => sendGetStartedIntent(resolveGetStartedSlotIntent(view, target), projectedInteractionTestID(target)),
    };

    // Same hosted layering as the first session: the shared morph host renders
    // both background layers under the screen; the screen suppresses its local
    // background slots so adjacent funnel steps morph instead of hard-swapping.
    const scanResults = snapshot.context.protectFree.scanning ? null : snapshot.context.protectFree.lastScan;
    // T-275: free scan-result account risk only via singular accessor.
    const freeCanonicalRisk = canonicalRiskStatusFromAppState(appState);
    // T-275: scan-result glow is canonical-only — never fall back to
    // view.riskOverrides (exposure/estimate) when channels are null.
    // Non-scan-result steps still use view.riskOverrides for pre-auth exposure.
    const scanResultChannels = view.stateId === 'scan-result'
      ? resolveGetStartedScanResultRiskChannels(freeCanonicalRisk)
      : null;
    const getStartedRiskStatus = view.stateId === 'scan-result'
      ? (scanResultChannels?.gauge ?? undefined)
      : riskStatusFromOverrides(view.riskOverrides);
    return (
      <FigmaScreenScaler
        fullBleedBackground={<GetStartedFreeBackgroundHost stateId={view.stateId} riskStatus={getStartedRiskStatus} />}
      >
        <View style={styles.hostedScreenFrame}>
          <GetStartedFreeScreen
            stateId={view.stateId}
            interaction={interaction}
            runtimeState={getStarted}
            scanResults={scanResults}
            canonicalRiskStatus={freeCanonicalRisk}
            hostedBackground
          />
        </View>
      </FigmaScreenScaler>
    );
  }

  {
    const visualCaptureTree = visualCapture.renderVisualCapture();
    if (visualCaptureTree) return visualCaptureTree;
  }

  // Renders the focused non-Protect surface inside the persistent shell.
  // Canonical MVP tabs are Protect · Train · Profile; legacy Hub/Cleanup/Shield
  // are no longer top-level app sections.
  function renderSurface() {
    if (selectedTab === 'profile' && diagnosticsOpen) {
      return <DeveloperDiagnosticsScreen onBack={() => requestCanonicalRoute({ area: 'profile', screen: 'main' })} />;
    }

    if (selectedTab === 'train') {
      // Expired subscription: replace Train surface with re-engage gate.
      if (appState && isSubscriptionBlocked(appState)) {
        return <TrainExpiredScreen view={buildExpiredTrainView()} />;
      }

      return (
        <View style={styles.trainRuntimeSurface} testID="screen-train-runtime">
          <TrainScreenTransitionFrame stateKey={formatCanonicalUrl(trainView)}>
            <TrainRuntimeScreen
              services={services}
              sessionToken={productSession?.sessionToken ?? ''}
              onNavigate={handleTrainRuntimeNavigate}
              onMutationLifecycle={(event) => {
                if (event.type === 'started') {
                  send({ type: 'TRAIN_RUNTIME_MUTATION_STARTED', mutationId: event.mutationId, kind: event.kind });
                  return;
                }
                send({
                  type: 'TRAIN_RUNTIME_MUTATION_SETTLED',
                  mutationId: event.mutationId,
                  kind: event.kind,
                  success: event.success,
                });
              }}
              onRuntimeSignal={(signal) => sendUi(trainRuntimeSignalToEvent(signal), signal._source)}
              onLocationChange={(location) => sendUi({ type: 'TRAIN_LOCATION_CHANGED', location })}
              routePath={formatCanonicalUrl(trainView)}
              emailProvider={resolveTrainEmailProvider(appState?.profile.email).provider}
            />
          </TrainScreenTransitionFrame>
        </View>
      );
    }

      // T-555: Profile owns its 16pt gutters, so the frame adds none and the
      // root spans the full host width. Main is a scroll document under the
      // shell tab bar; every drill-in owns its scroll (ProfileSubflowScroll)
      // and a bottom-anchored CTA, so it gets the measured host instead — a
      // `height: 100%` root inside the scroll document resolves to 0pt.
      const profileScreen = profileLocation.screen === 'diagnostics' ? 'main' : profileLocation.screen;
      const profileDrillIn = !resolveShellRouteChrome({ activeTab: 'profile', profileScreen }).hasTabBar;
      return (
        <SurfaceFrame resetKey={`profile:${profileLocation.screen}`} gutters={false} scroll={!profileDrillIn}>
          <PricingContentProvider value={pricingContent}>
            <ProfileScreen
              {...screenProps}
              productSession={productSession}
              sendFamilyInvites={services.sendFamilyInvites}
              changePassword={services.changePassword}
              requestAccountDeletion={services.requestAccountDeletion}
              requestSubscriptionCancellation={services.requestSubscriptionCancellation}
              pricingContent={pricingContent}
              authMessage={authMessage}
              productApiBusy={productApiBusy}
              onLogout={logout}
              onAccountDeleted={handleAccountDeleted}
              flow={profileLocation.screen === 'diagnostics' ? 'main' : profileLocation.screen}
              onFlowChange={(screen) => requestCanonicalRoute({ area: 'profile', screen })}
              onOpenDiagnostics={() => requestCanonicalRoute({ area: 'profile', screen: 'diagnostics' })}
            />
          </PricingContentProvider>
        </SurfaceFrame>
      );
  }

  // Degraded: the session is valid but app-state failed to load. Without this
  // gate the main shell rendered silently with `appState = null`, which for a
  // fresh user swallowed the first-session onboarding route (feedback: "logged
  // in for the first time and onboarding never started"). Show the failure and
  // a retry instead of a half-empty shell.
  if (!booting && fontsLoaded && snapshot.matches('degraded')) {
    return (
      <AppRecoveryScreen
        message={appStateMessage || undefined}
        onRetry={refreshProductState}
      />
    );
  }

  if (!booting && fontsLoaded && showingAccountStartup && !showingFirstSession && !showingAuthScreen
    && !snapshot.matches('forgotPassword') && !snapshot.matches('getStarted') && !hasLoadedAppState) {
    return (
      <AppStartupLoader testID="app-startup-loader" />
    );
  }

  // Authenticated main app: a single persistent MainAppShell holds the focused
  // surface (Protect/Train/Profile) so switching tabs is a focus change, not a
  // screen replacement. Auth/first-session/get-started flows keep their own
  // screen-level rendering and are intentionally outside the shell.
  if (!booting && fontsLoaded && !showingFirstSession && !showingAuthScreen
    && !snapshot.matches('forgotPassword') && !snapshot.matches('getStarted') && hasLoadedAppState) {
    const isFree = appState.plan === 'free';
    const shellTabs = resolveShellTabs(appState.plan);
    const activeTab = resolveActiveShellTab(selectedTab);
    const protectSurface = protectAppSurface.renderRuntimeProtectSurface(isFree);

    // T-449 (owner, «чого зникає табар, коли з'являється тост»): the
    // resolved/removed toast beat over the OVERVIEW underlay keeps the tab
    // bar (the toast floats above it); beats over drill-in underlays stay
    // tab-bar-less per the Figma TaskRemoved/TaskResolved pins.
    const protectLocation = snapshot.context.protect.location;
    const protectToastBeatOverMain = protectLocation.name === 'taskResolution'
      && (protectLocation.screen === 'resolved' || protectLocation.screen === 'removed')
      && protectTaskSheetUnderlayLocation(snapshot.context.protect).name === 'main';
    const showShellTabBar = selectedTab !== 'protect' || isFree
      || protectLocation.name === 'main' || protectToastBeatOverMain;
    // Synchronous shell-owned chrome from the committed route — not a lagged
    // child effect — so the tab bar matches the same paint as Train
    // home ↔ lesson sticky transitions.
    const routeChrome = resolveShellRouteChrome({
      activeTab,
      trainScreen: trainView.screen,
      trainLessonStep: trainView.screen === 'lesson' ? (trainView.step ?? 'lesson') : null,
      profileScreen: profileLocation.screen,
      protectLocationName: snapshot.context.protect.location.name,
      protectToastBeatOverMain,
      isFreePlan: isFree,
    });

    return (
      <MainAppShell
        surfaceKey={resolveSurfaceKey(activeTab, diagnosticsOpen)}
        activeTab={activeTab}
        onSelectTab={(tab) => handleSelectTab(tab)}
        tabs={shellTabs}
        showTabBar={showShellTabBar}
        routeChrome={routeChrome}
      >
        {sensitiveRevealActive ? (
          <View pointerEvents="none" style={styles.nativeLifecycleWitness} testID="sensitive-reveal-active" />
        ) : null}
        {isTrackingDiagnosticReceiptEnabled() ? <TrackingDiagnosticReceiptHost /> : null}
        {selectedTab === 'protect' ? protectSurface : renderSurface()}
      </MainAppShell>
    );
  }

  // Full-bleed like MainAppShell (no SafeAreaView): the Figma auth/onboarding
  // frames already reserve the status-bar zone, so insetting here letterboxed
  // them on device (see src/shell/shellChrome.test.ts invariants).
  return (
    <View style={styles.safeArea}>
      <StatusBar barStyle="light-content" />
      <View style={styles.root}>
        {sensitiveRevealActive ? (
          <View pointerEvents="none" style={styles.nativeLifecycleWitness} testID="sensitive-reveal-active" />
        ) : null}
        {booting || !fontsLoaded ? (
          // Font-independent by construction: the Loader frame is logo-only, so
          // it is safe on the `!fontsLoaded` branch that the old text was not.
          <AppStartupLoader testID="app-boot-loader" />
        ) : showingFirstSession ? (
          renderFirstSessionScreen()
        ) : snapshot.matches('forgotPassword') ? (
          renderForgotPasswordScreen()
        ) : snapshot.matches('getStarted') ? (
          renderGetStartedScreen()
        ) : visualAuthRoute === 'createAccount' || (!visualMode && authRoute === 'createAccount') ? (
          <ScrollView contentContainerStyle={[styles.page, styles.authVisualPage]} keyboardShouldPersistTaps="handled">
            <CreateAccountScreen
              authEmail={authEmail}
              authPassword={authPassword}
              authMessage={authMessage}
              authError={authError}
              productApiBusy={productApiBusy}
              onAuthEmailChange={(email) => send({ type: 'AUTH_EMAIL_CHANGED', email })}
              onAuthPasswordChange={(password) => send({ type: 'AUTH_PASSWORD_CHANGED', password })}
              onLogin={loginProductAccount}
              onRegister={registerProductAccount}
              onSignIn={() => visualMode
                ? visualCapture.requestAuthRoute('signIn')
                : requestCanonicalRoute({ area: 'auth', screen: 'signIn' })}
            />
          </ScrollView>
        ) : (
          <FigmaSignInViewport
            authEmail={authEmail}
            authPassword={authPassword}
            authMessage={authMessage}
            authError={authError}
            productApiBusy={productApiBusy}
            onAuthEmailChange={(email) => send({ type: 'AUTH_EMAIL_CHANGED', email })}
            onAuthPasswordChange={(password) => send({ type: 'AUTH_PASSWORD_CHANGED', password })}
            onLogin={loginProductAccount}
            onRegister={registerProductAccount}
            // T-450: no email reset flow yet — the sign-in "Forgot password?
            // Contact support" line exits to the Freshdesk ticket form.
            // Pre-auth surface: no profile email, so the canonical builder
            // emits the plain ticket-form URL without an empty query param.
            onOpenSupportTicket={() => void openSupportTicket()}
            onGetStarted={() => sendUi({ type: 'GET_STARTED_START' }, 'progress-header-back')}
          />
        )}
      </View>
    </View>
  );
}

function navigationGestureRouteId(snapshot: MobileAppSnapshot): string {
  if (snapshot.matches('firstSession')) return 'onboarding.transientBeat';
  if (snapshot.matches('forgotPassword')) {
    return snapshot.context.forgotPassword.location === 'changePassword'
      || snapshot.context.forgotPassword.location === 'resettingPassword'
      ? 'auth.passwordReset'
      : 'auth.forgotPassword';
  }
  if (snapshot.matches('getStarted')) return 'getStarted';
  const location = projectCanonicalLocation(snapshot);
  return location ? findCanonicalRouteDescriptor(location)?.id ?? 'unknown' : 'unknown';
}

function navigationGestureHasBackEntry(snapshot: MobileAppSnapshot): boolean {
  if (snapshot.matches('firstSession')) {
    return snapshot.context.onboarding.history.length > 0
      || snapshot.matches({ firstSession: 'inviteFamily' });
  }
  if (snapshot.matches('getStarted')) return snapshot.context.getStartedHistory.length > 0;
  if (snapshot.matches('forgotPassword')) return true;
  if (snapshot.context.selectedTab === 'protect') return snapshot.context.protect.history.length > 0;
  if (snapshot.context.selectedTab === 'train') return snapshot.context.trainLocation.screen !== 'home';
  if (snapshot.context.selectedTab === 'profile') return snapshot.context.profileLocation.screen !== 'main';
  return false;
}

function navigationGestureBackEvent(snapshot: MobileAppSnapshot): MobileAppEvent | null {
  if (snapshot.matches('firstSession')) return { type: 'ONBOARDING_BACK' };
  if (snapshot.matches('forgotPassword')) return { type: 'FORGOT_PASSWORD_BACK_TO_LOGIN' };
  if (snapshot.matches('getStarted')) return { type: 'GET_STARTED_BACK' };
  if (snapshot.context.selectedTab === 'protect' && snapshot.context.protect.location.name !== 'main') {
    return { type: 'PROTECT_BACK' };
  }
  if (snapshot.context.selectedTab === 'train' && snapshot.context.trainLocation.screen !== 'home') {
    const parent = trainParentLocation(snapshot.context.trainLocation);
    return parent ? { type: 'ROUTE_REQUESTED', location: parent, source: 'user' } : null;
  }
  if (snapshot.context.selectedTab === 'profile' && snapshot.context.profileLocation.screen === 'diagnostics') {
    return { type: 'ROUTE_REQUESTED', location: { area: 'profile', screen: 'main' }, source: 'user' };
  }
  return null;
}

function trainParentLocation(location: Extract<CanonicalLocation, { area: 'train' }>): Extract<CanonicalLocation, { area: 'train' }> | null {
  switch (location.screen) {
    case 'home': return null;
    case 'courses':
    case 'certificates': return { area: 'train', screen: 'home' };
    case 'course':
    case 'lesson':
    case 'quiz':
    case 'certificate': return { area: 'train', screen: 'courses' };
  }
}

/**
 * Every blocking loader in the app is the transferred Figma Loader screen
 * (6438:15329, T-244): the SymbolLogo tile on Background/Default with the metal
 * ring. The frame carries no copy by design, so the old wordmark + "Onyx is
 * getting ready" + per-state message are gone; the testID stays so scenario
 * journeys keep resolving the surface.
 */
function AppStartupLoader({ testID }: { testID: string }) {
  return <SharedAppStartupLoader testID={testID} />;
}

const styles = StyleSheet.create({
  safeArea: {
    backgroundColor: colors.background,
    flex: 1,
  },
  root: {
    backgroundColor: colors.background,
    flex: 1,
  },
  nativeLifecycleWitness: {
    height: 1,
    opacity: 0,
    position: 'absolute',
    width: 1,
  },
  // Screen-size frame for flows that layer a shared background morph host
  // under the projected foreground (first session, GetStartedFree funnel,
  // forgot-password recovery).
  hostedScreenFrame: {
    height: '100%',
    overflow: 'hidden',
    position: 'relative',
    width: '100%',
  },
  page: {
    gap: spacing.lg,
    paddingBottom: 104,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.md,
  },
  authPage: {
    paddingBottom: spacing.xl,
  },
  authVisualPage: {
    paddingBottom: spacing.xl,
    paddingTop: 96,
  },
  trainRuntimeSurface: {
    flex: 1,
    minHeight: 0,
    width: '100%',
  },
});
