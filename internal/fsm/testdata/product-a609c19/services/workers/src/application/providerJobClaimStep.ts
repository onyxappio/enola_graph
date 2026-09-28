import { Effect } from 'effect';
import {
  ProviderJobCommand,
  ProviderJobEvent,
  ProviderJobRejection,
  ProviderJobState,
  providerJobClaimCommandId,
  providerJobClaimFingerprint,
  providerJobClaimIdempotencyKey,
} from '@onyx/scan-engine';
import { step, type StepResult } from '@onyx/state-machine-runtime';
import { ProviderJobRuntime } from './providerJobMachineRuntime';
import type { ProviderJobRow } from './services';
import { ScanEventPublisher, ScanJobRepository } from './services';
import type { ProviderJobSkipReason } from '@onyx/contracts';

export type ClaimHealOutcome =
  | { kind: 'job_not_found' }
  | { kind: 'claim_conflict' }
  | { kind: 'skipped'; skipReason: ProviderJobSkipReason }
  | { kind: 'failed_final'; reason: string };

export type ClaimIdentity = {
  commandId: string;
  idempotencyKey: string;
  fingerprint: string;
};

export const claimIdentityFor = (providerJobId: string, eventId: string): ClaimIdentity => ({
  commandId: providerJobClaimCommandId(providerJobId),
  idempotencyKey: providerJobClaimIdempotencyKey(providerJobId, eventId),
  fingerprint: providerJobClaimFingerprint({ providerJobId, eventId }),
});

export const stepClaimRequested = (input: {
  providerJobId: string;
  eventId: string;
  at: number;
  lockedUntil: number;
  executionEpochHint: number;
}) =>
  Effect.gen(function* () {
    const runtime = yield* ProviderJobRuntime;
    const identity = claimIdentityFor(input.providerJobId, input.eventId);
    const request = {
      instanceId: input.providerJobId,
      eventId: input.eventId,
      idempotencyKey: identity.idempotencyKey,
      fingerprint: identity.fingerprint,
      event: ProviderJobEvent.ClaimRequested({
        eventId: input.eventId,
        at: input.at,
        commandId: identity.commandId,
        executionId: input.eventId,
        executionEpoch: input.executionEpochHint,
        lockedUntil: input.lockedUntil,
      }),
    };
    const result: StepResult<ProviderJobState, ProviderJobCommand, ProviderJobRejection> = yield* step(
      runtime.machine,
      runtime.repository,
      request,
    );
    return { result, identity };
  });

export const acknowledgeClaimWithoutExecute = (
  job: ProviderJobRow,
  publisher: {
    publishAggregationRequested: (scanRunId: string) => Effect.Effect<void>;
  },
): Effect.Effect<ClaimHealOutcome> =>
  Effect.gen(function* () {
    if (job.state === 'completed') {
      yield* publisher.publishAggregationRequested(job.scanRunId);
      return { kind: 'claim_conflict' } as const;
    }
    if (job.state === 'failed') {
      yield* publisher.publishAggregationRequested(job.scanRunId);
      return { kind: 'failed_final', reason: job.failureReason ?? 'failed' } as const;
    }
    if (job.state === 'skipped') {
      if (job.skipReason == null) return { kind: 'claim_conflict' } as const;
      yield* publisher.publishAggregationRequested(job.scanRunId);
      return { kind: 'skipped', skipReason: job.skipReason } as const;
    }
    return { kind: 'claim_conflict' } as const;
  });

export const healClaimWithoutExecute = (providerJobId: string) =>
  Effect.gen(function* () {
    const repository = yield* ScanJobRepository;
    const publisher = yield* ScanEventPublisher;
    const current = yield* repository.loadProviderJob(providerJobId);
    if (!current) return { kind: 'job_not_found' } as const;
    return yield* acknowledgeClaimWithoutExecute(current, publisher);
  });
