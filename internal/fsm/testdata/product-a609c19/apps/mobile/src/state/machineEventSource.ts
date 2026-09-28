import type { MobileAppEvent } from './mobileAppMachine.types';

export {
  projectedInteractionTestID,
  projectedNodeEventSource,
} from './projectedEventSource';

/** Attach a UI initiator to a machine event. Omits `_source` when testID is empty. */
export function withMachineEventSource<E extends MobileAppEvent>(
  event: E,
  testID: string | undefined,
): E {
  if (typeof testID !== 'string' || testID.length === 0) return event;
  return { ...event, _source: { testID } };
}
