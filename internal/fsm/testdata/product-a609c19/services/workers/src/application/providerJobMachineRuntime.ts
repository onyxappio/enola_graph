import { randomUUID } from 'node:crypto';
import { Context, Effect, Layer } from 'effect';
import {
  providerJobRuntimeMachine,
  type ProviderJobCommand,
  type ProviderJobEvent,
  type ProviderJobMachineRepository,
  type ProviderJobRejection,
  type ProviderJobState,
} from '@onyx/scan-engine';
import {
  IdGenerator,
  RuntimeClock,
  TechnicalFailure,
  type RuntimeMachine,
} from '@onyx/state-machine-runtime';
import { WorkerClock } from './services';

export { providerJobRuntimeMachine };

export class ProviderJobRuntime extends Context.Tag('workers/ProviderJobRuntime')<
  ProviderJobRuntime,
  {
    machine: RuntimeMachine<ProviderJobState, ProviderJobEvent, ProviderJobCommand, ProviderJobRejection>;
    repository: ProviderJobMachineRepository;
  }
>() {}

export const uuidIdGeneratorLayer: Layer.Layer<IdGenerator> = Layer.succeed(IdGenerator, {
  next: Effect.sync(() => randomUUID()),
});

export const workerRuntimeClockLayer: Layer.Layer<RuntimeClock, never, WorkerClock> = Layer.effect(
  RuntimeClock,
  Effect.gen(function* () {
    const clock = yield* WorkerClock;
    return { now: Effect.sync(() => clock.now().getTime()) };
  }),
);

export function createProviderJobRuntimeLayer(
  repository: ProviderJobMachineRepository,
): Layer.Layer<ProviderJobRuntime> {
  return Layer.succeed(ProviderJobRuntime, {
    machine: providerJobRuntimeMachine,
    repository,
  });
}

export const unusedProviderJobRuntimeLayer: Layer.Layer<ProviderJobRuntime> = Layer.succeed(ProviderJobRuntime, {
  machine: providerJobRuntimeMachine,
  repository: {
    load: () => Effect.fail(new TechnicalFailure({ cause: 'provider-job runtime unused while claim step is off' })),
    lookupReceipt: () => Effect.fail(new TechnicalFailure({ cause: 'provider-job runtime unused while claim step is off' })),
    commit: () => Effect.fail(new TechnicalFailure({ cause: 'provider-job runtime unused while claim step is off' })),
    reconcile: () => Effect.fail(new TechnicalFailure({ cause: 'provider-job runtime unused while claim step is off' })),
  },
});
