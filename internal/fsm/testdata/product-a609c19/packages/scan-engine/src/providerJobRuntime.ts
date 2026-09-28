import { defineRuntimeMachine } from '@onyx/state-machine-runtime';
import { providerJobMachine } from './machines/providerJob';

/** Shared interpreter boundary for ProviderJob transitions. */
export const providerJobRuntimeMachine = defineRuntimeMachine(
  providerJobMachine,
  new Set(['ProviderSucceeded', 'ProviderFailed']),
);
