import { Effect } from 'effect';
import {
  isAccepted,
  isRejected,
  type Decision as MachineDecision,
  type MachineDefinition,
  type Tagged,
} from '@onyx/state-machine-kernel';
import { CasExhausted, IdempotencyConflict, UncertainCommit, type MachineRuntimeError } from './errors';
import { IdGenerator, RuntimeClock, TracingHooks, type MachineRepository } from './ports';
import { copyCorrelation } from './correlation';
import type {
  CasPolicy,
  CommandFence,
  CommitIntent,
  CorrelationInput,
  DecisionReceipt,
  FencedCommandOutcome,
  InstanceRecord,
  OutboxEnvelope,
  ReplaySafeResult,
  RuntimeFenceReason,
  RuntimeMachine,
  StepRequest,
  StepResult,
} from './types';

const DEFAULT_CAS: CasPolicy = { maxAttempts: 3 };

type IntentSource<S extends Tagged, C extends Tagged, R> =
  | { readonly _tag: 'Decided'; readonly decision: MachineDecision<S, C, R> }
  | { readonly _tag: 'RejectedByRuntime'; readonly reason: RuntimeFenceReason };

const rejectionCodeOf = (reason: unknown): string | undefined => {
  if (reason && typeof reason === 'object' && '_tag' in reason && typeof reason._tag === 'string') {
    return reason._tag;
  }
  return undefined;
};

const logicalCommandId = (instanceId: string, command: Tagged): string => {
  if ('commandId' in command && typeof command.commandId === 'string') return command.commandId;
  return `${instanceId}:${command._tag}`;
};

const commandExecutionId = (command: Tagged): string | undefined =>
  'executionId' in command && typeof command.executionId === 'string' && command.executionId.length > 0
    ? command.executionId
    : undefined;

const commandExecutionEpoch = (command: Tagged): number | undefined =>
  'executionEpoch' in command &&
  typeof command.executionEpoch === 'number' &&
  Number.isInteger(command.executionEpoch) &&
  command.executionEpoch > 0
    ? command.executionEpoch
    : undefined;

const fenceMatches = (fence: CommandFence | undefined, outcome: FencedCommandOutcome): boolean =>
  Boolean(
    fence &&
      fence.commandId === outcome.commandId &&
      fence.executionId === outcome.executionId &&
      fence.executionEpoch === outcome.executionEpoch,
  );

export const isCompleteCommandOutcome = (
  outcome: Partial<FencedCommandOutcome> | undefined,
): outcome is FencedCommandOutcome =>
  Boolean(
    outcome &&
      typeof outcome.commandId === 'string' &&
      outcome.commandId.length > 0 &&
      typeof outcome.executionId === 'string' &&
      outcome.executionId.length > 0 &&
      typeof outcome.executionEpoch === 'number' &&
      Number.isFinite(outcome.executionEpoch),
  );

export const isStaleCommandOutcome = (
  record: InstanceRecord<unknown>,
  outcome: FencedCommandOutcome,
): boolean => !fenceMatches(record.commandFences.get(outcome.commandId), outcome);

const classifyRuntimeFence = (
  eventTag: string,
  outcomeEventTags: ReadonlySet<string>,
  commandOutcome: Partial<FencedCommandOutcome> | undefined,
  record: InstanceRecord<unknown>,
): RuntimeFenceReason | undefined => {
  if (!outcomeEventTags.has(eventTag)) return undefined;
  if (commandOutcome == null) return { _tag: 'MissingCommandOutcome' };
  if (!isCompleteCommandOutcome(commandOutcome)) return { _tag: 'PartialCommandOutcome' };
  if (isStaleCommandOutcome(record, commandOutcome)) return { _tag: 'StaleCommandOutcome' };
  return undefined;
};

const committedResult = <S extends Tagged, C extends Tagged, R>(
  source: IntentSource<S, C, R>,
  receipt: DecisionReceipt,
): StepResult<S, C, R> =>
  source._tag === 'RejectedByRuntime'
    ? { _tag: 'RejectedByRuntime', reason: source.reason, receipt }
    : { _tag: 'Decided', decision: source.decision };

export const step = <S extends Tagged, E extends Tagged, C extends Tagged, R>(
  machine: RuntimeMachine<S, E, C, R>,
  repository: MachineRepository<S, C>,
  request: StepRequest<E>,
  casPolicy: CasPolicy = DEFAULT_CAS,
): Effect.Effect<StepResult<S, C, R>, MachineRuntimeError, IdGenerator | RuntimeClock | TracingHooks> =>
  Effect.gen(function* () {
    const ids = yield* IdGenerator;
    const clock = yield* RuntimeClock;
    const tracing = yield* TracingHooks;
    let casAttempts = 0;

    const emit = (
      outcome: 'Accepted' | 'Rejected' | 'Replay',
      decisionId: string,
      correlation: CorrelationInput,
    ) =>
      tracing.onStep({
        instanceId: request.instanceId,
        decisionId,
        outcome,
        correlation: copyCorrelation(correlation),
      });

    while (true) {
      const record = yield* repository.load(request.instanceId);
      const existing = yield* repository.lookupReceipt({
        instanceId: request.instanceId,
        eventId: request.eventId,
        idempotencyKey: request.idempotencyKey,
      });
      if (existing) {
        if (existing.fingerprint !== request.fingerprint) {
          return yield* Effect.fail(
            new IdempotencyConflict({
              instanceId: request.instanceId,
              eventId: request.eventId,
              idempotencyKey: request.idempotencyKey,
            }),
          );
        }
        yield* emit('Replay', existing.decisionId, existing.correlation);
        return { _tag: 'Replayed', receipt: existing };
      }

      const runtimeFence = classifyRuntimeFence(
        request.event._tag,
        machine.outcomeEventTags,
        request.commandOutcome,
        record,
      );
      const source: IntentSource<S, C, R> = runtimeFence
        ? { _tag: 'RejectedByRuntime', reason: runtimeFence }
        : { _tag: 'Decided', decision: machine.definition.decide(record.snapshot, request.event) };
      const decisionId = yield* ids.next;
      const recordedAt = yield* clock.now;
      const intent = yield* buildIntent({
        definition: machine.definition,
        record,
        request,
        source,
        decisionId,
        recordedAt,
        ids,
      });
      const result = yield* repository.commit(intent);
      if (result._tag === 'IdempotencyConflict') {
        return yield* Effect.fail(
          new IdempotencyConflict({
            instanceId: request.instanceId,
            eventId: request.eventId,
            idempotencyKey: request.idempotencyKey,
          }),
        );
      }
      if (result._tag === 'Replayed') {
        yield* emit('Replay', result.receipt.decisionId, result.receipt.correlation);
        return { _tag: 'Replayed', receipt: result.receipt };
      }
      if (result._tag === 'Committed') {
        yield* emit(intent.receipt.replay.outcome, decisionId, record.correlation);
        return committedResult(source, intent.receipt);
      }
      if (result._tag === 'Uncertain') {
        const found = yield* repository.reconcile(decisionId);
        if (found) {
          yield* emit(found.replay.outcome === 'Accepted' ? 'Accepted' : 'Rejected', found.decisionId, found.correlation);
          return { _tag: 'Replayed', receipt: found };
        }
        const retry = yield* repository.commit(intent);
        if (retry._tag === 'IdempotencyConflict') {
          return yield* Effect.fail(
            new IdempotencyConflict({
              instanceId: request.instanceId,
              eventId: request.eventId,
              idempotencyKey: request.idempotencyKey,
            }),
          );
        }
        if (retry._tag === 'Replayed') {
          yield* emit('Replay', retry.receipt.decisionId, retry.receipt.correlation);
          return { _tag: 'Replayed', receipt: retry.receipt };
        }
        if (retry._tag === 'Committed') {
          yield* emit(intent.receipt.replay.outcome, decisionId, record.correlation);
          return committedResult(source, intent.receipt);
        }
        const foundAfterRetry = yield* repository.reconcile(decisionId);
        if (foundAfterRetry) {
          yield* emit(
            foundAfterRetry.replay.outcome === 'Accepted' ? 'Accepted' : 'Rejected',
            foundAfterRetry.decisionId,
            foundAfterRetry.correlation,
          );
          return { _tag: 'Replayed', receipt: foundAfterRetry };
        }
        return yield* Effect.fail(new UncertainCommit({ decisionId }));
      }

      casAttempts += 1;
      if (casAttempts >= casPolicy.maxAttempts) {
        return yield* Effect.fail(
          new CasExhausted({ instanceId: request.instanceId, attempts: casAttempts }),
        );
      }
    }
  });

const buildIntent = <S extends Tagged, E extends Tagged, C extends Tagged, R>(input: {
  readonly definition: MachineDefinition<S, E, C, R>;
  readonly record: InstanceRecord<S>;
  readonly request: StepRequest<E>;
  readonly source: IntentSource<S, C, R>;
  readonly decisionId: string;
  readonly recordedAt: number;
  readonly ids: { readonly next: Effect.Effect<string> };
}): Effect.Effect<CommitIntent<S, C>> =>
  Effect.gen(function* () {
    const correlation = copyCorrelation(input.record.correlation);
    const acceptedDecision =
      input.source._tag === 'Decided' && isAccepted(input.source.decision) ? input.source.decision : undefined;
    const nextSnapshot = acceptedDecision ? acceptedDecision.next : input.record.snapshot;
    const nextRevision = acceptedDecision ? input.record.revision + 1 : input.record.revision;
    const fences = new Map(input.record.commandFences);
    const outbox: OutboxEnvelope<C>[] = [];
    if (acceptedDecision) {
      for (const command of acceptedDecision.commands) {
        const commandId = logicalCommandId(input.record.instanceId, command);
        const previous = fences.get(commandId);
        const executionEpoch = commandExecutionEpoch(command) ?? (previous?.executionEpoch ?? 0) + 1;
        const executionId = commandExecutionId(command) ?? (yield* input.ids.next);
        fences.set(commandId, {
          commandId,
          executionId,
          executionEpoch,
          issuingRevision: nextRevision,
        });
        outbox.push({
          commandId,
          executionId,
          executionEpoch,
          issuingRevision: nextRevision,
          command,
          correlation: copyCorrelation(correlation),
        });
      }
    }
    const rejectionCode =
      input.source._tag === 'RejectedByRuntime'
        ? input.source.reason._tag
        : input.source._tag === 'Decided' && isRejected(input.source.decision)
          ? rejectionCodeOf(input.source.decision.reason)
          : undefined;
    const replay: ReplaySafeResult = {
      outcome: acceptedDecision ? 'Accepted' : 'Rejected',
      stateTagBefore: input.record.snapshot._tag,
      stateTagAfter: nextSnapshot._tag,
      rejectionCode,
      commandTags: acceptedDecision ? acceptedDecision.commands.map((command) => command._tag) : [],
    };
    const receipt: DecisionReceipt = {
      decisionId: input.decisionId,
      instanceId: input.record.instanceId,
      machineId: input.definition.id,
      machineVersion: input.definition.version,
      eventId: input.request.eventId,
      eventTag: input.request.event._tag,
      idempotencyKey: input.request.idempotencyKey,
      fingerprint: input.request.fingerprint,
      outcome: replay.outcome,
      rejectionCode: replay.rejectionCode,
      revisionBefore: input.record.revision,
      revisionAfter: nextRevision,
      recordedAt: input.recordedAt,
      responseSchemaVersion: 1,
      correlation: copyCorrelation(correlation),
      replay,
    };
    return {
      instanceId: input.record.instanceId,
      expectedRevision: input.record.revision,
      snapshot: nextSnapshot,
      revision: nextRevision,
      commandFences: fences,
      receipt,
      outbox,
    };
  });
