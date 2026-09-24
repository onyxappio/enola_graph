import {
  checkMachineInvariants,
  defineMachine,
  type CommandHandlerRegistration,
  type Decision as MachineDecision,
  type MachineRegistration,
  type TransitionRule,
} from '@onyx/state-machine-kernel';
import type { ScanProviderJobState } from '@onyx/contracts';
import { Data, Match } from 'effect';
import {
  SCAN_AGGREGATE_PRIVACY_POLICY,
  assertClosedMetadataPrivacyPolicy,
  type ClosedMetadataPrivacyPolicy,
} from './privacyPolicy';

export type ScanJobSummary = {
  readonly jobId: string;
  readonly state: ScanProviderJobState;
};

export type ScanRunAggregationExecution = {
  readonly commandId: string;
  readonly executionId: string;
  readonly executionEpoch: number;
};

export type PendingScanRunAggregation = ScanRunAggregationExecution & {
  readonly expectedRevision: number;
};

export type ScanRunFailureReason = 'all_providers_failed' | 'no_supported_provider' | 'aggregation_persistence_failed';

export type ScanRunState = Data.TaggedEnum<{
  Requested: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
  };
  Queued: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
  };
  Running: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly jobs: readonly ScanJobSummary[];
    readonly aggregationAttemptCount: number;
    readonly maxAggregationAttempts: number;
    readonly pendingAggregation: PendingScanRunAggregation | null;
  };
  Completed: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly completedJobIds: readonly string[];
  };
  CompletedPartial: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly completedJobIds: readonly string[];
    readonly failedJobIds: readonly string[];
    readonly skippedJobIds: readonly string[];
  };
  Failed: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
    readonly failureReason: ScanRunFailureReason;
    readonly failedJobIds: readonly string[];
    readonly skippedJobIds: readonly string[];
  };
  Expired: {
    readonly scanRunId: string;
    readonly revision: number;
    readonly machineVersion: number;
  };
}>;

export const ScanRunState = Data.taggedEnum<ScanRunState>();

export type ScanRunEvent = Data.TaggedEnum<{
  Start: { readonly eventId: string; readonly jobs: readonly ScanJobSummary[] };
  AggregationRequested: {
    readonly eventId: string;
    readonly jobs: readonly ScanJobSummary[];
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
  };
  ReportPersisted: {
    readonly eventId: string;
    readonly reportVersion: number;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  AggregationPersistenceFailed: {
    readonly eventId: string;
    readonly reason: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  Expire: { readonly eventId: string; readonly at: number };
  AdminReopenRequested: {
    readonly eventId: string;
    readonly actorClass: string;
    readonly actorId: string;
    readonly jobId: string;
  };
  ProductHandoffAck: { readonly eventId: string };
}>;

export const ScanRunEvent = Data.taggedEnum<ScanRunEvent>();

export type ScanRunCommand = Data.TaggedEnum<{
  RequestAggregation: { readonly scanRunId: string };
  PersistUnifiedReport: {
    readonly scanRunId: string;
    readonly commandId: string;
    readonly executionId: string;
    readonly executionEpoch: number;
    readonly issuedAtRevision: number;
  };
  NotifyProductHandoff: { readonly scanRunId: string };
}>;

export const ScanRunCommand = Data.taggedEnum<ScanRunCommand>();

export type ScanRunRejection = Data.TaggedEnum<{
  EventNotAllowed: { readonly state: ScanRunState['_tag']; readonly event: ScanRunEvent['_tag'] };
  NotYetTerminal: { readonly liveJobIds: readonly string[] };
  AlreadyFinalized: { readonly state: ScanRunState['_tag'] };
  AdminCapabilityMissing: { readonly actorClass: string };
  AggregationAlreadyPending: ScanRunAggregationExecution;
  StaleAggregationOutcome: ScanRunAggregationExecution;
  InvalidPayload: { readonly event: ScanRunEvent['_tag']; readonly field: string };
}>;

export const ScanRunRejection = Data.taggedEnum<ScanRunRejection>();

export const SCAN_RUN_MACHINE_VERSION = 1 as const;

export type ScanRunDecision = MachineDecision<ScanRunState, ScanRunCommand, ScanRunRejection>;

const isNonEmpty = (value: string): boolean => value.trim().length > 0;
const isTimestamp = (value: number): boolean => Number.isFinite(value) && value >= 0;
const isPositiveInteger = (value: number): boolean => Number.isInteger(value) && value > 0;
const isNonNegativeInteger = (value: number): boolean => Number.isInteger(value) && value >= 0;
const jobStates = new Set<ScanProviderJobState>(['queued', 'running', 'completed', 'failed', 'skipped']);

const jobsPayloadIssue = (jobs: readonly ScanJobSummary[]): string | null => {
  const ids = new Set<string>();
  for (const job of jobs) {
    if (!isNonEmpty(job.jobId)) return 'jobs.jobId';
    if (!jobStates.has(job.state)) return 'jobs.state';
    if (ids.has(job.jobId)) return 'jobs.jobId';
    ids.add(job.jobId);
  }
  return null;
};

const scanRunPayloadIssue = (event: ScanRunEvent): string | null =>
  Match.value(event).pipe(
    Match.tag('Start', (input) => {
      if (!isNonEmpty(input.eventId)) return 'eventId';
      return jobsPayloadIssue(input.jobs);
    }),
    Match.tag('AggregationRequested', (input) => {
      if (!isNonEmpty(input.eventId)) return 'eventId';
      const jobsIssue = jobsPayloadIssue(input.jobs);
      if (jobsIssue) return jobsIssue;
      if (!isNonEmpty(input.commandId)) return 'commandId';
      if (!isNonEmpty(input.executionId)) return 'executionId';
      if (!isPositiveInteger(input.executionEpoch)) return 'executionEpoch';
      return null;
    }),
    Match.tag('ReportPersisted', (persisted) => {
      if (!isNonEmpty(persisted.eventId)) return 'eventId';
      if (!isPositiveInteger(persisted.reportVersion)) return 'reportVersion';
      if (!isNonEmpty(persisted.commandId)) return 'commandId';
      if (!isNonEmpty(persisted.executionId)) return 'executionId';
      if (!isPositiveInteger(persisted.executionEpoch)) return 'executionEpoch';
      if (!isNonNegativeInteger(persisted.issuedAtRevision)) return 'issuedAtRevision';
      return null;
    }),
    Match.tag('AggregationPersistenceFailed', (failure) => {
      if (!isNonEmpty(failure.eventId)) return 'eventId';
      if (!isNonEmpty(failure.reason)) return 'reason';
      if (!isNonEmpty(failure.commandId)) return 'commandId';
      if (!isNonEmpty(failure.executionId)) return 'executionId';
      if (!isPositiveInteger(failure.executionEpoch)) return 'executionEpoch';
      if (!isNonNegativeInteger(failure.issuedAtRevision)) return 'issuedAtRevision';
      return null;
    }),
    Match.tag('Expire', (expire) => {
      if (!isNonEmpty(expire.eventId)) return 'eventId';
      if (!isTimestamp(expire.at)) return 'at';
      return null;
    }),
    Match.tag('AdminReopenRequested', (admin) => {
      if (!isNonEmpty(admin.eventId)) return 'eventId';
      if (!isNonEmpty(admin.actorClass)) return 'actorClass';
      if (!isNonEmpty(admin.actorId)) return 'actorId';
      if (!isNonEmpty(admin.jobId)) return 'jobId';
      return null;
    }),
    Match.tag('ProductHandoffAck', (ack) => (!isNonEmpty(ack.eventId) ? 'eventId' : null)),
    Match.exhaustive,
  );

const hasValidPayload = (_snapshot: ScanRunState, event: ScanRunEvent): boolean => scanRunPayloadIssue(event) === null;

type AggregationOutcome = Extract<
  ScanRunEvent,
  { _tag: 'ReportPersisted' | 'AggregationPersistenceFailed' }
>;

const aggregationExecutionOf = (event: AggregationOutcome): ScanRunAggregationExecution => ({
  commandId: event.commandId,
  executionId: event.executionId,
  executionEpoch: event.executionEpoch,
});

const isCurrentAggregationOutcome = (
  snapshot: ScanRunState,
  event: AggregationOutcome,
  ignoreFence = false,
): snapshot is Extract<ScanRunState, { _tag: 'Running' }> =>
  snapshot._tag === 'Running' &&
  snapshot.pendingAggregation !== null &&
  (ignoreFence ||
    (snapshot.pendingAggregation.commandId === event.commandId &&
      snapshot.pendingAggregation.executionId === event.executionId &&
      snapshot.pendingAggregation.executionEpoch === event.executionEpoch &&
      snapshot.pendingAggregation.expectedRevision === event.issuedAtRevision));

const liveJobIds = (jobs: readonly ScanJobSummary[]): readonly string[] =>
  jobs.filter((job) => job.state === 'queued' || job.state === 'running').map((job) => job.jobId);

const completedJobIds = (jobs: readonly ScanJobSummary[]): readonly string[] =>
  jobs.filter((job) => job.state === 'completed').map((job) => job.jobId);

const failedJobIds = (jobs: readonly ScanJobSummary[]): readonly string[] =>
  jobs.filter((job) => job.state === 'failed').map((job) => job.jobId);

const skippedJobIds = (jobs: readonly ScanJobSummary[]): readonly string[] =>
  jobs.filter((job) => job.state === 'skipped').map((job) => job.jobId);

export const decideScanRunAggregation = (
  jobs: readonly ScanJobSummary[],
): 'not_yet' | 'completed' | 'completed_partial' | 'failed' => {
  if (liveJobIds(jobs).length > 0) return 'not_yet';
  const completed = completedJobIds(jobs).length;
  const failed = failedJobIds(jobs).length;
  if (completed > 0) return failed > 0 ? 'completed_partial' : 'completed';
  return 'failed';
};

const failureReasonOf = (jobs: readonly ScanJobSummary[]): Exclude<ScanRunFailureReason, 'aggregation_persistence_failed'> =>
  failedJobIds(jobs).length > 0 ? 'all_providers_failed' : 'no_supported_provider';

const initialQueued = (): Extract<ScanRunState, { _tag: 'Queued' }> =>
  ScanRunState.Queued({ scanRunId: 'run_1', revision: 0, machineVersion: SCAN_RUN_MACHINE_VERSION });

const startReduce = (
  snapshot: ScanRunState,
  event: ScanRunEvent,
): { next: ScanRunState; commands: readonly ScanRunCommand[] } => {
  if ((snapshot._tag !== 'Queued' && snapshot._tag !== 'Requested') || event._tag !== 'Start') {
    return { next: snapshot, commands: [] };
  }
  const next = ScanRunState.Running({
    scanRunId: snapshot.scanRunId,
    revision: snapshot.revision + 1,
    machineVersion: snapshot.machineVersion,
    jobs: event.jobs,
    aggregationAttemptCount: 0,
    maxAggregationAttempts: 3,
    pendingAggregation: null,
  });
  return {
    next,
    commands: liveJobIds(event.jobs).length === 0 ? [ScanRunCommand.RequestAggregation({ scanRunId: snapshot.scanRunId })] : [],
  };
};

const expireReduce = (snapshot: ScanRunState): { next: ScanRunState; commands: readonly ScanRunCommand[] } => ({
  next: ScanRunState.Expired({
    scanRunId: snapshot.scanRunId,
    revision: snapshot.revision + 1,
    machineVersion: snapshot.machineVersion,
  }),
  commands: [],
});

const reopenJobs = (snapshot: ScanRunState, jobId: string): readonly ScanJobSummary[] => {
  if (snapshot._tag === 'Completed') {
    return snapshot.completedJobIds.map((id) => ({ jobId: id, state: id === jobId ? 'queued' : 'completed' }));
  }
  if (snapshot._tag === 'CompletedPartial') {
    return [
      ...snapshot.completedJobIds.map((id) => ({ jobId: id, state: 'completed' as const })),
      ...snapshot.failedJobIds.map((id) => ({
        jobId: id,
        state: id === jobId ? ('queued' as const) : ('failed' as const),
      })),
      ...snapshot.skippedJobIds.map((id) => ({ jobId: id, state: 'skipped' as const })),
    ] as const;
  }
  if (snapshot._tag === 'Failed') {
    return [
      ...snapshot.failedJobIds.map((id) => ({
        jobId: id,
        state: id === jobId ? ('queued' as const) : ('failed' as const),
      })),
      ...snapshot.skippedJobIds.map((id) => ({ jobId: id, state: 'skipped' as const })),
    ] as const;
  }
  return [{ jobId, state: 'queued' }];
};

export type ScanRunOverrides = {
  readonly allowAggregationWhenLive?: boolean;
  readonly ignoreAggregationOutcomeFence?: boolean;
};

const buildRules = (
  overrides: ScanRunOverrides = {},
): readonly TransitionRule<ScanRunState, ScanRunEvent, ScanRunCommand, ScanRunRejection>[] => {
  const allowLive = overrides.allowAggregationWhenLive === true;
  const ignoreAggregationOutcomeFence = overrides.ignoreAggregationOutcomeFence === true;
  const aggregationGuardRejection = (snapshot: ScanRunState, event: ScanRunEvent): ScanRunRejection => {
    const field = scanRunPayloadIssue(event);
    if (field) return ScanRunRejection.InvalidPayload({ event: event._tag, field });
    if (snapshot._tag === 'Running' && snapshot.pendingAggregation !== null) {
      return ScanRunRejection.AggregationAlreadyPending({
        commandId: snapshot.pendingAggregation.commandId,
        executionId: snapshot.pendingAggregation.executionId,
        executionEpoch: snapshot.pendingAggregation.executionEpoch,
      });
    }
    return ScanRunRejection.NotYetTerminal({
      liveJobIds: event._tag === 'AggregationRequested' ? liveJobIds(event.jobs) : [],
    });
  };
  const reopen = (from: 'Completed' | 'CompletedPartial' | 'Failed' | 'Expired'): TransitionRule<
    ScanRunState,
    ScanRunEvent,
    ScanRunCommand,
    ScanRunRejection
  > => ({
    id: `${from.toLowerCase()}-admin-reopen`,
    from,
    on: 'AdminReopenRequested',
    to: 'Running',
    guardIds: ['payload-valid', 'admin-actor'],
    actorClass: 'admin',
    guard: (snapshot, event) =>
      hasValidPayload(snapshot, event) && event._tag === 'AdminReopenRequested' && event.actorClass === 'admin',
    guardRejection: (_snapshot, event) => {
      const field = scanRunPayloadIssue(event);
      return field
        ? ScanRunRejection.InvalidPayload({ event: event._tag, field })
        : ScanRunRejection.AdminCapabilityMissing({
            actorClass: event._tag === 'AdminReopenRequested' ? event.actorClass : 'unknown',
          });
    },
    reduce: (snapshot, event) => {
      if (event._tag !== 'AdminReopenRequested') return { next: snapshot, commands: [] };
      return {
        next: ScanRunState.Running({
          scanRunId: snapshot.scanRunId,
          revision: snapshot.revision + 1,
          machineVersion: snapshot.machineVersion,
          jobs: reopenJobs(snapshot, event.jobId),
          aggregationAttemptCount: 0,
          maxAggregationAttempts: 3,
          pendingAggregation: null,
        }),
        commands: [],
      };
    },
  });

  return [
    {
      id: 'queued-start',
      from: 'Queued',
      on: 'Start',
      to: 'Running',
      guardIds: ['payload-valid'],
      actorClass: 'worker',
      guard: hasValidPayload,
      reduce: startReduce,
    },
    {
      id: 'requested-start',
      from: 'Requested',
      on: 'Start',
      to: 'Running',
      guardIds: ['payload-valid'],
      actorClass: 'worker',
      guard: hasValidPayload,
      reduce: startReduce,
    },
    {
      id: 'queued-expire',
      from: 'Queued',
      on: 'Expire',
      to: 'Expired',
      guardIds: ['payload-valid'],
      actorClass: 'clock',
      guard: hasValidPayload,
      reduce: expireReduce,
    },
    {
      id: 'running-expire',
      from: 'Running',
      on: 'Expire',
      to: 'Expired',
      guardIds: ['payload-valid'],
      actorClass: 'clock',
      guard: hasValidPayload,
      reduce: expireReduce,
    },
    {
      id: 'running-aggregation-reportable',
      from: 'Running',
      on: 'AggregationRequested',
      to: 'Running',
      guardIds: ['payload-valid', 'aggregation-not-pending', 'final-only', 'reportable'],
      actorClass: 'worker',
      guard: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'AggregationRequested') return false;
        if (!hasValidPayload(snapshot, event)) return false;
        if (snapshot.pendingAggregation !== null) return false;
        const kind = decideScanRunAggregation(event.jobs);
        return (allowLive || kind !== 'not_yet') && (kind === 'completed' || kind === 'completed_partial' || allowLive);
      },
      guardRejection: aggregationGuardRejection,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'AggregationRequested') {
          return { next: snapshot, commands: [] };
        }
        const revision = snapshot.revision + 1;
        return {
          next: ScanRunState.Running({
            ...snapshot,
            revision,
            jobs: event.jobs,
            pendingAggregation: {
              commandId: event.commandId,
              executionId: event.executionId,
              executionEpoch: event.executionEpoch,
              expectedRevision: revision,
            },
          }),
          commands: [
            ScanRunCommand.PersistUnifiedReport({
              scanRunId: snapshot.scanRunId,
              commandId: event.commandId,
              executionId: event.executionId,
              executionEpoch: event.executionEpoch,
              issuedAtRevision: revision,
            }),
          ],
        };
      },
    },
    {
      id: 'running-aggregation-failed',
      from: 'Running',
      on: 'AggregationRequested',
      to: 'Failed',
      guardIds: ['payload-valid', 'aggregation-not-pending', 'final-only', 'all-failed'],
      actorClass: 'worker',
      guard: (_snapshot, event) =>
        hasValidPayload(_snapshot, event) &&
        _snapshot._tag === 'Running' &&
        _snapshot.pendingAggregation === null &&
        event._tag === 'AggregationRequested' &&
        decideScanRunAggregation(event.jobs) === 'failed',
      guardRejection: aggregationGuardRejection,
      reduce: (snapshot, event) => {
        if (snapshot._tag !== 'Running' || event._tag !== 'AggregationRequested') {
          return { next: snapshot, commands: [] };
        }
        return {
          next: ScanRunState.Failed({
            scanRunId: snapshot.scanRunId,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            failureReason: failureReasonOf(event.jobs),
            failedJobIds: failedJobIds(event.jobs),
            skippedJobIds: skippedJobIds(event.jobs),
          }),
          commands: [ScanRunCommand.NotifyProductHandoff({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-report-persisted-completed',
      from: 'Running',
      on: 'ReportPersisted',
      to: 'Completed',
      guardIds: ['payload-valid', 'current-aggregation', 'completed'],
      actorClass: 'worker',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'ReportPersisted' &&
        isCurrentAggregationOutcome(snapshot, event, ignoreAggregationOutcomeFence) &&
        decideScanRunAggregation(snapshot.jobs) === 'completed',
      reduce: (snapshot) => {
        if (snapshot._tag !== 'Running') return { next: snapshot, commands: [] };
        return {
          next: ScanRunState.Completed({
            scanRunId: snapshot.scanRunId,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            completedJobIds: completedJobIds(snapshot.jobs),
          }),
          commands: [ScanRunCommand.NotifyProductHandoff({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-report-persisted-partial',
      from: 'Running',
      on: 'ReportPersisted',
      to: 'CompletedPartial',
      guardIds: ['payload-valid', 'current-aggregation', 'completed-partial'],
      actorClass: 'worker',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'ReportPersisted' &&
        isCurrentAggregationOutcome(snapshot, event, ignoreAggregationOutcomeFence) &&
        decideScanRunAggregation(snapshot.jobs) === 'completed_partial',
      reduce: (snapshot) => {
        if (snapshot._tag !== 'Running') return { next: snapshot, commands: [] };
        return {
          next: ScanRunState.CompletedPartial({
            scanRunId: snapshot.scanRunId,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            completedJobIds: completedJobIds(snapshot.jobs),
            failedJobIds: failedJobIds(snapshot.jobs),
            skippedJobIds: skippedJobIds(snapshot.jobs),
          }),
          commands: [ScanRunCommand.NotifyProductHandoff({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    {
      id: 'running-aggregation-persist-retry',
      from: 'Running',
      on: 'AggregationPersistenceFailed',
      to: 'Running',
      guardIds: ['payload-valid', 'current-aggregation', 'retry-remaining'],
      actorClass: 'worker',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'AggregationPersistenceFailed' &&
        isCurrentAggregationOutcome(snapshot, event, ignoreAggregationOutcomeFence) &&
        snapshot.aggregationAttemptCount + 1 < snapshot.maxAggregationAttempts,
      reduce: (snapshot) => {
        if (snapshot._tag !== 'Running') return { next: snapshot, commands: [] };
        return {
          next: ScanRunState.Running({
            ...snapshot,
            revision: snapshot.revision + 1,
            aggregationAttemptCount: snapshot.aggregationAttemptCount + 1,
            pendingAggregation: null,
          }),
          commands: [],
        };
      },
    },
    {
      id: 'running-aggregation-persist-exhausted',
      from: 'Running',
      on: 'AggregationPersistenceFailed',
      to: 'Failed',
      guardIds: ['payload-valid', 'current-aggregation', 'retry-exhausted'],
      actorClass: 'worker',
      guard: (snapshot, event) =>
        snapshot._tag === 'Running' &&
        hasValidPayload(snapshot, event) &&
        event._tag === 'AggregationPersistenceFailed' &&
        isCurrentAggregationOutcome(snapshot, event, ignoreAggregationOutcomeFence) &&
        snapshot.aggregationAttemptCount + 1 >= snapshot.maxAggregationAttempts,
      reduce: (snapshot) => {
        if (snapshot._tag !== 'Running') return { next: snapshot, commands: [] };
        return {
          next: ScanRunState.Failed({
            scanRunId: snapshot.scanRunId,
            revision: snapshot.revision + 1,
            machineVersion: snapshot.machineVersion,
            failureReason: 'aggregation_persistence_failed',
            failedJobIds: failedJobIds(snapshot.jobs),
            skippedJobIds: skippedJobIds(snapshot.jobs),
          }),
          commands: [ScanRunCommand.NotifyProductHandoff({ scanRunId: snapshot.scanRunId })],
        };
      },
    },
    reopen('Completed'),
    reopen('CompletedPartial'),
    reopen('Failed'),
    reopen('Expired'),
  ];
};

const reject = (snapshot: ScanRunState, event: ScanRunEvent): ScanRunRejection =>
  scanRunPayloadIssue(event) !== null
    ? ScanRunRejection.InvalidPayload({ event: event._tag, field: scanRunPayloadIssue(event)! })
    : Match.value(event).pipe(
    Match.tag('AggregationRequested', (aggregation) =>
      snapshot._tag !== 'Running'
        ? ScanRunRejection.AlreadyFinalized({ state: snapshot._tag })
        : snapshot.pendingAggregation !== null
          ? ScanRunRejection.AggregationAlreadyPending({
              commandId: snapshot.pendingAggregation.commandId,
              executionId: snapshot.pendingAggregation.executionId,
              executionEpoch: snapshot.pendingAggregation.executionEpoch,
            })
          : ScanRunRejection.NotYetTerminal({ liveJobIds: liveJobIds(aggregation.jobs) }),
    ),
    Match.tag('AdminReopenRequested', (admin) =>
      ScanRunRejection.AdminCapabilityMissing({ actorClass: admin.actorClass }),
    ),
    Match.tag('ReportPersisted', 'AggregationPersistenceFailed', (outcome) =>
      ScanRunRejection.StaleAggregationOutcome(aggregationExecutionOf(outcome)),
    ),
    Match.orElse(() => ScanRunRejection.EventNotAllowed({ state: snapshot._tag, event: event._tag })),
  );

const commandHandlers: readonly CommandHandlerRegistration<ScanRunCommand, ScanRunEvent>[] = [
  { commandTag: 'RequestAggregation', outcomeEventTag: 'AggregationRequested' },
  { commandTag: 'PersistUnifiedReport', outcomeEventTag: 'ReportPersisted' },
  { commandTag: 'NotifyProductHandoff', outcomeEventTag: 'ProductHandoffAck' },
];

export type ScanRunRegistration = MachineRegistration<ScanRunState, ScanRunEvent, ScanRunCommand, ScanRunRejection> & {
  readonly privacyPolicy: ClosedMetadataPrivacyPolicy;
};

export const createScanRunRegistration = (overrides: ScanRunOverrides = {}): ScanRunRegistration => {
  const definition = defineMachine<ScanRunState, ScanRunEvent, ScanRunCommand, ScanRunRejection>({
    id: 'scan-run',
    version: SCAN_RUN_MACHINE_VERSION,
    initialState: { ...initialQueued() },
    stateTags: ['Requested', 'Queued', 'Running', 'Completed', 'CompletedPartial', 'Failed', 'Expired'],
    eventTags: [
      'Start',
      'AggregationRequested',
      'ReportPersisted',
      'AggregationPersistenceFailed',
      'Expire',
      'AdminReopenRequested',
      'ProductHandoffAck',
    ],
    terminalStateTags: new Set(['Completed', 'CompletedPartial', 'Failed', 'Expired']),
    adminEventTags: new Set(['AdminReopenRequested']),
    rules: buildRules(overrides),
    reject,
    telemetry: { namespace: 'scan-run' },
  });
  const registration: ScanRunRegistration = {
    definition,
    commandTags: ['RequestAggregation', 'PersistUnifiedReport', 'NotifyProductHandoff'],
    commandHandlers,
    migrationStateTags: new Set(['Requested']),
    waitingStateTags: new Set(['Running']),
    adminActorClasses: new Set(['admin']),
    migrators: [],
    privacyPolicy: SCAN_AGGREGATE_PRIVACY_POLICY,
  };
  assertClosedMetadataPrivacyPolicy(registration.privacyPolicy);
  const violations = checkMachineInvariants(registration).violations;
  if (violations.length > 0) {
    throw new Error(violations.map((violation) => violation.message).join('; '));
  }
  return registration;
};

export const scanRunRegistration = createScanRunRegistration();
export const scanRunMachine = scanRunRegistration.definition;
