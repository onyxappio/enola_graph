import { useCallback, useEffect, useReducer, useRef } from 'react';
import { StyleSheet, View } from 'react-native';

import { BottomSheet as FigmaBottomSheet, type BottomSheetCta } from '../../../components/figma/bottomSheet/BottomSheet';
import { ExtensionSetupVideo } from '../../../components/figma/bottomSheet/ExtensionSetupVideo';
import { bottomSheetTaskSpacing } from '../../../components/figma/bottomSheet/bottomSheetSpacing';
import { MainAppShell } from '../../../shell/MainAppShell';
import type { ProductTrainState } from '../../../api';
import type { MobileAppEvent } from '../../../state/mobileAppMachine.types';
import {
  clearProtectToast,
  createProtectRuntimeMachineState,
  createProtectRuntimeState,
  markProtectTaskOpened,
  protectMainLocation,
  protectTodoLocation,
  removeProtectTask,
  resolveProtectTask,
  type ProtectBackendTaskType,
  type ProtectRuntimeLocation,
  type ProtectRuntimeMachineState,
  type ProtectRuntimeTask,
} from '../../../state/protectRuntimeState';
import { taskTypeToSheetKind } from '../taskCtaArchetype';
import { buildTaskSheetContent } from '../taskSheetContent';
import { taskSheetFinding } from '../taskSheetFinding';
import { resolveProtectSurfaceModel, type ProtectSurfaceModel } from './protectSurfaceModel';
import { visualTrainProductSummary } from './visualTrainProductSummary';
import { ProtectSurface, type ProtectVisualTodoOverlay } from './ProtectSurface';
import type { ProtectTodoFrameKey } from './ProtectTodoScene';

/**
 * Figma-authored High overview band behind every protect.task.live sheet pin.
 * Same explicit visual-fixture band as ProtectLiveVisualHost task-detail /
 * High overview — never derived from local task level (T-275).
 */
export const PROTECT_TASK_VISUAL_CANONICAL_RISK_STATUS = 'high' as const;

export type ProtectTaskVisualState =
  | 'change-password'
  | 'change-password-confirm'
  | 'change-password-resolved'
  | 'monitor-leak'
  | 'monitor-leak-resolved'
  | 'update-ios'
  | 'update-ios-resolved'
  | 'update-onyx'
  | 'update-onyx-resolved'
  | 'browsing-protection'
  | 'browsing-protection-resolved'
  | 'suspicious-activity'
  | 'suspicious-activity-still'
  | 'suspicious-activity-resolved'
  | 'suspicious-activity-handled'
  | 'review-device-management'
  | 'review-device-management-question'
  | 'review-device-management-remove'
  | 'review-device-management-escalate'
  | 'review-device-management-work'
  | 'review-device-management-resolved';

type TaskVisualSpec = {
  id: string;
  taskType: ProtectBackendTaskType;
  category: ProtectRuntimeTask['category'];
  kind: ProtectRuntimeTask['kind'];
  level: ProtectRuntimeTask['level'];
  title: string;
  description: string;
  source?: string;
  finding?: ProtectRuntimeTask['finding'];
  /** Backend task metadata used by taskSheetFinding for pin-accurate fields. */
  details?: ProtectRuntimeTask['details'];
  browsingProtection?: 'on' | 'off';
  location: ProtectRuntimeLocation;
  resolved?: boolean;
  monitoring?: boolean;
  /** Content-builder variant for intermediate/resolved branch frames. */
  contentVariant?: 'confirming' | 'stillSeeing' | 'remove' | 'escalate' | 'guidedAnswer' | 'handled' | 'workSchool';
};

type TaskVisualSheetSpec = {
  contentNodeId: string;
  /** Visible BottomSheet instance; distinct from the screen's ContentWrapper. */
  sheetInstanceNodeId: string;
  cta: BottomSheetCta;
  frameKey: ProtectTodoFrameKey;
  height: number;
  tab: 'active' | 'resolved';
  text: string;
  top: number;
  /**
   * Compact-host content top inset for pins that sit above the steady-state
   * resolving geometry (e.g. confirm 496×356). Omit for 498×354 resolving hosts.
   */
  taskContentTopInset?: number;
};

// Pin sample profile email (Figma Protect task frames, version 2379798399526217483).
const PROFILE_EMAIL = 'janedoe@gmail.com';
const VISUAL_TRAIN_STATE: ProductTrainState = {
  catalogVersion: '2026-06-27',
  courses: [
    {
      courseId: 'digital-hygiene-basics',
      title: 'Safety basics',
      status: 'available',
      lessonCount: 100,
      completedLessons: 1,
      todosCompleted: 1,
      certificateSlug: 'safety-starter',
      lessons: [],
      finalQuiz: { status: 'locked' },
    },
  ],
  certificates: [],
};
/** Resolved-date provenance for pin copy "Resolved May 15, 2026". */
const RESOLVED_AT = '2026-05-15T00:00:00.000Z';
const NOW = RESOLVED_AT;

const PASSWORD_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.vimeoPassword',
  taskType: 'change_password',
  category: 'leak',
  kind: 'passwordLeak',
  level: 'critical',
  title: 'Change your Vimeo password',
  description: 'Your Vimeo password appeared in a data breach in 2023. Change it now to protect your account.',
  source: 'Vimeo, 2023',
  finding: {
    findingId: 'finding.vimeo.password',
    service: 'Vimeo',
    dataType: 'password',
    maskedValue: '••••••••',
    year: 2023,
    email: 'janedoe@gmail.com',
    canReveal: true,
  },
  details: {
    service: 'Vimeo',
    dataType: 'password',
    email: 'janedoe@gmail.com',
    leakDate: 'Oct 22, 2023',
    year: 2023,
  },
};

const MONITOR_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.monitorNetflixEmail',
  taskType: 'monitor_leak',
  category: 'leak',
  kind: 'nonPasswordLeak',
  level: 'high',
  title: 'Monitor Netflix email leak',
  description: "Your password is still safe, so there's nothing to reset. Onyx will keep this leak under watch.",
  source: 'Netflix, 2020',
  finding: {
    findingId: 'finding.netflix.email',
    service: 'Netflix',
    dataType: 'email',
    maskedValue: 'janedoe@gmail.com',
    year: 2020,
    email: 'janedoe@gmail.com',
    canReveal: false,
  },
  details: {
    service: 'Netflix',
    dataType: 'email',
    email: 'janedoe@gmail.com',
    leakDate: 'Oct 22, 2020',
    year: 2020,
    // URL-shaped source maps to the Website field via taskSheetFinding.
    source: 'https://www.netflix.com',
  },
};

const IOS_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.iosUpdate',
  taskType: 'update_ios',
  category: 'device',
  kind: 'deterministicDeviceIssue',
  level: 'critical',
  title: 'iPhone 16 Pro Max',
  description: 'Install the latest iOS update to close known security gaps.',
  details: {
    currentVer: '16.2',
    latestVer: '17.5',
    deviceModel: 'iPhone 16 Pro Max',
  },
};

const IOS_TASK_RESOLVED: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  ...IOS_TASK,
  details: {
    currentVer: '26.5',
    latestVer: '26.5',
    deviceModel: 'iPhone 16 Pro Max',
  },
};

const ONYX_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.onyxUpdate',
  taskType: 'update_onyx',
  category: 'device',
  kind: 'deterministicDeviceIssue',
  level: 'critical',
  title: 'Update Onyx',
  description: 'Install the latest Onyx update to get the newest protections.',
  details: {
    currentVer: '2.4.0',
    latestVer: '2.5.1',
  },
};

const BROWSING_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.browserProtection',
  taskType: 'turn_on_browser_protection',
  category: 'browsing',
  kind: 'browserProtectionOff',
  level: 'high',
  title: 'Turn on browsing protection',
  description: 'Onyx can block risky links when browsing protection is enabled.',
  browsingProtection: 'off',
};

const SUSPICIOUS_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.suspiciousActivity',
  taskType: 'check_suspicious_activity',
  category: 'device',
  kind: 'heuristicDeviceIssue',
  level: 'critical',
  title: 'Check suspicious phone activity',
  description: 'Restart your phone and re-check unusual safety signals.',
};

const DEVICE_REVIEW_TASK: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'> = {
  id: 'protect.task.deviceManagement',
  taskType: 'review_device_management',
  category: 'device',
  kind: 'heuristicDeviceIssue',
  level: 'critical',
  title: 'Review device management',
  description: 'Check whether a VPN, DNS, or device profile was added without your permission.',
};

function detailLocation(task: Omit<TaskVisualSpec, 'location'>): ProtectRuntimeLocation {
  // Pin frames: browsing + monitor are nonCritical sheets; password/device are critical.
  if (task.taskType === 'turn_on_browser_protection' || task.taskType === 'monitor_leak') {
    return { name: 'taskDetail', variant: 'nonCritical' };
  }
  return { name: 'taskDetail', variant: task.level === 'critical' ? 'critical' : 'nonCritical' };
}

function spec(
  base: Omit<TaskVisualSpec, 'location' | 'resolved' | 'monitoring' | 'contentVariant'>,
  location?: ProtectRuntimeLocation,
  flags: Partial<Pick<TaskVisualSpec, 'resolved' | 'monitoring' | 'contentVariant' | 'browsingProtection' | 'details'>> = {},
): TaskVisualSpec {
  return { ...base, location: location ?? detailLocation(base), ...flags };
}

const TASK_VISUAL_SPECS: Record<ProtectTaskVisualState, TaskVisualSpec> = {
  'change-password': spec(PASSWORD_TASK),
  'change-password-confirm': spec(PASSWORD_TASK, { name: 'taskResolution', screen: 'resolving' }, { contentVariant: 'confirming' }),
  'change-password-resolved': spec(PASSWORD_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true }),
  'monitor-leak': spec(MONITOR_TASK),
  'monitor-leak-resolved': spec(MONITOR_TASK, { name: 'taskResolution', screen: 'resolved' }, { monitoring: true }),
  'update-ios': spec(IOS_TASK),
  'update-ios-resolved': spec(IOS_TASK_RESOLVED, { name: 'taskResolution', screen: 'resolved' }, { resolved: true }),
  'update-onyx': spec(ONYX_TASK),
  'update-onyx-resolved': spec(ONYX_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true }),
  'browsing-protection': spec(BROWSING_TASK),
  'browsing-protection-resolved': spec(BROWSING_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true, browsingProtection: 'on' }),
  'suspicious-activity': spec(SUSPICIOUS_TASK),
  'suspicious-activity-still': spec(SUSPICIOUS_TASK, { name: 'taskResolution', screen: 'resolving' }, { contentVariant: 'stillSeeing' }),
  'suspicious-activity-resolved': spec(SUSPICIOUS_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true }),
  'suspicious-activity-handled': spec(SUSPICIOUS_TASK, { name: 'taskResolution', screen: 'removed' }, { contentVariant: 'handled' }),
  'review-device-management': spec(DEVICE_REVIEW_TASK),
  'review-device-management-question': spec(DEVICE_REVIEW_TASK, { name: 'taskResolution', screen: 'guidedAnswer' }, { contentVariant: 'guidedAnswer' }),
  'review-device-management-remove': spec(DEVICE_REVIEW_TASK, { name: 'taskResolution', screen: 'resolving' }, { contentVariant: 'remove' }),
  'review-device-management-escalate': spec(DEVICE_REVIEW_TASK, { name: 'taskResolution', screen: 'resolving' }, { contentVariant: 'escalate' }),
  'review-device-management-work': spec(DEVICE_REVIEW_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true, contentVariant: 'workSchool' }),
  'review-device-management-resolved': spec(DEVICE_REVIEW_TASK, { name: 'taskResolution', screen: 'resolved' }, { resolved: true }),
};

const TASK_VISUAL_SHEETS: Record<ProtectTaskVisualState, TaskVisualSheetSpec> = {
  'change-password': {
    contentNodeId: '2896:30364',
    sheetInstanceNodeId: '2896:30374',
    cta: 'on',
    frameKey: 'taskDetailCritical',
    height: 624,
    tab: 'active',
    top: 228,
    text: 'Needs action Vimeo password has leaked Your Vimeo password appeared in a data breach in 2023. Change it now to protect your account. Email janedoe@gmail.com Password •••••••• Leak date Oct 22, 2023 Change password I’ve already handled it',
  },
  'change-password-confirm': {
    contentNodeId: '2896:30610',
    sheetInstanceNodeId: '2896:30675',
    cta: 'on',
    frameKey: 'resolvingTask',
    height: 356,
    tab: 'active',
    // Pin BottomSheet instance y=496 / h=356. Steady-state resolving-task is a
    // separate 498×354 pin — do not share that top or this content inset.
    top: 496,
    taskContentTopInset: bottomSheetTaskSpacing.contentExtraTop,
    text: "Have you changed the password? Changing your password will help protect your account and data. Yes, I've changed it No, I'll do it later",
  },
  'change-password-resolved': {
    contentNodeId: '2896:30694',
    sheetInstanceNodeId: '2896:30731',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 377,
    tab: 'resolved',
    top: 475,
    text: 'Resolved Vimeo password changed You changed Vimeo password that appeared in a data breach in 2023. Email janedoe@gmail.com Resolved May 15, 2026',
  },
  'monitor-leak': {
    contentNodeId: '2896:30762',
    sheetInstanceNodeId: '2896:30914',
    cta: 'on',
    frameKey: 'taskDetailNonCritical',
    height: 550,
    tab: 'active',
    top: 302,
    text: "Needs attention Netflix email has leaked Your password is still safe, so there's nothing to reset. Onyx will keep this leak under watch. Website https://www.netflix.com Email janedoe@gmail.com Leak date Oct 22, 2020 Monitor this leak",
  },
  'monitor-leak-resolved': {
    contentNodeId: '2896:30774',
    sheetInstanceNodeId: '2896:31081',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 399,
    tab: 'resolved',
    top: 453,
    text: 'Resolved Netflix put under monitoring Your Netflix email leaked. We’ll alert you if a password or new data shows up for this address. Email janedoe@gmail.com Resolved May 15, 2026',
  },
  'update-ios': {
    contentNodeId: '2896:31117',
    sheetInstanceNodeId: '2896:31268',
    cta: 'on',
    frameKey: 'taskDetailCritical',
    height: 495,
    tab: 'active',
    top: 357,
    text: 'Needs attention Update your iPhone software Your iPhone is running an outdated iOS version. Security updates protect against known attacks. Current iOS 16.2 Latest iOS 17.5 Open software update',
  },
  'update-ios-resolved': {
    contentNodeId: '2896:31124',
    sheetInstanceNodeId: '2896:31234',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 377,
    tab: 'resolved',
    top: 475,
    text: 'Resolved iOS up to date Your iPhone 16 Pro Max’s OS is up to date and running the latest security features. Software update iOS 26.5 Resolved May 15, 2026',
  },
  'update-onyx': {
    contentNodeId: '2896:31409',
    sheetInstanceNodeId: '2896:31412',
    cta: 'on',
    frameKey: 'taskDetailCritical',
    height: 473,
    tab: 'active',
    top: 379,
    text: 'Needs attention Update Onyx A new version of Onyx is available. Update to get the latest checks and protections. Current 2.4.0 Latest 2.5.1 Update in App Store',
  },
  'update-onyx-resolved': {
    contentNodeId: '2896:31416',
    sheetInstanceNodeId: '2896:31418',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 321,
    tab: 'resolved',
    top: 531,
    text: 'Resolved Onyx is up to date Version 2.5.1 Resolved May 15, 2026',
  },
  'browsing-protection': {
    contentNodeId: '2896:31696',
    sheetInstanceNodeId: '2896:31853',
    cta: 'on',
    frameKey: 'taskDetailNonCritical',
    height: 719,
    tab: 'active',
    top: 133,
    text: 'Recommended Turn on browsing protection Browse without guessing which links are safe. Onyx blocks scam and phishing sites before they load. 1 In Settings go to Safari 2 Tap Extensions 3 Turn on Onyx Browsing Protection 4 Scroll down and Allow on All Websites Open settings',
  },
  'browsing-protection-resolved': {
    contentNodeId: '2896:31708',
    sheetInstanceNodeId: '2896:31710',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 298,
    tab: 'resolved',
    top: 554,
    text: 'Resolved Browsing protection on Onyx is now monitoring every link you tap in your browser. Resolved May 15, 2026',
  },
  'suspicious-activity': {
    contentNodeId: '2904:31922',
    sheetInstanceNodeId: '2904:31924',
    cta: 'on',
    frameKey: 'taskDetailCritical',
    height: 372,
    tab: 'active',
    top: 480,
    text: 'Needs attention Restart your phone to clear unusual phone safety signals Onyx found unusual signals. This does not prove spyware yet. A restart clears most false alarms, so let’s restart and check again. I restarted, check again',
  },
  'suspicious-activity-still': {
    contentNodeId: '2904:31928',
    sheetInstanceNodeId: '2904:32047',
    cta: 'on',
    frameKey: 'resolvingTask',
    height: 418,
    tab: 'active',
    top: 434,
    text: 'Needs attention Still seeing unusual signals The signal is still there after a restart. Apple can help check whether your iPhone is modified. If it stays, backing up and restoring your iPhone can remove it. Contact Apple Support I’ve already handled it',
  },
  'suspicious-activity-resolved': {
    contentNodeId: '2904:32077',
    sheetInstanceNodeId: '2904:32117',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 326,
    tab: 'resolved',
    top: 526,
    text: 'Resolved No unusual phone safety signals Onyx didn’t find signs your iPhone’s security has been modified. Resolved May 15, 2026',
  },
  'suspicious-activity-handled': {
    contentNodeId: '2904:32255',
    sheetInstanceNodeId: '2904:32257',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 326,
    tab: 'resolved',
    top: 526,
    text: 'Resolved Unusual phone safety signals marked as handled You’ve dismissed this check. Onyx couldn’t confirm the signal is gone. Resolved May 15, 2026',
  },
  'review-device-management': {
    contentNodeId: '2910:6708',
    sheetInstanceNodeId: '2910:6710',
    cta: 'on',
    frameKey: 'taskDetailCritical',
    height: 508,
    tab: 'active',
    top: 344,
    text: 'Monthly check Check for settings you didn’t add Some apps or people can add hidden settings that let them see what you do online or control your phone. This quick monthly check helps you spot any you didn’t set up. 1 In Settings go to General 2 Scroll to VPN & Device Management 3 See if anything there looks unfamiliar Open settings',
  },
  'review-device-management-question': {
    contentNodeId: '2911:39548',
    sheetInstanceNodeId: '2916:33831',
    cta: 'off',
    frameKey: 'resolvingTask',
    height: 414,
    tab: 'active',
    top: 438,
    // T-449 (owner): the return sheet repeats the Settings steps — see the
    // pin note in taskSheetContent.test.ts. Figma 2911:39547 still lacks them.
    text: 'Monthly check See anything you didn’t add? Look for anything you don’t remember setting up: a name you don’t know, or something an app or another person added. 1 In Settings go to General 2 Scroll to VPN & Device Management 3 See if anything there looks unfamiliar No, it all looks familiar Yes, there’s something I didn’t add',
  },
  'review-device-management-remove': {
    contentNodeId: '2910:6884',
    sheetInstanceNodeId: '2910:6886',
    cta: 'on',
    frameKey: 'resolvingTask',
    height: 510,
    tab: 'active',
    top: 342,
    text: 'Needs attention Remove what you didn’t add Anything you didn’t set up could let someone see what you do online or control your phone. If you don’t recognize it, it’s safest to remove it. 1 Tap the item you don’t recognize 2 Tap Remove or Delete 3 Confirm with your passcode Open settings It says my phone is “managed”',
  },
  'review-device-management-escalate': {
    contentNodeId: '2910:7005',
    sheetInstanceNodeId: '2910:7007',
    cta: 'on',
    frameKey: 'resolvingTask',
    height: 446,
    tab: 'active',
    top: 406,
    text: 'Needs attention Someone else may control your phone Your phone says it’s “managed” by someone but you didn’t set that up. That can give them access to your phone. Apple can help you check and remove it. Contact Apple Support This is my work or school phone',
  },
  'review-device-management-work': {
    contentNodeId: '2910:7130',
    sheetInstanceNodeId: '2916:33924',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 348,
    tab: 'resolved',
    top: 504,
    text: 'Resolved Phone settings set by your work or school Your employer or school set these up, that’s expected. Nothing to remove, and we won’t flag it again. Resolved May 15, 2026',
  },
  'review-device-management-resolved': {
    contentNodeId: '2910:7482',
    sheetInstanceNodeId: '2910:7484',
    cta: 'off',
    frameKey: 'taskDetailResolved',
    height: 320,
    tab: 'resolved',
    top: 532,
    text: 'Resolved No unfamiliar phone settings You checked your phone settings and everything looked like something you set up yourself. Resolved May 15, 2026',
  },
};

function taskVisualOverlay(initial: ProtectTaskVisualState, model: ProtectSurfaceModel, onDismiss: () => void): ProtectVisualTodoOverlay {
  const sheet = TASK_VISUAL_SHEETS[initial];
  const taskSpec = TASK_VISUAL_SPECS[initial];
  const task = model.selectedTask;
  // The frozen screen stores task copy as one aggregate text run, but reusable
  // nested owners must still come from the same semantic content builder as
  // the live task scene. This keeps Illustration16x9 tied to the task kind and
  // resolved state rather than to a Storybook screenshot ID.
  const taskContent = buildTaskSheetContent(
    taskTypeToSheetKind(taskSpec.taskType),
    task ? taskSheetFinding(task, model) : {},
    {
      resolved: taskSpec.resolved,
      resolvedAt: taskSpec.resolved ? RESOLVED_AT : undefined,
      variant: taskSpec.contentVariant,
    },
  );

  return {
    kind: 'todoOverlay',
    blankCanvas: true,
    contentNodeId: sheet.contentNodeId,
    frameKey: sheet.frameKey,
    tab: sheet.tab,
    overlay: (
      <FigmaBottomSheet
        hostId="protect.visual-task-resolution"
        cta={sheet.cta}
        contentVariant="task"
        structureInstanceId={sheet.sheetInstanceNodeId}
        taskContent={taskContent}
        // Pin body TEXT frame (e.g. browsing-protection 66) — glyph-hug under-occupies.
        bodyMinHeight={taskContent.descriptionFrameHeight ?? undefined}
        taskContentTopInset={sheet.taskContentTopInset}
        text={sheet.text}
        // T-452 (owner: «i dont see the video»): the browsing-protection sheet
        // carries the walkthrough video card in Figma; the visual host mounts
        // the same code-owned media as the live sheet (visual capture keeps it
        // paused on the bundled poster, so frames stay deterministic).
        projectedMedia={initial === 'browsing-protection' ? <ExtensionSetupVideo /> : undefined}
        onDismissGesture={onDismiss}
        style={[styles.visualSheet, { height: sheet.height, top: sheet.top }]}
      />
    ),
  };
}

/**
 * Visual-route host for the live Protect task-specific sheets.
 *
 * It renders the real `ProtectSurface` inside the real `MainAppShell` and seeds a
 * local runtime task matching the selected Figma task frame. That keeps the
 * Storybook/Figma transfer tied to the same task-detail, resolution, guided
 * answer, risk-drop, and resolved views used by the app instead of projected
 * static shells.
 */
function startState(initial: ProtectTaskVisualState): ProtectRuntimeMachineState {
  const taskSpec = TASK_VISUAL_SPECS[initial];
  const taskStatus: ProtectRuntimeTask['status'] = taskSpec.monitoring ? 'monitoring' : taskSpec.resolved ? 'resolved' : 'open';
  const task: ProtectRuntimeTask = {
    id: taskSpec.id,
    category: taskSpec.category,
    level: taskSpec.level,
    status: taskStatus,
    kind: taskSpec.kind,
    taskType: taskSpec.taskType,
    title: taskSpec.title,
    description: taskSpec.description,
    source: taskSpec.source,
    finding: taskSpec.finding,
    details: taskSpec.details,
    createdAt: NOW,
    opened: false,
  };
  const baseRuntime = createProtectRuntimeState({
    canonicalRiskStatus: PROTECT_TASK_VISUAL_CANONICAL_RISK_STATUS,
    tasks: [task],
    browsing: { protection: taskSpec.browsingProtection ?? 'on', checkedLinksToday: 128, blockedPagesToday: taskSpec.browsingProtection === 'off' ? 0 : 3 },
  });
  const runtime = markProtectTaskOpened(baseRuntime, task.id);
  return createProtectRuntimeMachineState(runtime, { location: taskSpec.location });
}

function reduce(state: ProtectRuntimeMachineState, event: MobileAppEvent): ProtectRuntimeMachineState {
  switch (event.type) {
    case 'PROTECT_SEE_FIX': {
      if (state.location.name !== 'taskDetail') return state;
      const selectedTask = state.runtime.selectedTaskId
        ? state.runtime.tasks.find((task) => task.id === state.runtime.selectedTaskId)
        : null;
      const screen = selectedTask?.taskType === 'review_device_management' ? 'guidedAnswer' : 'resolving';
      return { ...state, location: { name: 'taskResolution', screen } };
    }
    case 'PROTECT_CONFIRM_RESOLVED':
    case 'PROTECT_TASK_CONFIRMED':
    case 'PROTECT_TASK_RECHECK': {
      const taskId = state.runtime.selectedTaskId;
      if (!taskId) return state;
      const runtime = resolveProtectTask(state.runtime, taskId);
      return { ...state, runtime, location: { name: 'taskResolution', screen: 'riskDropped' } };
    }
    case 'PROTECT_TASK_GUIDED_ANSWER': {
      if (state.location.name !== 'taskResolution' || state.location.screen !== 'guidedAnswer') return state;
      const taskId = state.runtime.selectedTaskId;
      if (!taskId) return state;
      if (event.branch === 'familiar' || event.branch === 'workSchool') {
        const runtime = resolveProtectTask(state.runtime, taskId);
        return { ...state, runtime, location: { name: 'taskResolution', screen: 'riskDropped' } };
      }
      return { ...state, location: { name: 'taskResolution', screen: 'resolving' } };
    }
    case 'PROTECT_REMOVE_TASK': {
      const taskId = state.runtime.selectedTaskId;
      if (!taskId) return state;
      const runtime = removeProtectTask(state.runtime, taskId);
      return { ...state, runtime, location: { name: 'taskResolution', screen: 'removed' } };
    }
    case 'PROTECT_BACK': {
      const leavingRiskDropped = state.location.name === 'taskResolution'
        && state.location.screen === 'riskDropped';
      return {
        ...state,
        location: protectMainLocation(state.runtime),
        runtime: leavingRiskDropped
          ? { ...state.runtime, provenRiskDrop: null }
          : state.runtime,
      };
    }
    case 'PROTECT_TOAST_ELAPSED': {
      const runtime = clearProtectToast(state.runtime);
      if (state.location.name === 'taskResolution' && state.location.screen === 'riskDropped') {
        return {
          ...state,
          runtime: { ...runtime, provenRiskDrop: null },
          location: { name: 'taskResolution', screen: 'resolved' },
        };
      }
      if (
        state.location.name === 'taskResolution' &&
        (state.location.screen === 'resolved' || state.location.screen === 'removed')
      ) {
        return { ...state, runtime, location: protectTodoLocation(runtime) };
      }
      return { ...state, runtime };
    }
    default:
      return state;
  }
}

export function ProtectTaskResolutionVisualHost({ initial = 'change-password' }: { initial?: ProtectTaskVisualState }) {
  const [machineState, send] = useReducer(reduce, initial, startState);
  const dispatch = useCallback((event: MobileAppEvent) => send(event), []);

  const toast = machineState.runtime.toast;
  const elapsed = useRef(dispatch);
  elapsed.current = dispatch;
  useEffect(() => {
    if (!toast) return undefined;
    const handle = setTimeout(() => elapsed.current({ type: 'PROTECT_TOAST_ELAPSED' }), 2200);
    return () => clearTimeout(handle);
  }, [toast]);

  const model = resolveProtectSurfaceModel(machineState, PROFILE_EMAIL, visualTrainProductSummary(VISUAL_TRAIN_STATE));
  return (
    <MainAppShell
      surfaceKey={`protect:task:${initial}`}
      activeTab="protect"
      onSelectTab={() => undefined}
      tabs={['protect', 'train', 'profile']}
      showTabBar={false}
    >
      <View style={styles.screenRoot} testID={`screen-protect-task-${initial}`}>
        <ProtectSurface
          machineState={machineState}
          send={dispatch}
          profileEmail={PROFILE_EMAIL}
          trainState={visualTrainProductSummary(VISUAL_TRAIN_STATE)}
          visualTodoOverlay={taskVisualOverlay(initial, model, () => dispatch({ type: 'PROTECT_BACK' }))}
        />
      </View>
    </MainAppShell>
  );
}

const styles = StyleSheet.create({
  screenRoot: {
    flex: 1,
  },
  visualSheet: {
    left: 0,
    position: 'absolute',
  },
});
