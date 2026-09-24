import { Effect } from 'effect';
import {
  ProviderJobCommand,
  ProviderJobEvent,
  ProviderJobRejection,
  ProviderJobState,
  providerJobSkipEventId,
  providerJobSkipFingerprint,
  providerJobSkipIdempotencyKey,
} from '@onyx/scan-engine';
import { step, type StepResult } from '@onyx/state-machine-runtime';
import { ProviderJobRuntime } from './providerJobMachineRuntime';

export const stepProviderDisabledSkip = (input: {
  readonly providerJobId: string;
  readonly revision: number;
}) =>
  Effect.gen(function* () {
    const runtime = yield* ProviderJobRuntime;
    const reason = 'provider_disabled' as const;
    const eventId = providerJobSkipEventId(input.providerJobId, reason, input.revision);
    const result: StepResult<ProviderJobState, ProviderJobCommand, ProviderJobRejection> = yield* step(
      runtime.machine,
      runtime.repository,
      {
        instanceId: input.providerJobId,
        eventId,
        idempotencyKey: providerJobSkipIdempotencyKey(input.providerJobId, reason, input.revision),
        fingerprint: providerJobSkipFingerprint({ providerJobId: input.providerJobId, reason, revision: input.revision }),
        event: ProviderJobEvent.SkipRequested({ eventId, reason }),
      },
    );
    return { result, eventId };
  });
