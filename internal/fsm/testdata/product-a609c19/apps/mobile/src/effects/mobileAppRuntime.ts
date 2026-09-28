import { Effect } from 'effect';

import {
  buildMachineTransitionRecord,
  buildRejectedRecord,
  isSelfReferentialAnalyticsEffect,
  type MachineTransitionRecord,
} from '../analytics/machineEventTracking';
import { setProductionAnalyticsSuppressed } from '../analytics/openCorrelation';
import { installAppsFlyerOpenTelemetryFromRuntime } from '../appsflyerOpenTelemetry';
import type { MobileAppEvent, MobileAppInput } from '../state/mobileAppMachine.types';
import {
  createInitialMobileAppSnapshot,
  sendMobileAppEvent,
  settleMobileAppEffect,
  startMobileAppInterpreter,
  type MobileAppEffectRequest,
  type MobileAppSnapshot,
} from '../behavior/mobileAppInterpreter';
import { runMobileAppEffect, type MobileAppServices } from './mobileAppServices';

export type MobileAppRuntime = {
  start: () => void;
  send: (event: MobileAppEvent) => void;
  getSnapshot: () => MobileAppSnapshot;
  subscribe: (listener: () => void) => () => void;
  /** Disposes generation-owned open-telemetry install for this runtime. */
  dispose: () => void;
};

/** Optional observer for production-composed runtime evidence. */
export type MobileAppRuntimeEventObserver = (
  event: MobileAppEvent,
  snapshot: MobileAppSnapshot,
) => void;

export type CreateMobileAppRuntimeOptions = {
  input: MobileAppInput;
  services: MobileAppServices;
  eventObserver?: MobileAppRuntimeEventObserver;
  onMachineTransition?: (record: MachineTransitionRecord) => void;
};

function asMachineTrigger(value: object): { type?: string } & Record<string, unknown> {
  return value as { type?: string } & Record<string, unknown>;
}

/** Previous runtime install disposer — remount replaces without App.tsx edits. */
let previousOpenTelemetryInstallDisposer: (() => void) | null = null;

export function createMobileAppRuntime({
  input,
  services,
  eventObserver,
  onMachineTransition,
}: CreateMobileAppRuntimeOptions): MobileAppRuntime {
  // Nonvisual seams: visual suppress + AppsFlyer open telemetry (keeps App.tsx
  // origin/main-identical for Gates freshness). Must be ESM imports — the
  // browser journey harness defines `require: '(path) => path'`.
  setProductionAnalyticsSuppressed(Boolean(input.visualMode));
  previousOpenTelemetryInstallDisposer?.();
  const disposeOpenTelemetryInstall = installAppsFlyerOpenTelemetryFromRuntime({
    visualMode: Boolean(input.visualMode),
    exchangeAvailable: Boolean(services.exchangeAppsFlyerAutologin),
  });
  previousOpenTelemetryInstallDisposer = disposeOpenTelemetryInstall;

  let currentSnapshot = createInitialMobileAppSnapshot(input);
  let started = false;
  const listeners = new Set<() => void>();

  function notify() {
    for (const listener of listeners) listener();
  }

  function applySnapshot(snapshot: MobileAppSnapshot) {
    currentSnapshot = snapshot;
    notify();
  }

  function emitMachineTransition(build: () => MachineTransitionRecord) {
    if (!onMachineTransition) return;
    try {
      onMachineTransition(build());
    } catch {
      // Machine-event tracking must never break the runtime.
    }
  }

  function runEffects(effects: MobileAppEffectRequest[]) {
    for (const effect of effects) {
      void Effect.runPromise(runMobileAppEffect(services, effect)).then((result) => {
        const before = currentSnapshot;
        const transition = settleMobileAppEffect(currentSnapshot, result);
        if (!transition.accepted) return;
        applySnapshot(transition.snapshot);
        if (!isSelfReferentialAnalyticsEffect(result.type)) {
          emitMachineTransition(() => buildMachineTransitionRecord(
            'effect-result',
            asMachineTrigger(result),
            before,
            transition.snapshot,
          ));
        }
        runEffects(transition.effects);
      });
    }
  }

  return {
    start() {
      if (started) return;
      started = true;
      const transition = startMobileAppInterpreter(currentSnapshot);
      applySnapshot(transition.snapshot);
      emitMachineTransition(() => buildMachineTransitionRecord(
        'start',
        { type: '$app.start' },
        null,
        transition.snapshot,
      ));
      runEffects(transition.effects);
    },
    send(event) {
      const before = currentSnapshot;
      const transition = sendMobileAppEvent(currentSnapshot, event);
      if (!transition.accepted) {
        emitMachineTransition(() => buildRejectedRecord(asMachineTrigger(event), before));
        return;
      }
      applySnapshot(transition.snapshot);
      const ignored = transition.trace.some((entry) => entry.startsWith('rejected:'));
      // Interpreter `rejected()` keeps accepted:true with a `rejected:` trace.
      // Surface those as accepted:false machine records so ignored injections
      // are not counted as transitions.
      emitMachineTransition(() => (
        ignored
          ? buildRejectedRecord(asMachineTrigger(event), before)
          : buildMachineTransitionRecord(
            'event',
            asMachineTrigger(event),
            before,
            transition.snapshot,
          )
      ));
      if (!ignored) {
        eventObserver?.(event, transition.snapshot);
      }
      runEffects(transition.effects);
    },
    getSnapshot() {
      return currentSnapshot;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    dispose() {
      disposeOpenTelemetryInstall();
      if (previousOpenTelemetryInstallDisposer === disposeOpenTelemetryInstall) {
        previousOpenTelemetryInstallDisposer = null;
      }
    },
  };
}

export function waitForMobileAppSnapshot(
  runtime: Pick<MobileAppRuntime, 'getSnapshot' | 'subscribe'>,
  predicate: (snapshot: MobileAppSnapshot) => boolean,
  options: { timeout?: number } = {},
): Promise<MobileAppSnapshot> {
  const current = runtime.getSnapshot();
  if (predicate(current)) return Promise.resolve(current);

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      unsubscribe();
      reject(new Error('Timed out waiting for mobile app runtime snapshot.'));
    }, options.timeout ?? 1_000);

    const unsubscribe = runtime.subscribe(() => {
      const snapshot = runtime.getSnapshot();
      if (!predicate(snapshot)) return;
      clearTimeout(timeout);
      unsubscribe();
      resolve(snapshot);
    });
  });
}
