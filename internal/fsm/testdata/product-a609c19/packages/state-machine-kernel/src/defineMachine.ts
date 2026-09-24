import { Decision } from './decision';
import { MachineDefinitionError } from './machineDefinitionError';
import type { DefineMachineInput, MachineDefinition, Tagged, TransitionRule } from './types';

const hasTag = <Tag extends string>(tags: ReadonlyArray<Tag>, tag: string): tag is Tag =>
  tags.some((candidate) => candidate === tag);

const isPlainObject = (value: object): boolean => {
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
};

const isArrayIndex = (key: string): boolean => {
  if (!/^\d+$/.test(key)) return false;
  const index = Number(key);
  return index <= 0xffffffff - 1 && String(index) === key;
};

const failNonPlain = (_value?: object): never => {
  throw new MachineDefinitionError({
    code: 'NonPlainMetadata',
    message: 'Definition metadata must be plain data, arrays, sets, or functions.',
  });
};

const requireDataDescriptor = (value: object, key: PropertyKey): PropertyDescriptor => {
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  if (!descriptor || descriptor.get || descriptor.set) {
    return failNonPlain(value);
  }
  return descriptor;
};

const ownDataValue = <T>(value: T, active: WeakSet<object>, memo: WeakMap<object, object>): T =>
  typeof value === 'function' ? value : deepOwn(value, active, memo);

const defineOwnedProperty = (
  target: object,
  key: PropertyKey,
  descriptor: PropertyDescriptor,
  active: WeakSet<object>,
  memo: WeakMap<object, object>,
): void => {
  Object.defineProperty(target, key, {
    value: ownDataValue(descriptor.value, active, memo),
    enumerable: descriptor.enumerable,
    writable: false,
    configurable: false,
  });
};

const ownSet = <T>(values: Set<T>, active: WeakSet<object>, memo: WeakMap<object, object>): ReadonlySet<T> => {
  const items: T[] = [];
  const view = {
    has(value: T) {
      return items.includes(value);
    },
    get size() {
      return items.length;
    },
    keys() {
      return items.values();
    },
    values() {
      return items.values();
    },
    entries() {
      return items.map((item) => [item, item] as [T, T])[Symbol.iterator]();
    },
    forEach(callback: (value: T, value2: T, set: ReadonlySet<T>) => void, thisArg?: unknown) {
      for (const item of items) {
        callback.call(thisArg, item, item, view as ReadonlySet<T>);
      }
    },
    [Symbol.iterator]() {
      return items.values();
    },
    [Symbol.toStringTag]: 'Set',
  };
  for (const key of Object.getOwnPropertyNames(values)) {
    requireDataDescriptor(values, key);
  }
  for (const key of Object.getOwnPropertySymbols(values)) {
    requireDataDescriptor(values, key);
  }
  Set.prototype.forEach.call(values, (item: T) => {
    items.push(ownDataValue(item, active, memo));
  });
  Object.freeze(items);
  const frozen = Object.freeze(view) as ReadonlySet<T>;
  memo.set(values, frozen);
  return frozen;
};

function deepOwn<T>(
  value: T,
  active: WeakSet<object> = new WeakSet(),
  memo: WeakMap<object, object> = new WeakMap(),
): T {
  if (value === null || typeof value !== 'object') {
    return value;
  }
  if (typeof value === 'function') {
    return value;
  }
  if (active.has(value)) {
    failNonPlain(value);
  }
  const cached = memo.get(value);
  if (cached) return cached as T;
  if (value instanceof Date || value instanceof Map || (!Array.isArray(value) && !(value instanceof Set) && !isPlainObject(value))) {
    failNonPlain(value);
  }

  active.add(value);
  try {
    if (value instanceof Set) {
      return ownSet(value, active, memo) as T;
    }
    if (Array.isArray(value)) {
      const copy: unknown[] = [];
      for (let index = 0; index < value.length; index += 1) {
        const descriptor = requireDataDescriptor(value, String(index));
        copy[index] = ownDataValue(descriptor.value, active, memo);
      }
      for (const key of Object.getOwnPropertyNames(value)) {
        if (key === 'length' || isArrayIndex(key)) continue;
        defineOwnedProperty(copy, key, requireDataDescriptor(value, key), active, memo);
      }
      for (const key of Object.getOwnPropertySymbols(value)) {
        defineOwnedProperty(copy, key, requireDataDescriptor(value, key), active, memo);
      }
      const frozen = Object.freeze(copy) as T;
      memo.set(value, frozen as object);
      return frozen;
    }

    const prototype = Object.getPrototypeOf(value);
    const copy = prototype === null ? Object.create(null) : {};
    for (const key of Object.getOwnPropertyNames(value)) {
      defineOwnedProperty(copy, key, requireDataDescriptor(value, key), active, memo);
    }
    for (const key of Object.getOwnPropertySymbols(value)) {
      defineOwnedProperty(copy, key, requireDataDescriptor(value, key), active, memo);
    }
    const frozen = Object.freeze(copy) as T;
    memo.set(value, frozen as object);
    return frozen;
  } finally {
    active.delete(value);
  }
}

const REQUIRED_INPUT_KEYS = [
  'id',
  'version',
  'initialState',
  'stateTags',
  'eventTags',
  'terminalStateTags',
  'adminEventTags',
  'rules',
  'reject',
  'telemetry',
] as const;

const captureOwnData = <T extends object>(input: T): T => {
  if (input === null || typeof input !== 'object') {
    return failNonPlain();
  }
  const prototype = Object.getPrototypeOf(input);
  if (prototype !== Object.prototype && prototype !== null) {
    return failNonPlain(input);
  }
  const copy = Object.create(null);
  for (const key of REQUIRED_INPUT_KEYS) {
    const descriptor = requireDataDescriptor(input, key);
    Object.defineProperty(copy, key, {
      value: descriptor.value,
      enumerable: descriptor.enumerable,
      writable: false,
      configurable: false,
    });
  }
  for (const key of Object.getOwnPropertyNames(input)) {
    if ((REQUIRED_INPUT_KEYS as readonly string[]).includes(key)) continue;
    const descriptor = requireDataDescriptor(input, key);
    Object.defineProperty(copy, key, {
      value: descriptor.value,
      enumerable: descriptor.enumerable,
      writable: false,
      configurable: false,
    });
  }
  for (const key of Object.getOwnPropertySymbols(input)) {
    const descriptor = requireDataDescriptor(input, key);
    Object.defineProperty(copy, key, {
      value: descriptor.value,
      enumerable: descriptor.enumerable,
      writable: false,
      configurable: false,
    });
  }
  return copy as T;
};

export const defineMachine = <S extends Tagged, E extends Tagged, C, R>(
  input: DefineMachineInput<S, E, C, R>,
): MachineDefinition<S, E, C, R> => {
  const captured = captureOwnData(input);
  const reject = captured.reject;
  const id = captured.id;
  const version = captured.version;
  const active = new WeakSet<object>();
  const memo = new WeakMap<object, object>();
  const ownedRules = deepOwn(captured.rules, active, memo);
  const stateTags = deepOwn(captured.stateTags, active, memo);
  const eventTags = deepOwn(captured.eventTags, active, memo);
  const terminalStateTags = deepOwn(captured.terminalStateTags, active, memo);
  const adminEventTags = deepOwn(captured.adminEventTags, active, memo);
  const initialState = deepOwn(captured.initialState, active, memo);
  const telemetry = deepOwn(captured.telemetry, active, memo);

  const seenIds = new Set<string>();
  for (const rule of ownedRules) {
    if (seenIds.has(rule.id)) {
      throw new MachineDefinitionError({
        code: 'DuplicateRuleId',
        message: `Machine "${id}" declares duplicate rule id "${rule.id}".`,
        ruleIds: [rule.id],
      });
    }
    seenIds.add(rule.id);

    if (!hasTag(stateTags, rule.from) || !hasTag(stateTags, rule.to)) {
      throw new MachineDefinitionError({
        code: 'UnknownStateTag',
        message: `Machine "${id}" rule "${rule.id}" uses an unknown state tag.`,
        ruleIds: [rule.id],
      });
    }
    if (!hasTag(eventTags, rule.on)) {
      throw new MachineDefinitionError({
        code: 'UnknownEventTag',
        message: `Machine "${id}" rule "${rule.id}" uses an unknown event tag.`,
        ruleIds: [rule.id],
      });
    }
  }

  if (!hasTag(stateTags, initialState._tag)) {
    throw new MachineDefinitionError({
      code: 'UnknownInitialState',
      message: `Machine "${id}" initial state tag "${initialState._tag}" is not declared.`,
    });
  }

  for (const tag of terminalStateTags) {
    if (!hasTag(stateTags, tag)) {
      throw new MachineDefinitionError({
        code: 'UnknownTerminalState',
        message: `Machine "${id}" terminal state tag "${tag}" is not declared.`,
      });
    }
  }

  for (const tag of adminEventTags) {
    if (!hasTag(eventTags, tag)) {
      throw new MachineDefinitionError({
        code: 'UnknownAdminEvent',
        message: `Machine "${id}" admin event tag "${tag}" is not declared.`,
      });
    }
  }

  const rulesByFrom = new Map<string, Map<string, TransitionRule<S, E, C, R>[]>>();
  for (const rule of ownedRules) {
    let byOn = rulesByFrom.get(rule.from);
    if (!byOn) {
      byOn = new Map();
      rulesByFrom.set(rule.from, byOn);
    }
    const bucket = byOn.get(rule.on);
    if (bucket) {
      bucket.push(rule);
    } else {
      byOn.set(rule.on, [rule]);
    }
  }

  const decide = (snapshot: S, event: E): ReturnType<MachineDefinition<S, E, C, R>['decide']> => {
    const candidates = rulesByFrom.get(snapshot._tag)?.get(event._tag) ?? [];
    const applicable: TransitionRule<S, E, C, R>[] = [];
    let uniqueGuardRejection: R | undefined;
    let uniqueGuardRejectionCount = 0;

    for (const rule of candidates) {
      const passes = rule.guard ? rule.guard(snapshot, event) : true;
      if (passes) {
        applicable.push(rule);
        continue;
      }
      if (rule.guardRejection) {
        uniqueGuardRejection = rule.guardRejection(snapshot, event);
        uniqueGuardRejectionCount += 1;
      }
    }

    if (applicable.length > 1) {
      throw new MachineDefinitionError({
        code: 'AmbiguousTransition',
        message: `Machine "${id}" has ${applicable.length} applicable rules for ${snapshot._tag}/${event._tag}.`,
        ruleIds: applicable.map((rule) => rule.id),
        from: snapshot._tag,
        on: event._tag,
      });
    }

    if (applicable.length === 1) {
      const rule = applicable[0]!;
      const reduced = rule.reduce(snapshot, event);
      if (reduced.next._tag !== rule.to) {
        throw new MachineDefinitionError({
          code: 'ReducerStateMismatch',
          message: `Machine "${id}" rule "${rule.id}" declared to "${rule.to}" but reduced to "${reduced.next._tag}".`,
          ruleIds: [rule.id],
          from: snapshot._tag,
          on: event._tag,
        });
      }
      return Decision.Accepted({
        next: reduced.next,
        commands: Object.freeze([...reduced.commands]),
      });
    }

    if (candidates.length === 1 && uniqueGuardRejectionCount === 1) {
      return Decision.Rejected({
        current: snapshot,
        reason: uniqueGuardRejection as R,
      });
    }

    return Decision.Rejected({
      current: snapshot,
      reason: reject(snapshot, event),
    });
  };

  return Object.freeze({
    id,
    version,
    initialState,
    stateTags,
    eventTags,
    terminalStateTags,
    adminEventTags,
    rules: ownedRules,
    decide,
    telemetry,
  });
};
