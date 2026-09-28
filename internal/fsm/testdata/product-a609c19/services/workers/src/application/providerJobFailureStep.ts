import { Effect } from 'effect';
import {
  ProviderJobCommand,
  ProviderJobEvent,
  ProviderJobRejection,
  ProviderJobState,
  providerJobOutcomeEventId,
  providerJobOutcomeFingerprint,
  providerJobOutcomeIdempotencyKey,
} from '@onyx/scan-engine';
import { step, type StepResult } from '@onyx/state-machine-runtime';
import { ProviderJobRuntime } from './providerJobMachineRuntime';

export type ProviderJobFailureIdentity = {
  readonly eventId: string;
  readonly idempotencyKey: string;
  readonly fingerprint: string;
};

export const failureIdentityFor = (input: {
  readonly providerJobId: string;
  readonly commandId: string;
  readonly executionId: string;
  readonly executionEpoch: number;
}): ProviderJobFailureIdentity => {
  const identity = {
    providerJobId: input.providerJobId,
    eventTag: 'ProviderFailed' as const,
    commandId: input.commandId,
    executionId: input.executionId,
    executionEpoch: input.executionEpoch,
  };
  return {
    eventId: providerJobOutcomeEventId(identity),
    idempotencyKey: providerJobOutcomeIdempotencyKey(identity),
    fingerprint: providerJobOutcomeFingerprint(identity),
  };
};

export const stepProviderFailed = (input: {
  readonly providerJobId: string;
  readonly reason: string;
  readonly retryable: boolean;
  readonly nextAttemptAt: number | null;
  readonly commandId: string;
  readonly executionId: string;
  readonly executionEpoch: number;
  readonly issuedAtRevision: number;
}) =>
  Effect.gen(function*() {
    const runtime = yield* ProviderJobRuntime;
    const identity = failureIdentityFor(input);
    const event = ProviderJobEvent.ProviderFailed({
      eventId: identity.eventId,
      reason: input.reason,
      retryable: input.retryable,
      nextAttemptAt: input.nextAttemptAt,
      commandId: input.commandId,
      executionId: input.executionId,
      executionEpoch: input.executionEpoch,
      issuedAtRevision: input.issuedAtRevision,
    });
    const result: StepResult<ProviderJobState, ProviderJobCommand, ProviderJobRejection> = yield* step(
      runtime.machine,
      runtime.repository,
      {
        instanceId: input.providerJobId,
        eventId: identity.eventId,
        idempotencyKey: identity.idempotencyKey,
        fingerprint: identity.fingerprint,
        event,
        commandOutcome: {
          commandId: input.commandId,
          executionId: input.executionId,
          executionEpoch: input.executionEpoch,
        },
      },
    );
    return { result, identity };
  });
