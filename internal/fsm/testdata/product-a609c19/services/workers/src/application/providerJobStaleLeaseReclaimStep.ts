import { Effect } from 'effect';
import {
  ProviderJobCommand,
  ProviderJobEvent,
  ProviderJobRejection,
  ProviderJobState,
  providerJobStaleReclaimEventId,
  providerJobStaleReclaimFingerprint,
  providerJobStaleReclaimIdempotencyKey,
} from '@onyx/scan-engine';
import { step, type StepResult } from '@onyx/state-machine-runtime';
import { ProviderJobRuntime } from './providerJobMachineRuntime';

export const stepProviderStaleLeaseReclaim = (input: {
  readonly providerJobId: string;
  readonly revision: number;
  readonly at: number;
  readonly staleStartedBefore: number;
  readonly failureReason: string;
}) =>
  Effect.gen(function* () {
    const runtime = yield* ProviderJobRuntime;
    const eventId = providerJobStaleReclaimEventId(input);
    const result: StepResult<ProviderJobState, ProviderJobCommand, ProviderJobRejection> = yield* step(
      runtime.machine,
      runtime.repository,
      {
        instanceId: input.providerJobId,
        eventId,
        idempotencyKey: providerJobStaleReclaimIdempotencyKey(input),
        fingerprint: providerJobStaleReclaimFingerprint(input),
        event: ProviderJobEvent.StaleLeaseReclaimRequested({
          eventId,
          at: input.at,
          staleStartedBefore: input.staleStartedBefore,
          failureReason: input.failureReason,
        }),
      },
    );
    return { result, eventId };
  });
