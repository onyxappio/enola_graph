import { and, desc, eq, inArray } from 'drizzle-orm';
import { Effect } from 'effect';
import {
  adminAuditEvents,
  scanProviderJobCommandOutbox,
  scanProviderJobDecisionReceipts,
  scanProviderJobs,
  scanRuns,
  type OnyxDatabase,
} from '@onyx/database';
import {
  InstanceNotFound,
  TechnicalFailure,
  copyCorrelation,
  resolveStoredCorrelation,
  type CommandFence,
  type CorrelationInput,
  type DecisionReceipt,
  type InstanceRecord,
  type MachineRepository,
  type OutboxEnvelope,
} from '@onyx/state-machine-runtime';
import {
  ProviderJobState,
  type ProviderJobCommand,
} from './machines/providerJob';

const MACHINE_ID = 'scan-provider-job';
const RESPONSE_SCHEMA_VERSION = 1;

export type ProviderJobRepositoryOptions = {
  /**
   * Supplies correlation for a job before its first decision receipt exists.
   * Six-string documents remain readable through explicit normalization.
   * Missing fields stay unavailable; identifiers are never manufactured.
   */
  readonly correlationForJob?: (providerJobId: string) => CorrelationInput | Promise<CorrelationInput>;
  /**
   * Command tags that take exclusive in-process execution rights at claim
   * commit. Sets inline_owned_at; never sets delivered_at.
   */
  readonly inlineOwnedCommandTags?: readonly string[];
};

const PRE_BEGIN_CODES = new Set([
  'ECONNREFUSED',
  'ENOTFOUND',
  'EAI_AGAIN',
  'ENETUNREACH',
  'EHOSTUNREACH',
  'CONNECT_TIMEOUT',
]);

const KNOWN_PRE_COMMIT_SQLSTATES = new Set([
  '55P03', // lock_not_available on FOR UPDATE before writes
]);

export type ClassifiedCommitFailure =
  | { readonly _tag: 'InstanceNotFound'; readonly instanceId: string }
  | { readonly _tag: 'TechnicalFailure'; readonly cause: string }
  | { readonly _tag: 'Uncertain'; readonly cause: string };

const errorCode = (cause: unknown): string | undefined => {
  if (!cause || typeof cause !== 'object' || !('code' in cause)) return undefined;
  const code = (cause as { code?: unknown }).code;
  return typeof code === 'string' || typeof code === 'number' ? String(code) : undefined;
};

export const classifyProviderJobCommitFailure = (cause: unknown): ClassifiedCommitFailure => {
  if (cause instanceof InstanceNotFound) {
    return { _tag: 'InstanceNotFound', instanceId: cause.instanceId };
  }
  const message = cause instanceof Error ? cause.message : String(cause);
  const code = errorCode(cause);
  if (code && (PRE_BEGIN_CODES.has(code) || KNOWN_PRE_COMMIT_SQLSTATES.has(code))) {
    return { _tag: 'TechnicalFailure', cause: message };
  }
  // Conservative: a write may have reached PostgreSQL.
  return { _tag: 'Uncertain', cause: message };
};

export type ProviderJobMachineRepository = MachineRepository<ProviderJobState, ProviderJobCommand>;

export const createProviderJobRepository = (
  db: OnyxDatabase,
  options: ProviderJobRepositoryOptions = {},
): ProviderJobMachineRepository => ({
  load: (instanceId) =>
    runDatabaseEffect(async () => {
      const [row] = await db
        .select()
        .from(scanProviderJobs)
        .where(eq(scanProviderJobs.providerJobId, instanceId))
        .limit(1);
      if (!row) throw new InstanceNotFound({ instanceId });

      const [latestReceipt] = await db
        .select({ correlation: scanProviderJobDecisionReceipts.correlation })
        .from(scanProviderJobDecisionReceipts)
        .where(eq(scanProviderJobDecisionReceipts.providerJobId, instanceId))
        .orderBy(desc(scanProviderJobDecisionReceipts.createdAt))
        .limit(1);
      const supplied = options.correlationForJob
        ? await Promise.resolve(options.correlationForJob(instanceId))
        : undefined;
      const correlation = resolveStoredCorrelation(
        latestReceipt?.correlation,
        instanceId,
        supplied ? () => supplied : undefined,
      );
      const snapshot = snapshotFromRow(row);

      return {
        instanceId: row.providerJobId,
        machineId: MACHINE_ID,
        machineVersion: row.machineVersion,
        revision: row.revision,
        snapshot,
        correlation,
        commandFences: commandFenceFromSnapshot(snapshot),
      } satisfies InstanceRecord<ProviderJobState>;
    }),

  lookupReceipt: (query) =>
    runDatabaseEffect(async () => {
      const [row] = await db
        .select()
        .from(scanProviderJobDecisionReceipts)
        .where(and(
          eq(scanProviderJobDecisionReceipts.providerJobId, query.instanceId),
          eq(scanProviderJobDecisionReceipts.eventId, query.eventId),
          eq(scanProviderJobDecisionReceipts.idempotencyKey, query.idempotencyKey),
        ))
        .limit(1);
      return row ? receiptFromRow(row) : undefined;
    }),

  commit: (intent) =>
    Effect.tryPromise({
      try: async () => {
        try {
          return await db.transaction(async (tx) => {
            const [current] = await tx
              .select()
              .from(scanProviderJobs)
              .where(eq(scanProviderJobs.providerJobId, intent.instanceId))
              .for('update')
              .limit(1);
            if (!current) throw new InstanceNotFound({ instanceId: intent.instanceId });

            const [priorByKey] = await tx
              .select()
              .from(scanProviderJobDecisionReceipts)
              .where(and(
                eq(scanProviderJobDecisionReceipts.providerJobId, intent.instanceId),
                eq(scanProviderJobDecisionReceipts.eventId, intent.receipt.eventId),
                eq(scanProviderJobDecisionReceipts.idempotencyKey, intent.receipt.idempotencyKey),
              ))
              .limit(1);
            if (priorByKey) {
              return priorByKey.fingerprint === intent.receipt.fingerprint
                ? { _tag: 'Replayed' as const, receipt: receiptFromRow(priorByKey) }
                : { _tag: 'IdempotencyConflict' as const };
            }

            const [priorByDecision] = await tx
              .select()
              .from(scanProviderJobDecisionReceipts)
              .where(eq(scanProviderJobDecisionReceipts.decisionId, intent.receipt.decisionId))
              .limit(1);
            if (priorByDecision) {
              return priorByDecision.providerJobId === intent.instanceId
                && priorByDecision.eventId === intent.receipt.eventId
                && priorByDecision.idempotencyKey === intent.receipt.idempotencyKey
                && priorByDecision.fingerprint === intent.receipt.fingerprint
                ? { _tag: 'Replayed' as const, receipt: receiptFromRow(priorByDecision) }
                : { _tag: 'IdempotencyConflict' as const };
            }

            if (current.revision !== intent.expectedRevision) return { _tag: 'CasLost' as const };
            if (snapshotFromRow(current)._tag !== intent.receipt.replay.stateTagBefore) {
              return { _tag: 'CasLost' as const };
            }

            const accepted = intent.receipt.outcome === 'Accepted';
            const expectedRevisionAfter = accepted ? intent.expectedRevision + 1 : intent.expectedRevision;
            if (intent.revision !== expectedRevisionAfter || intent.receipt.revisionBefore !== intent.expectedRevision) {
              return { _tag: 'CasLost' as const };
            }
            if (accepted && !acceptedClaimPreimageHolds(current, intent.snapshot, intent.receipt.replay.stateTagBefore)) {
              return { _tag: 'CasLost' as const };
            }

            const reopenCommands = intent.outbox.filter(
              (envelope): envelope is OutboxEnvelope<Extract<ProviderJobCommand, { _tag: 'ReopenScanRun' }>> =>
                envelope.command._tag === 'ReopenScanRun',
            );
            if (accepted) {
              for (const envelope of reopenCommands) {
                if (envelope.command.jobId !== intent.instanceId) return { _tag: 'CasLost' as const };
                const [reopened] = await tx
                  .update(scanRuns)
                  .set({ state: 'running', failureReason: null, completedAt: null, updatedAt: new Date(intent.receipt.recordedAt) })
                  .where(and(
                    eq(scanRuns.scanRunId, envelope.command.scanRunId),
                    inArray(scanRuns.state, ['completed', 'completed_partial', 'failed', 'expired']),
                  ))
                  .returning({ scanRunId: scanRuns.scanRunId });
                if (!reopened) return { _tag: 'CasLost' as const };
              }
            }

            if (accepted) {
              await tx
                .update(scanProviderJobs)
                .set(rowUpdateFromSnapshot(intent.snapshot, intent.receipt.recordedAt, current))
                .where(and(
                  eq(scanProviderJobs.providerJobId, intent.instanceId),
                  eq(scanProviderJobs.revision, intent.expectedRevision),
                ));
            }

            await tx.insert(scanProviderJobDecisionReceipts).values(receiptInsert(intent.receipt));
            if (intent.outbox.length > 0) {
              await tx.insert(scanProviderJobCommandOutbox).values(
                intent.outbox.map((envelope) =>
                  outboxInsert(
                    envelope,
                    intent.instanceId,
                    intent.receipt.decisionId,
                    intent.receipt.recordedAt,
                    options.inlineOwnedCommandTags ?? [],
                  ),
                ),
              );
            }
            if (accepted) {
              for (const envelope of reopenCommands) {
                await tx.insert(adminAuditEvents).values({
                  auditEventId: `aud_${intent.receipt.decisionId}`,
                  actorId: envelope.command.actorId,
                  action: 'provider_job_retried',
                  targetType: 'provider_job',
                  targetId: envelope.command.jobId,
                  metadata: {
                    scanRunId: envelope.command.scanRunId,
                    decisionId: intent.receipt.decisionId,
                  },
                });
              }
            }
            return { _tag: 'Committed' as const };
          });
        } catch (cause) {
          if (cause instanceof InstanceNotFound) throw cause;
          const classified = classifyProviderJobCommitFailure(cause);
          if (classified._tag === 'Uncertain') return { _tag: 'Uncertain' as const };
          if (classified._tag === 'InstanceNotFound') {
            throw new InstanceNotFound({ instanceId: classified.instanceId });
          }
          throw new TechnicalFailure({ cause: classified.cause });
        }
      },
      catch: (cause) =>
        cause instanceof InstanceNotFound || cause instanceof TechnicalFailure
          ? cause
          : new TechnicalFailure({ cause: cause instanceof Error ? cause.message : String(cause) }),
    }),

  reconcile: (decisionId) =>
    runDatabaseEffect(async () => {
      const [row] = await db
        .select()
        .from(scanProviderJobDecisionReceipts)
        .where(eq(scanProviderJobDecisionReceipts.decisionId, decisionId))
        .limit(1);
      return row ? receiptFromRow(row) : undefined;
    }),
});

type ProviderJobRow = typeof scanProviderJobs.$inferSelect;

const snapshotFromRow = (row: ProviderJobRow): ProviderJobState => {
  const identity = executionIdentityFromRow(row);
  const common = {
    jobId: row.providerJobId,
    scanRunId: row.scanRunId,
    provider: row.provider,
    revision: row.revision,
    machineVersion: row.machineVersion,
  } as const;

  switch (row.state) {
    case 'queued':
      return ProviderJobState.Queued({
        ...common,
        attemptCount: row.attemptCount,
        maxAttempts: row.maxAttempts,
        nextAttemptAt: timestampNumber(row.nextAttemptAt),
        lastFailureReason: row.failureReason,
      });
    case 'running':
      return ProviderJobState.Running({
        ...common,
        attemptCount: row.attemptCount,
        maxAttempts: row.maxAttempts,
        commandId: identity.commandId,
        executionId: identity.executionId,
        executionEpoch: identity.executionEpoch,
        expectedFence: row.revision,
        lockedUntil: timestampNumber(row.lockedUntil),
        startedAt: timestampNumber(row.startedAt) ?? timestampNumber(row.updatedAt) ?? 0,
      });
    case 'completed':
      return ProviderJobState.Completed({
        ...common,
        resultRef: row.resultRef ?? `legacy:result:${row.providerJobId}`,
        commandId: identity.commandId,
        executionId: identity.executionId,
        executionEpoch: identity.executionEpoch,
      });
    case 'failed':
      return ProviderJobState.Failed({
        ...common,
        attemptCount: row.attemptCount,
        maxAttempts: row.maxAttempts,
        failureReason: row.failureReason ?? 'legacy:failure',
        commandId: identity.commandId,
        executionId: identity.executionId,
        executionEpoch: identity.executionEpoch,
      });
    case 'skipped':
      return ProviderJobState.Skipped({
        ...common,
        skipReason: row.skipReason ?? 'provider_disabled',
      });
  }
};

const executionIdentityFromRow = (row: ProviderJobRow): {
  commandId: string;
  executionId: string;
  executionEpoch: number;
} => ({
  commandId: row.machineCommandId ?? `legacy:call-provider:${row.providerJobId}`,
  executionId: row.machineExecutionId ?? `legacy:execution:${row.providerJobId}`,
  executionEpoch: row.machineExecutionEpoch ?? Math.max(row.attemptCount, 1),
});

const acceptedClaimPreimageHolds = (
  current: ProviderJobRow,
  next: ProviderJobState,
  stateTagBefore: string,
): boolean => {
  if (next._tag !== 'Running' || stateTagBefore !== 'Queued') return true;
  if (current.attemptCount !== next.attemptCount - 1) return false;
  if (current.maxAttempts !== next.maxAttempts) return false;
  if (current.provider !== next.provider) return false;
  if (current.scanRunId !== next.scanRunId) return false;
  if (current.machineVersion !== next.machineVersion) return false;
  if (current.nextAttemptAt != null && current.nextAttemptAt.getTime() > next.startedAt) return false;
  return true;
};

const commandFenceFromSnapshot = (snapshot: ProviderJobState): ReadonlyMap<string, CommandFence> => {
  if (snapshot._tag !== 'Running' && snapshot._tag !== 'Completed' && snapshot._tag !== 'Failed') return new Map();
  return new Map([[snapshot.commandId, {
    commandId: snapshot.commandId,
    executionId: snapshot.executionId,
    executionEpoch: snapshot.executionEpoch,
    issuingRevision: snapshot._tag === 'Running' ? snapshot.expectedFence : snapshot.revision - 1,
  }]]);
};

const rowUpdateFromSnapshot = (
  snapshot: ProviderJobState,
  recordedAt: number,
  current: ProviderJobRow,
): Partial<typeof scanProviderJobs.$inferInsert> => {
  const at = new Date(recordedAt);
  switch (snapshot._tag) {
    case 'Queued':
      return {
        state: 'queued', machineVersion: snapshot.machineVersion, revision: snapshot.revision,
        attemptCount: snapshot.attemptCount, maxAttempts: snapshot.maxAttempts,
        nextAttemptAt: timestampDate(snapshot.nextAttemptAt), lockedBy: null, lockedUntil: null,
        machineCommandId: null, machineExecutionId: null, machineExecutionEpoch: null, resultRef: null,
        skipReason: null, failureReason: snapshot.lastFailureReason, retryable: null, startedAt: null,
        completedAt: null, updatedAt: at,
      };
    case 'Running':
      return {
        state: 'running', machineVersion: snapshot.machineVersion, revision: snapshot.revision,
        attemptCount: snapshot.attemptCount, maxAttempts: snapshot.maxAttempts,
        nextAttemptAt: null, lockedBy: snapshot.executionId, lockedUntil: timestampDate(snapshot.lockedUntil),
        machineCommandId: snapshot.commandId, machineExecutionId: snapshot.executionId,
        machineExecutionEpoch: snapshot.executionEpoch, resultRef: null, skipReason: null,
        failureReason: null, retryable: null, startedAt: timestampDate(snapshot.startedAt), completedAt: null, updatedAt: at,
      };
    case 'Completed':
      return {
        state: 'completed', machineVersion: snapshot.machineVersion, revision: snapshot.revision,
        machineCommandId: snapshot.commandId, machineExecutionId: snapshot.executionId,
        machineExecutionEpoch: snapshot.executionEpoch, resultRef: snapshot.resultRef,
        lockedBy: null, lockedUntil: null, nextAttemptAt: null, skipReason: null, failureReason: null,
        retryable: null, startedAt: current.startedAt, completedAt: at, updatedAt: at,
      };
    case 'Failed':
      return {
        state: 'failed', machineVersion: snapshot.machineVersion, revision: snapshot.revision,
        attemptCount: snapshot.attemptCount, maxAttempts: snapshot.maxAttempts,
        machineCommandId: snapshot.commandId, machineExecutionId: snapshot.executionId,
        machineExecutionEpoch: snapshot.executionEpoch, resultRef: null,
        lockedBy: null, lockedUntil: null, nextAttemptAt: null, skipReason: null,
        failureReason: snapshot.failureReason, retryable: false, startedAt: current.startedAt, completedAt: at, updatedAt: at,
      };
    case 'Skipped':
      return {
        state: 'skipped', machineVersion: snapshot.machineVersion, revision: snapshot.revision,
        machineCommandId: null, machineExecutionId: null, machineExecutionEpoch: null, resultRef: null,
        lockedBy: null, lockedUntil: null, nextAttemptAt: null, skipReason: snapshot.skipReason,
        failureReason: null, retryable: null, startedAt: current.startedAt, completedAt: at, updatedAt: at,
      };
  }
};

const receiptInsert = (receipt: DecisionReceipt): typeof scanProviderJobDecisionReceipts.$inferInsert => ({
  decisionId: receipt.decisionId,
  providerJobId: receipt.instanceId,
  machineId: receipt.machineId,
  machineVersion: receipt.machineVersion,
  revisionBefore: receipt.revisionBefore,
  revisionAfter: receipt.revisionAfter,
  eventId: receipt.eventId,
  eventTag: receipt.eventTag,
  idempotencyKey: receipt.idempotencyKey,
  fingerprint: receipt.fingerprint,
  outcome: receipt.outcome,
  rejectionCode: receipt.rejectionCode ?? null,
  stateTagBefore: receipt.replay.stateTagBefore,
  stateTagAfter: receipt.replay.stateTagAfter,
  commandTags: receipt.replay.commandTags,
  correlation: copyCorrelation(receipt.correlation),
  responseSchemaVersion: RESPONSE_SCHEMA_VERSION,
  recordedAt: new Date(receipt.recordedAt),
});

const outboxInsert = (
  envelope: OutboxEnvelope<ProviderJobCommand>,
  providerJobId: string,
  decisionId: string,
  recordedAt: number,
  inlineOwnedCommandTags: readonly string[],
): typeof scanProviderJobCommandOutbox.$inferInsert => ({
  outboxId: `${providerJobId}:${envelope.commandId}:${envelope.executionId}:${envelope.executionEpoch}:${envelope.issuingRevision}`,
  decisionId,
  providerJobId,
  commandId: envelope.commandId,
  executionId: envelope.executionId,
  executionEpoch: envelope.executionEpoch,
  issuingRevision: envelope.issuingRevision,
  commandTag: envelope.command._tag,
  commandPayload: envelope.command as Record<string, unknown>,
  correlation: copyCorrelation(envelope.correlation),
  deliveredAt: null,
  inlineOwnedAt: inlineOwnedCommandTags.includes(envelope.command._tag) || envelope.command._tag === 'ReopenScanRun'
    ? new Date(recordedAt)
    : null,
});

type ReceiptRow = typeof scanProviderJobDecisionReceipts.$inferSelect;

const receiptFromRow = (row: ReceiptRow): DecisionReceipt => ({
  decisionId: row.decisionId,
  instanceId: row.providerJobId,
  machineId: row.machineId,
  machineVersion: row.machineVersion,
  eventId: row.eventId,
  eventTag: row.eventTag,
  idempotencyKey: row.idempotencyKey,
  fingerprint: row.fingerprint,
  outcome: row.outcome,
  rejectionCode: row.rejectionCode ?? undefined,
  revisionBefore: row.revisionBefore,
  revisionAfter: row.revisionAfter,
  recordedAt: timestampNumber(row.recordedAt) ?? 0,
  responseSchemaVersion: row.responseSchemaVersion,
  correlation: resolveStoredCorrelation(row.correlation, row.providerJobId),
  replay: {
    outcome: row.outcome,
    stateTagBefore: row.stateTagBefore,
    stateTagAfter: row.stateTagAfter,
    rejectionCode: row.rejectionCode ?? undefined,
    commandTags: row.commandTags,
  },
});

const timestampNumber = (value: Date | string | null): number | null => value == null ? null : new Date(value).getTime();
const timestampDate = (value: number | null): Date | null => value == null ? null : new Date(value);

const runDatabaseEffect = <A>(thunk: () => Promise<A>): Effect.Effect<A, InstanceNotFound | TechnicalFailure> =>
  Effect.tryPromise({
    try: thunk,
    catch: (cause) => cause instanceof InstanceNotFound
      ? cause
      : new TechnicalFailure({ cause: cause instanceof Error ? cause.message : String(cause) }),
  });
