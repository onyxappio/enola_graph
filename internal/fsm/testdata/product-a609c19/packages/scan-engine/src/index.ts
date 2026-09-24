// Coverage profiles
export {
  coverageProfiles,
  assertProfileSupportsSubjectType,
  getProvidersForProfile,
  supportedEmailSubjectTypes,
} from './coverageProfiles';
export type { SupportedEmailSubjectType } from './coverageProfiles';

// Pub/Sub
export {
  createPubSubScanEventPublisher,
  createScanRunRequestedEnvelope,
  publishScanRunRequestedEvent,
} from './pubsub';
export type { ScanEventPublisher, PublishMessage } from './pubsub';

// Repository
export { createScanEngineRepository, selectReusableMarketingFunnelRun } from './repository';
export { classifyProviderJobCommitFailure, createProviderJobRepository } from './providerJobRepository';
export type {
  ScanEngineRepository,
  ResolveAnonymousSubjectInput,
  CreateScanRunInput,
  RecentMarketingFunnelRun,
  ScanPublicationClaim,
  ReserveMarketingFunnelScanInput,
  AuthorizedMarketingScanResult,
} from './repository';
export type { ProviderJobMachineRepository, ProviderJobRepositoryOptions } from './providerJobRepository';
export {
  ProviderJobCommand,
  ProviderJobEvent,
  ProviderJobRejection,
  ProviderJobState,
  providerJobMachine,
} from './machines/providerJob';
export { providerJobRuntimeMachine } from './providerJobRuntime';
export {
  allocateLegacyExecutionToken,
  isLegacyExecutionToken,
  providerJobClaimCommandId,
  providerJobClaimFingerprint,
  providerJobClaimIdempotencyKey,
  providerJobAdminRetryEventId,
  providerJobAdminRetryFingerprint,
  providerJobAdminRetryIdempotencyKey,
  providerJobOutcomeEventId,
  providerJobOutcomeFingerprint,
  providerJobOutcomeIdempotencyKey,
  providerJobSkipEventId,
  providerJobSkipFingerprint,
  providerJobSkipIdempotencyKey,
  providerJobStaleReclaimEventId,
  providerJobStaleReclaimFingerprint,
  providerJobStaleReclaimIdempotencyKey,
  LEGACY_EXECUTION_TOKEN_PREFIX,
  PROVIDER_JOB_MACHINE_ID,
  PROVIDER_JOB_MACHINE_VERSION,
} from './providerJobClaimIdentity';

// Scan run creation
export {
  createProductScanRun,
  startMarketingFunnelScan,
  MarketingScanPublicationPendingError,
  MARKETING_SCAN_ACCESS_TOKEN_TTL_MS,
} from './createScanRun';
export type {
  CreateProductScanRunInput,
  CreateProductScanRunResult,
  ScanCrypto,
  StartMarketingFunnelScanInput,
  StartMarketingFunnelScanResult,
} from './createScanRun';
