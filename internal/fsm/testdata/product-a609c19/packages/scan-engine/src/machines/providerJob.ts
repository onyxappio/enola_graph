import {
  checkMachineInvariants,
  defineMachine,
  type CommandHandlerRegistration,
  type Decision as MachineDecision,
  type MachineRegistration,
  type TransitionRule,
} from '@onyx/state-machine-kernel';
import type { ProviderJobSkipReason } from '@onyx/contracts';
import { Data, Match } from 'effect';
import {
  SCAN_AGGREGATE_PRIVACY_POLICY,
  assertClosedMetadataPrivacyPolicy,
  type ClosedMetadataPrivacyPolicy,
} from './privacyPolicy';

export type ProviderJobState = Data.TaggedEnum<{
  Queued: {
    readonly jobId: string;
    readonly scanRunId: string;
    readonly provider: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly attemptCount: number;
    readonly maxAttempts: number;
    readonly nextAttemptAt: number | null;
    readonly lastFailureReason: string | null;
  };
  Running: {
    readonly jobId: string;
    readonly scanRunId: string;
    readonly provider: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly attemptCount: number;
    readonly maxAttempts: number;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly expectedFence: number;
    readonly lockedUntil: number | null;
    readonly startedAt: number;
  };
  Completed: {
    readonly jobId: string;
    readonly scanRunId: string;
    readonly provider: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly resultRef: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
  };
  Failed: {
    readonly jobId: string;
    readonly scanRunId: string;
    readonly provider: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly attemptCount: number;
    readonly maxAttempts: number;
    readonly failureReason: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
  };
  Skipped: {
    readonly jobId: string;
    readonly scanRunId: string;
    readonly provider: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly skipReason: ProviderJobSkipReason;
  };
}>;

export const ProviderJobState = Data.taggedEnum<ProviderJobState>();

export type ProviderJobEvent = Data.TaggedEnum<{
  ClaimRequested: {
    readonly eventId: string;
    readonly at: number;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly lockedUntil: number;
  };
  SkipRequested: {
    readonly eventId: string;
    readonly reason: ProviderJobSkipReason;
  };
  ProviderSucceeded: {
    readonly eventId: string;
    readonly resultRef: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  ProviderFailed: {
    readonly eventId: string;
    readonly reason: string;
    readonly retryable: boolean;
    readonly nextAttemptAt: number | null;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  StaleLeaseReclaimRequested: {
    readonly eventId: string;
    readonly at: number;
    readonly staleStartedBefore: number;
    readonly failureReason: string;
  };
  AdminRetryRequested: {
    readonly eventId: string;
    readonly actorClass: string;
    readonly actorId: string;
  };
  AggregationAck: { readonly eventId: string };
  ScanRunReopened: { readonly eventId: string };
}>;

export const ProviderJobEvent = Data.taggedEnum<ProviderJobEvent>();

export type ProviderJobCommand = Data.TaggedEnum<{
  CallProvider: {
    readonly jobId: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  PublishAggregation: { readonly scanRunId: string };
  ScheduleRetry: { readonly jobId: string; readonly nextAttemptAt: number };
  DispatchJob: { readonly jobId: string };
  ReopenScanRun: { readonly scanRunId: string; readonly jobId: string; readonly actorId: string };
}>;

export const ProviderJobCommand = Data.taggedEnum<ProviderJobCommand>();

export type ProviderJobRejection = Data.TaggedEnum<{
  EventNotAllowed: {
    readonly state: ProviderJobState['_tag'];
    readonly event: ProviderJobEvent['_tag'];
  };
  ClaimNotDue: { readonly nextAttemptAt: number; readonly at: number };
  ClaimConflict: { readonly state: ProviderJobState['_tag'] };
  StaleCommandOutcome: {
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
  };
  StaleLeaseNotExpired: { readonly lockedUntil: number | null; readonly at: number };
  NotRetryable: { readonly state: ProviderJobState['_tag'] };
  AdminCapabilityMissing: { readonly actorClass: string };
  InvalidPayload: { readonly event: ProviderJobEvent['_tag']; readonly field: string };
}>;

export const ProviderJobRejection = Data.taggedEnum<ProviderJobRejection>();

export const PROVIDER_JOB_MACHINE_VERSION = 1 as const;

export type ProviderJobDecision = MachineDecision<ProviderJobState, ProviderJobCommand, ProviderJobRejection>;

type OutcomeEvent = Extract<ProviderJobEvent, { _tag: 'ProviderSucceeded' | 'ProviderFailed' }>;

const isCurrentAttempt = (
  snapshot: ProviderJobState,
  event: OutcomeEvent,
  fenceWithAttemptCount = false,
): snapshot is Extract<ProviderJobState, { _tag: 'Running' }> =>
  snapshot._tag === 'Running' &&
  snapshot.commandId === event.commandId &&
  snapshot.executionId === event.executionId &&
  snapshot.expectedFence === event.issuedAtRevision &&
  (fenceWithAttemptCount ? snapshot.attemptCount === event.executionEpoch : snapshot.executionEpoch === event.executionEpoch);

const isDue = (snapshot: Extract<ProviderJobState, { _tag: 'Queued' }>, at: number): boolean =>
  snapshot.nextAttemptAt === null || snapshot.nextAttemptAt <= at;

const isStaleLease = (
  snapshot: Extract<ProviderJobState, { _tag: 'Running' }>,
  event: Extract<ProviderJobEvent, { _tag: 'StaleLeaseReclaimRequested' }>,
): boolean =>
  snapshot.lockedUntil === null
    ? snapshot.startedAt <= event.staleStartedBefore
    : snapshot.lockedUntil <= event.at;

const isNonEmpty = (value: string): boolean => value.trim().length > 0;
const isTimestamp = (value: number): boolean => Number.isFinite(value) && value >= 0;
const isPositiveInteger = (value: number): boolean => Number.isInteger(value) && value > 0;
const isNonNegativeInteger = (value: number): boolean => Number.isInteger(value) && value >= 0;
const skipReasons = new Set<ProviderJobSkipReason>([
  'unsupported_subject_type',
  'provider_disabled',
  'not_in_coverage_profile',
]);

const providerJobPayloadIssue = (event: ProviderJobEvent): string | null =>
  Match.value(event).pipe(
    Match.tag('ClaimRequested', (claim) => {
      if (!isNonEmpty(claim.eventId)) return 'eventId';
      if (!isTimestamp(claim.at)) return 'at';
      if (!isNonEmpty(claim.commandId)) return 'commandId';
      if (!isNonEmpty(claim.executionId)) return 'executionId';
      if (!isPositiveInteger(claim.executionEpoch)) return 'executionEpoch';
      if (!isTimestamp(claim.lockedUntil) || claim.lockedUntil <= claim.at) return 'lockedUntil';
      return null;
    }),
    Match.tag('SkipRequested', (skip) => {
      if (!isNonEmpty(skip.eventId)) return 'eventId';
      if (!skipReasons.has(skip.reason)) return 'reason';
      return null;
    }),
    Match.tag('ProviderSucceeded', (outcome) => {
      if (!isNonEmpty(outcome.eventId)) return 'eventId';
      if (!isNonEmpty(outcome.resultRef)) return 'resultRef';
      if (!isNonEmpty(outcome.commandId)) return 'commandId';
      if (!isNonEmpty(outcome.executionId)) return 'executionId';
      if (!isPositiveInteger(outcome.executionEpoch)) return 'executionEpoch';
      if (!isNonNegativeInteger(outcome.issuedAtRevision)) return 'issuedAtRevision';
      return null;
    }),
    Match.tag('ProviderFailed', (outcome) => {
      if (!isNonEmpty(outcome.eventId)) return 'eventId';
      if (!isNonEmpty(outcome.reason)) return 'reason';
      if (!isNonEmpty(outcome.commandId)) return 'commandId';
      if (!isNonEmpty(outcome.executionId)) return 'executionId';
      if (!isPositiveInteger(outcome.executionEpoch)) return 'executionEpoch';
      if (!isNonNegativeInteger(outcome.issuedAtRevision)) return 'issuedAtRevision';
      if (outcome.nextAttemptAt !== null && !isTimestamp(outcome.nextAttemptAt)) return 'nextAttemptAt';
      return null;
    }),
    Match.tag('StaleLeaseReclaimRequested', (reclaim) => {
      if (!isNonEmpty(reclaim.eventId)) return 'eventId';
      if (!isTimestamp(reclaim.at)) return 'at';
      if (!isTimestamp(reclaim.staleStartedBefore)) return 'staleStartedBefore';
      if (!isNonEmpty(reclaim.failureReason)) return 'failureReason';
      return null;
    }),
    Match.tag('AdminRetryRequested', (admin) => {
      if (!isNonEmpty(admin.eventId)) return 'eventId';
      if (!isNonEmpty(admin.actorClass)) return 'actorClass';
      if (!isNonEmpty(admin.actorId)) return 'actorId';
      return null;
    }),
    Match.tag('AggregationAck', 'ScanRunReopened', (ack) => (!isNonEmpty(ack.eventId) ? 'eventId' : null)),
    Match.exhaustive,
  );

const hasValidPayload = (_snapshot: ProviderJobState, event: ProviderJobEvent): boolean =>
  providerJobPayloadIssue(event) === null;

const identity = {
  jobId: 'job_1',
  scanRunId: 'run_1',
  provider: 'hibp',
} as const;

const initialQueued = (): Extract<ProviderJobState, { _tag: 'Queued' }> =>
  ProviderJobState.Queued({
    ...identity,
    revision: 0,
    machineVersion: PROVIDER_JOB_MACHINE_VERSION,
    attemptCount: 0,
    maxAttempts: 2,
    nextAttemptAt: null,
    lastFailureReason: null,
  });

export type ProviderJobOverrides = {
  readonly unguardRuleIds?: ReadonlySet<string>;
  readonly fenceWithAttemptCount?: boolean;
  readonly allowAdminRetryFromCompleted?: boolean;
};

const buildRules = (
  overrides: ProviderJobOverrides = {},
): readonly TransitionRule<ProviderJobState, ProviderJobEvent, ProviderJobCommand, ProviderJobRejection>[] => {
  const fenceWithAttemptCount = overrides.fenceWithAttemptCount === true;
  const rules: TransitionRule<ProviderJobState, ProviderJobEvent, ProviderJobCommand, ProviderJobRejection>[] = [
    {
      id: 'queued-claim',
      from: 'Queued',
      on: 'ClaimRequested',
      to: 'Running',
      guardIds: ['payload-valid', 'due-fence'],
      actorClass: 'worker',
      guard: (snapshot, event) =>
        hasValidPayload(snapshot, event) &&
        snapshot._tag === 'Queued' &&
        event._tag === 'ClaimRequested' &&
        isDue(snapshot, event.at),
      guardRejection: (snapshot, event) => {
        const field = providerJobPayloadIssue(event);
        return field
          ? ProviderJobRejection.InvalidPayload({ event: event._tag, field })
          : ProviderJobRejection.ClaimNotDue({
              nextAttemptAt: snapshot._tag === 'Queued' ? snapshot.nextAttemptAt ?? 0 : 0,
              at: event._tag === 'ClaimRequested' ? event.at : 0,
            });
      },
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Queued' || event._tag !== 'ClaimRequested') {
          return { next: snapshot, commands: [] };
        }
        const revision = snapshot.revision + 1;
        // Local CAS consistency only: this iteration's snapshot attemptCount + 1.
        // Admin retry resets attemptCount to 0, so this is not a global epoch.
        const assignedEpoch = snapshot.attemptCount + 1;
        return {
          next: ProviderJobState.Running({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision,
            machineVersion: snapshot.machineVersion,
            attemptCount: assignedEpoch,
            maxAttempts: snapshot.maxAttempts,
            commandId: event.commandId,
            executionId: event.executionId,
            executionEpoch: assignedEpoch,
            expectedFence: revision,
            lockedUntil: event.lockedUntil,
            startedAt: event.at,
          }),
          commands: [
            ProviderJobCommand.CallProvider({
              jobId: snapshot.jobId,
              commandId: event.commandId,
              executionId: event.executionId,
              executionEpoch: assignedEpoch,
              issuedAtRevision: revision,
            }),
          ],
        };
      },
    },
    {
      id: 'queued-skip',
      from: 'Queued',
      on: 'SkipRequested',
      to: 'Skipped',
      guardIds: ['payload-valid'],
      actorClass: 'system',
      guard: hasValidPayload,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Queued' || event._tag !== 'SkipRequested') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Skipped({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            skipReason: event.reason,
          }),
          commands: [ProviderJobCommand.PublishAggregation({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-provider-succeeded',
      from: 'Running',
      on: 'ProviderSucceeded',
      to: 'Completed',
      guardIds: ['payload-valid', 'current-execution'],
      actorClass: 'provider',
      guard: (snapshot, event) =>
        hasValidPayload(snapshot, event) &&
        event._tag === 'ProviderSucceeded' &&
        isCurrentAttempt(snapshot, event, fenceWithAttemptCount),
      guardRejection: (_snapshot, event) => {
        const field = providerJobPayloadIssue(event);
        return field
          ? ProviderJobRejection.InvalidPayload({ event: event._tag, field })
          : ProviderJobRejection.StaleCommandOutcome({
              commandId: event._tag === 'ProviderSucceeded' ? event.commandId : '',
              executionId: event._tag === 'ProviderSucceeded' ? event.executionId : '',
              executionEpoch: event._tag === 'ProviderSucceeded' ? event.executionEpoch : 0,
            });
      },
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'ProviderSucceeded') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Completed({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            resultRef: event.resultRef,
            commandId: snapshot.commandId,
            executionId: snapshot.executionId,
            executionEpoch: snapshot.executionEpoch,
          }),
          commands: [ProviderJobCommand.PublishAggregation({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-provider-failed-retry',
      from: 'Running',
      on: 'ProviderFailed',
      to: 'Queued',
      guardIds: ['payload-valid', 'current-execution', 'retry-remaining'],
      actorClass: 'provider',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'ProviderFailed' &&
        isCurrentAttempt(snapshot, event, fenceWithAttemptCount) &&
        event.retryable &&
        event.nextAttemptAt !== null &&
        snapshot.attemptCount < snapshot.maxAttempts,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'ProviderFailed' || event.nextAttemptAt === null) {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Queued({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: snapshot.attemptCount,
            maxAttempts: snapshot.maxAttempts,
            nextAttemptAt: event.nextAttemptAt,
            lastFailureReason: event.reason,
          }),
          commands: [ProviderJobCommand.ScheduleRetry({ jobId: snapshot.jobId, nextAttemptAt: event.nextAttemptAt })],
        };
      },
    },
    {
      id: 'running-provider-failed-exhausted',
      from: 'Running',
      on: 'ProviderFailed',
      to: 'Failed',
      guardIds: ['payload-valid', 'current-execution', 'retry-exhausted'],
      actorClass: 'provider',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'ProviderFailed' &&
        isCurrentAttempt(snapshot, event, fenceWithAttemptCount) &&
        (!event.retryable || snapshot.attemptCount >= snapshot.maxAttempts),
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'ProviderFailed') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Failed({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: snapshot.attemptCount,
            maxAttempts: snapshot.maxAttempts,
            failureReason: event.reason,
            commandId: snapshot.commandId,
            executionId: snapshot.executionId,
            executionEpoch: snapshot.executionEpoch,
          }),
          commands: [ProviderJobCommand.PublishAggregation({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-stale-reclaim-retry',
      from: 'Running',
      on: 'StaleLeaseReclaimRequested',
      to: 'Queued',
      guardIds: ['payload-valid', 'stale-lease', 'retry-remaining'],
      actorClass: 'system',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'StaleLeaseReclaimRequested' &&
        isStaleLease(snapshot, event) &&
        snapshot.attemptCount < snapshot.maxAttempts,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'StaleLeaseReclaimRequested') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Queued({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: snapshot.attemptCount,
            maxAttempts: snapshot.maxAttempts,
            nextAttemptAt: event.at,
            lastFailureReason: event.failureReason,
          }),
          commands: [ProviderJobCommand.DispatchJob({ jobId: snapshot.jobId })],
        };
      },
    },
    {
      id: 'running-stale-reclaim-exhausted',
      from: 'Running',
      on: 'StaleLeaseReclaimRequested',
      to: 'Failed',
      guardIds: ['payload-valid', 'stale-lease', 'retry-exhausted'],
      actorClass: 'system',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'StaleLeaseReclaimRequested' &&
        isStaleLease(snapshot, event) &&
        snapshot.attemptCount >= snapshot.maxAttempts,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'StaleLeaseReclaimRequested') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ProviderJobState.Failed({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: snapshot.attemptCount,
            maxAttempts: snapshot.maxAttempts,
            failureReason: event.failureReason,
            commandId: snapshot.commandId,
            executionId: snapshot.executionId,
            executionEpoch: snapshot.executionEpoch,
          }),
          commands: [ProviderJobCommand.PublishAggregation({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'failed-admin-retry',
      from: 'Failed',
      on: 'AdminRetryRequested',
      to: 'Queued',
      guardIds: ['payload-valid', 'admin-actor'],
      actorClass: 'admin',
      guard: (snapshot, event) =>
        hasValidPayload(snapshot, event) && event._tag === 'AdminRetryRequested' && event.actorClass === 'admin',
      guardRejection: (_snapshot, event) => {
        const field = providerJobPayloadIssue(event);
        return field
          ? ProviderJobRejection.InvalidPayload({ event: event._tag, field })
          : ProviderJobRejection.AdminCapabilityMissing({
              actorClass: event._tag === 'AdminRetryRequested' ? event.actorClass : 'unknown',
            });
      },
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Failed' || event._tag !== 'AdminRetryRequested') return { next: snapshot, commands: [] };
        return {
          next: ProviderJobState.Queued({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: 0,
            maxAttempts: snapshot.maxAttempts,
            nextAttemptAt: null,
            lastFailureReason: null,
          }),
          commands: [
            ProviderJobCommand.ReopenScanRun({
              scanRunId: snapshot.scanRunId,
              jobId: snapshot.jobId,
              actorId: event.actorId,
            }),
            ProviderJobCommand.DispatchJob({ jobId: snapshot.jobId }),
          ],
        };
      },
    },
  ];

  if (overrides.allowAdminRetryFromCompleted) {
    rules.push({
      id: 'completed-admin-retry-mutant',
      from: 'Completed',
      on: 'AdminRetryRequested',
      to: 'Queued',
      guardIds: [],
      actorClass: 'admin',
      reduce: (snapshot) => {
        if (snapshot._tag !== 'Completed') return { next: snapshot, commands: [] };
        return {
          next: ProviderJobState.Queued({
            jobId: snapshot.jobId,
            scanRunId: snapshot.scanRunId,
            provider: snapshot.provider,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            attemptCount: 0,
            maxAttempts: 2,
            nextAttemptAt: null,
            lastFailureReason: null,
          }),
          commands: [],
        };
      },
    });
  }

  return rules.map((rule) =>
    overrides.unguardRuleIds?.has(rule.id) ? { ...rule, guard: undefined, guardIds: [] as const, guardRejection: undefined } : rule,
  );
};

const reject = (snapshot: ProviderJobState, event: ProviderJobEvent): ProviderJobRejection =>
  providerJobPayloadIssue(event) !== null
    ? ProviderJobRejection.InvalidPayload({ event: event._tag, field: providerJobPayloadIssue(event)! })
    : Match.value(event).pipe(
    Match.tag('ClaimRequested', () =>
      snapshot._tag === 'Queued'
        ? ProviderJobRejection.ClaimNotDue({ nextAttemptAt: snapshot.nextAttemptAt ?? 0, at: event._tag === 'ClaimRequested' ? event.at : 0 })
        : ProviderJobRejection.ClaimConflict({ state: snapshot._tag }),
    ),
    Match.tag('ProviderSucceeded', (outcome) =>
      ProviderJobRejection.StaleCommandOutcome({
        commandId: outcome.commandId,
        executionId: outcome.executionId,
        executionEpoch: outcome.executionEpoch,
      }),
    ),
    Match.tag('ProviderFailed', (outcome) =>
      ProviderJobRejection.StaleCommandOutcome({
        commandId: outcome.commandId,
        executionId: outcome.executionId,
        executionEpoch: outcome.executionEpoch,
      }),
    ),
    Match.tag('AdminRetryRequested', (admin) =>
      admin.actorClass === 'admin'
        ? ProviderJobRejection.NotRetryable({ state: snapshot._tag })
        : ProviderJobRejection.AdminCapabilityMissing({ actorClass: admin.actorClass }),
    ),
    Match.tag('StaleLeaseReclaimRequested', (reclaim) =>
      snapshot._tag === 'Running'
        ? ProviderJobRejection.StaleLeaseNotExpired({ lockedUntil: snapshot.lockedUntil, at: reclaim.at })
        : ProviderJobRejection.EventNotAllowed({ state: snapshot._tag, event: reclaim._tag }),
    ),
    Match.orElse(() => ProviderJobRejection.EventNotAllowed({ state: snapshot._tag, event: event._tag })),
  );

const commandHandlers: readonly CommandHandlerRegistration<ProviderJobCommand, ProviderJobEvent>[] = [
  { commandTag: 'CallProvider', outcomeEventTag: 'ProviderSucceeded' },
  { commandTag: 'PublishAggregation', outcomeEventTag: 'AggregationAck' },
  { commandTag: 'ScheduleRetry', outcomeEventTag: 'ClaimRequested' },
  { commandTag: 'DispatchJob', outcomeEventTag: 'ClaimRequested' },
  { commandTag: 'ReopenScanRun', outcomeEventTag: 'ScanRunReopened' },
];

export type ProviderJobRegistration = MachineRegistration<
  ProviderJobState,
  ProviderJobEvent,
  ProviderJobCommand,
  ProviderJobRejection
> & {
  readonly privacyPolicy: ClosedMetadataPrivacyPolicy;
};

export const createProviderJobRegistration = (overrides: ProviderJobOverrides = {}): ProviderJobRegistration => {
  const definition = defineMachine<ProviderJobState, ProviderJobEvent, ProviderJobCommand, ProviderJobRejection>({
    id: 'scan-provider-job',
    version: PROVIDER_JOB_MACHINE_VERSION,
    initialState: { ...initialQueued() },
    stateTags: ['Queued', 'Running', 'Completed', 'Failed', 'Skipped'],
    eventTags: [
      'ClaimRequested',
      'SkipRequested',
      'ProviderSucceeded',
      'ProviderFailed',
      'StaleLeaseReclaimRequested',
      'AdminRetryRequested',
      'AggregationAck',
      'ScanRunReopened',
    ],
    terminalStateTags: new Set(['Completed', 'Failed', 'Skipped']),
    adminEventTags: new Set(['AdminRetryRequested']),
    rules: buildRules(overrides),
    reject,
    telemetry: { namespace: 'scan-provider-job' },
  });
  const registration: ProviderJobRegistration = {
    definition,
    commandTags: ['CallProvider', 'PublishAggregation', 'ScheduleRetry', 'DispatchJob', 'ReopenScanRun'],
    commandHandlers,
    migrationStateTags: new Set(),
    waitingStateTags: new Set(['Running']),
    adminActorClasses: new Set(['admin']),
    migrators: [],
    privacyPolicy: SCAN_AGGREGATE_PRIVACY_POLICY,
  };
  assertClosedMetadataPrivacyPolicy(registration.privacyPolicy);
  if (!overrides.allowAdminRetryFromCompleted) {
    const violations = checkMachineInvariants(registration).violations;
    if (violations.length > 0) {
      throw new Error(violations.map((violation) => violation.message).join('; '));
    }
  }
  return registration;
};

export const providerJobRegistration = createProviderJobRegistration();
export const providerJobMachine = providerJobRegistration.definition;
