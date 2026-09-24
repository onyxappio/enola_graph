import type {
  GetStartedQuizAnswers,
  Q1Motivation,
  Q2PhoneUse,
  Q3PayOnline,
  Q4Exposed,
  Q5TapLinks,
  Q6MoneyApps,
} from './getStartedExposureModel';

/**
 * GetStartedFree runtime state.
 *
 * Source of truth: team-memory spec `onboarding/getstarted-free.md` (v1 free
 * onboarding) + `onboarding/getstarted-exposure-logic.md` (quiz scoring).
 * Pre-auth flow: quiz -> estimated exposure -> create a free account -> a
 * leaks-only scan -> land in the (free) Protect tab.
 */
export type GetStartedRuntimeLocation =
  | 'welcome'
  | 'socialProof'
  | 'riskUnknown'
  | 'q1'
  | 'q2'
  | 'q3'
  | 'q4'
  | 'q5'
  | 'q6'
  | 'analyzing'
  | 'result'
  | 'createAccount'
  | 'creatingAccount'
  | 'accountAllSet'
  | 'scan'
  | 'scanResult'
  | 'passwordLeaked'
  | 'leaksToMonitor'
  | 'noLeakFound';

export type GetStartedQuizQuestionKey = keyof GetStartedQuizAnswers;

export type GetStartedTimerPhase = 'analyzing' | 'scan' | 'selectionDwell';

/**
 * Perceptible dwell after a single-select quiz answer before auto-advance
 * (feedback_aaef7d51). Shared by live + scenario getStartedTimer services.
 *
 * Owner 2026-08-05 on the get-started quiz: «автопереход тут давай 1 секунду»
 * → 2s reduced to 1s. Owner 2026-08-15 (voice, T-315): «глобальна правка:
 * мені хочеться, щоб такі radio button варіанти з автопереходом переходили
 * швидше в рази два на наступний екран, бо зараз довго, як буцімто затримка»
 * → 1s reduced to 500ms.
 *
 * This number is the QUIZ auto-advance only. It no longer backs the Analyzing
 * 100% hold: that is GET_STARTED_ANALYZING_FINAL_DWELL_MS, decoupled in T-315
 * so halving this one cannot silently halve a hold the owner approved
 * separately. This is the ONLY auto-advance dwell in the get-started
 * quiz and it covers every single-select step at once (q3 «Do you pay for
 * things online?», q5, q6 — see isSingleSelectLocation). The multi-select steps
 * (q1, q2 «What do you use your phone for?», q4) never auto-advance: they wait
 * on Continue, so they are unaffected by this number.
 */
export const GET_STARTED_SELECTION_DWELL_MS = 500;

export type GetStartedRuntimeState = {
  location: GetStartedRuntimeLocation;
  answers: GetStartedQuizAnswers;
  email: string;
  password: string;
  message: string | null;
  /**
   * T-453: the registration attempt was refused because an account already
   * exists for the typed email. A RESPONSE state like `message` (set only by
   * the registerAccount effect result, never by typing/blur — probing an
   * email-existence endpoint before submit is an enumeration tool). Renders
   * the neutral "Couldn't create an account. Try logging in" helper under
   * Password with the fields left in their default (non-error) state.
   * Cleared when the email changes or a new submit starts.
   */
  accountExists: boolean;
  /**
   * T-453: the user left create-account through "Try logging in". The sign-in
   * back chevron (GET_STARTED_START) consumes this to return to the form with
   * everything typed preserved instead of rewinding the funnel to Welcome.
   */
  tryLoginReturn: boolean;
  /**
   * The user has asked to submit the WHOLE FORM at least once on this
   * create-account visit — i.e. pressed "Create account".
   *
   * Enter in a field is deliberately NOT this signal any more. It used to be,
   * and the result was the defect the owner hit on 2026-08-06: «коли я ввіла
   * тільки мейл то помилка світиться вже і на паролі» — Enter after the email
   * scolded a password field she had never reached. Enter is an exit from ONE
   * field, so it now raises that field's own gate below; only the CTA is a
   * statement about the form as a whole.
   *
   * WRITER: GetStartedFreeScreen, which owns this as per-visit view state and
   * passes it into `getStartedCreateAccountInputHelpers`. It is deliberately
   * NOT written by the interpreter: an invalid GET_STARTED_CREATE_ACCOUNT is
   * answered with `rejected()`, which returns the snapshot unchanged, so a
   * machine-side marker on the refusal path can never survive the refusal it
   * is supposed to explain (that is exactly how this flag stayed false forever
   * in the first T-217 round).
   *
   * Validation hints stay hidden until this flips (T-217, owner
   * 2026-08-06: «за пароль дивись я ще ввожу його а він вже пише помилку, має
   * показувтаи після ентеру» / «після бо коли людина ще не закінчила вводити в
   * неї вже помилка висить це неправильно»). It gates only WHEN a hint may
   * appear — never WHAT it says: the hints themselves are recomputed from the
   * current values, so a shown error clears the moment the value becomes valid
   * instead of freezing the message from the failed attempt.
   */
  submitAttempted?: boolean;
  /**
   * The user has FINISHED WITH this create-account field at least once on this
   * visit. Two ways to finish, both meaning "I am done here":
   *  - the field lost focus — keyboard dismissed, tap outside, tab away (owner
   *    2026-08-06: «як тільки інпут виходить зі стану активного… наприклад коли
   *    клавіатура закривається або тапає людина поза інпутом»);
   *  - Enter was pressed IN this field.
   *
   * Hence `Exited`, not `Blurred`: Enter is an exit too, and reading it as a
   * form-wide submit is exactly what lit up an untouched password field.
   *
   * PER FIELD on purpose: leaving the email must not light up the password the
   * user has not reached yet, so this cannot be one shared flag. Same writer and
   * same reason as `submitAttempted` (GetStartedFreeScreen view state), and the
   * same semantics: it gates only WHEN a hint may appear, never WHAT it says —
   * the hint is recomputed from the current value, so it clears as soon as the
   * value becomes valid even while the field is focused again.
   */
  emailExited?: boolean;
  passwordExited?: boolean;
  /**
   * Incremented on every single-select choice that arms selectionDwell.
   * Timer completions with a mismatched generation are stale (re-select / Back).
   */
  selectionDwellGeneration?: number;
};

const MULTI_SELECT_QUESTIONS: ReadonlySet<GetStartedQuizQuestionKey> = new Set([
  'q1Motivations',
  'q2PhoneUse',
  'q4Exposed',
]);

export function createEmptyQuizAnswers(): GetStartedQuizAnswers {
  // Truly empty: single-select questions carry NO default — seeding defaults
  // rendered q3/q5/q6 with a pre-selected chip on first arrival (T1).
  return {
    q1Motivations: [],
    q2PhoneUse: [],
    q3PayOnline: null,
    q4Exposed: [],
    q5TapLinks: null,
    q6MoneyApps: null,
  };
}

export function createGetStartedRuntimeState(
  overrides: Partial<GetStartedRuntimeState> = {},
): GetStartedRuntimeState {
  return {
    location: 'welcome',
    answers: createEmptyQuizAnswers(),
    email: '',
    password: '',
    message: null,
    accountExists: false,
    tryLoginReturn: false,
    submitAttempted: false,
    emailExited: false,
    passwordExited: false,
    ...overrides,
  };
}

/**
 * Record a submit attempt on a plain state value.
 *
 * NOTE (T-217): the live screen does NOT go through here — it holds the flag as
 * view state and merges it into the state it hands the helper (see
 * `submitAttempted` above). This stays as the pure, idempotent state-shape
 * writer for callers that own a `GetStartedRuntimeState` value directly.
 */
export function markGetStartedSubmitAttempted(state: GetStartedRuntimeState): GetStartedRuntimeState {
  return state.submitAttempted ? state : { ...state, submitAttempted: true };
}

export function isMultiSelectQuestion(question: GetStartedQuizQuestionKey): boolean {
  return MULTI_SELECT_QUESTIONS.has(question);
}

type MultiSelectValue<K extends GetStartedQuizQuestionKey> = K extends 'q1Motivations'
  ? Q1Motivation
  : K extends 'q2PhoneUse'
    ? Q2PhoneUse
    : K extends 'q4Exposed'
      ? Q4Exposed
      : never;

type SingleSelectValue<K extends GetStartedQuizQuestionKey> = K extends 'q3PayOnline'
  ? Q3PayOnline
  : K extends 'q5TapLinks'
    ? Q5TapLinks
    : K extends 'q6MoneyApps'
      ? Q6MoneyApps
      : never;

export function toggleQuizChoice<K extends 'q1Motivations' | 'q2PhoneUse' | 'q4Exposed'>(
  state: GetStartedRuntimeState,
  question: K,
  value: MultiSelectValue<K>,
): GetStartedRuntimeState {
  const current = state.answers[question] as MultiSelectValue<K>[];
  const next = current.includes(value)
    ? current.filter((item) => item !== value)
    : [...current, value];
  return {
    ...state,
    answers: { ...state.answers, [question]: next },
    message: null,
  };
}

export function setQuizChoice<K extends 'q3PayOnline' | 'q5TapLinks' | 'q6MoneyApps'>(
  state: GetStartedRuntimeState,
  question: K,
  value: SingleSelectValue<K>,
): GetStartedRuntimeState {
  return {
    ...state,
    answers: { ...state.answers, [question]: value },
    message: null,
  };
}

export function isQuizQuestionAnswered(state: GetStartedRuntimeState, question: GetStartedQuizQuestionKey): boolean {
  if (isMultiSelectQuestion(question)) {
    return (state.answers[question] as unknown[]).length > 0;
  }
  // Single-select questions require a real user selection (no defaults).
  return state.answers[question] != null;
}

export function isValidGetStartedEmail(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
}

export function isValidGetStartedPassword(password: string): boolean {
  return password.length >= 8;
}

export function canSubmitGetStartedAccount(state: GetStartedRuntimeState): boolean {
  return (
    isValidGetStartedEmail(state.email)
    && isValidGetStartedPassword(state.password)
    && state.location !== 'creatingAccount'
  );
}

const Q1_LABELS: Record<string, Q1Motivation> = {
  'My data is exposed online': 'dataExposed',
  'Stop scam calls & texts': 'stopScams',
  'Protect my family': 'protectFamily',
  'Stop identity theft': 'stopIdentityTheft',
  'Just want to feel safer': 'feelSafer',
};

const Q2_LABELS: Record<string, Q2PhoneUse> = {
  Email: 'email',
  Shopping: 'shopping',
  'Banking & payments': 'banking',
  Messaging: 'messaging',
  'Social media': 'social',
  Browsing: 'browsing',
};

const Q3_LABELS: Record<string, Q3PayOnline> = {
  'Not really': 'notReally',
  Sometimes: 'sometimes',
  'Most of the time': 'mostOfTheTime',
};

const Q4_LABELS: Record<string, Q4Exposed> = {
  Email: 'email',
  'Phone number': 'phone',
  'Home address': 'address',
  Passwords: 'passwords',
  'Card details': 'card',
  'Social Security number': 'ssn',
};

const Q5_LABELS: Record<string, Q5TapLinks> = {
  'I avoid them': 'avoid',
  Sometimes: 'sometimes',
  'Pretty often': 'prettyOften',
};

const Q6_LABELS: Record<string, Q6MoneyApps> = {
  Yes: 'yes',
  No: 'no',
  'Not sure': 'notSure',
};

export function selectedQuizChoiceLabels(
  state: GetStartedRuntimeState,
  location: GetStartedRuntimeLocation = state.location,
): string[] {
  switch (location) {
    case 'q1':
      return labelsForSelectedValues(Q1_LABELS, state.answers.q1Motivations);
    case 'q2':
      return labelsForSelectedValues(Q2_LABELS, state.answers.q2PhoneUse);
    case 'q3':
      return labelsForSelectedValues(Q3_LABELS, state.answers.q3PayOnline ? [state.answers.q3PayOnline] : []);
    case 'q4':
      return labelsForSelectedValues(Q4_LABELS, state.answers.q4Exposed);
    case 'q5':
      return labelsForSelectedValues(Q5_LABELS, state.answers.q5TapLinks ? [state.answers.q5TapLinks] : []);
    case 'q6':
      return labelsForSelectedValues(Q6_LABELS, state.answers.q6MoneyApps ? [state.answers.q6MoneyApps] : []);
    default:
      return [];
  }
}

/**
 * Apply a quiz OptionRow selection (identified by its visible label) to the
 * answers for the current question location. Multi-select toggles; single-select
 * replaces. Unknown labels are ignored.
 */
export function applyQuizSelection(
  state: GetStartedRuntimeState,
  location: GetStartedRuntimeLocation,
  label: string,
): GetStartedRuntimeState {
  switch (location) {
    case 'q1': {
      const value = Q1_LABELS[label];
      return value ? toggleQuizChoice(state, 'q1Motivations', value) : state;
    }
    case 'q2': {
      const value = Q2_LABELS[label];
      return value ? toggleQuizChoice(state, 'q2PhoneUse', value) : state;
    }
    case 'q4': {
      const value = Q4_LABELS[label];
      return value ? toggleQuizChoice(state, 'q4Exposed', value) : state;
    }
    case 'q3': {
      const value = Q3_LABELS[label];
      return value ? setQuizChoice(state, 'q3PayOnline', value) : state;
    }
    case 'q5': {
      const value = Q5_LABELS[label];
      return value ? setQuizChoice(state, 'q5TapLinks', value) : state;
    }
    case 'q6': {
      const value = Q6_LABELS[label];
      return value ? setQuizChoice(state, 'q6MoneyApps', value) : state;
    }
    default:
      return state;
  }
}

const QUIZ_ORDER: GetStartedRuntimeLocation[] = ['q1', 'q2', 'q3', 'q4', 'q5', 'q6'];

/** The next location after the given quiz question, or 'analyzing' after q6. */
export function nextQuizLocation(location: GetStartedRuntimeLocation): GetStartedRuntimeLocation {
  const index = QUIZ_ORDER.indexOf(location);
  if (index === -1 || index === QUIZ_ORDER.length - 1) return 'analyzing';
  return QUIZ_ORDER[index + 1];
}

/** Single-select questions auto-advance once a choice is selected. */
export function isSingleSelectLocation(location: GetStartedRuntimeLocation): boolean {
  return location === 'q3' || location === 'q5' || location === 'q6';
}

/**
 * Taxonomy step_name for onboarding_step_completed when leaving a quiz question.
 * Multi-select steps fire on CONTINUE; single-select on SELECT auto-advance.
 * Shared by web + native GetStartedFree (same interpreter).
 */
export function getStartedQuizStepName(
  location: GetStartedRuntimeLocation,
): `get_started_${'q1' | 'q2' | 'q3' | 'q4' | 'q5' | 'q6'}` | null {
  if (location === 'q1' || location === 'q2' || location === 'q3' || location === 'q4' || location === 'q5' || location === 'q6') {
    return `get_started_${location}`;
  }
  return null;
}

function labelsForSelectedValues<T extends string>(
  labelMap: Record<string, T>,
  selectedValues: readonly T[],
): string[] {
  const selected = new Set(selectedValues);
  return Object.entries(labelMap)
    .filter(([, value]) => selected.has(value))
    .map(([label]) => label);
}
