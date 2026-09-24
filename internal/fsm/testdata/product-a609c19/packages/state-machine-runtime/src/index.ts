export {
  CasExhausted,
  IdempotencyConflict,
  InstanceNotFound,
  TechnicalFailure,
  UncertainCommit,
} from './errors';
export type { MachineRuntimeError } from './errors';
export { createFakeRepository } from './fakeRepository';
export type { FakeMachineRepository, FakeRepositoryMode } from './fakeRepository';
export { fixedClockLayer, sequentialIdLayer, silentTracingLayer } from './layers';
export { IdGenerator, RuntimeClock, TracingHooks } from './ports';
export type { InterpreterConfig, MachineRepository } from './ports';
export { isCompleteCommandOutcome, isStaleCommandOutcome, step } from './step';
export {
  copyCorrelation,
  normalizeCorrelation,
  presentCorrelation,
  resolveStoredCorrelation,
  unavailableCorrelation,
} from './correlation';
export { CORRELATION_FIELDS, defineRuntimeMachine } from './types';
export type {
  CasPolicy,
  CommandFence,
  CommitIntent,
  CommitResult,
  CorrelationContext,
  CorrelationField,
  CorrelationInput,
  CorrelationPresence,
  DecisionReceipt,
  FencedCommandOutcome,
  InstanceRecord,
  LegacySixStringCorrelation,
  OutboxEnvelope,
  ReceiptQuery,
  ReplaySafeResult,
  RuntimeFenceReason,
  RuntimeMachine,
  StepRequest,
  StepResult,
} from './types';
