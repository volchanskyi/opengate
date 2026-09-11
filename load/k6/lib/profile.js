// The profile's own numbers, offered by the browser-side generator.
//
// `operator_arrivals_per_second` and `sessions` are technician-side facts: a
// journey a technician makes, a session a technician opens. The machine-side
// harness reads the profile and cannot offer either of them, and until this
// existed the browser-side scenarios carried shapes of their own — so a profile
// could declare fifteen journeys a second while the run offered twenty virtual
// users sleeping a second and a half between journeys, and nothing anywhere
// said the two disagreed. What varied the load was whichever file somebody
// happened to edit.
//
// So the walk is handed in, as JSON, and the scenario builds its executors from
// it. The profile is then the only home for the load as well as for the limits
// a night is judged against.

/**
 * The profile's walk, as the projection handed it over.
 *
 * A generator with no walk refuses at init rather than inventing one: a run
 * that offered a shape nobody declared is the defect this exists to close, and
 * a default here would be exactly that shape wearing a different name.
 */
export function phases() {
  const raw = __ENV.LOADTEST_PHASES;
  if (!raw) {
    throw new Error(
      "LOADTEST_PHASES is unset: the profile's walk is what this offers, and a generator that invents one measures a load nobody declared"
    );
  }
  const walk = JSON.parse(raw);
  if (!Array.isArray(walk) || walk.length === 0) {
    throw new Error(`LOADTEST_PHASES declares no phase: ${raw}`);
  }
  return walk;
}

/**
 * The most virtual users one generator will hold.
 *
 * A user costs memory whether or not it is busy, and the top of the capacity
 * ladder asks for a hundred and sixty journeys a second — which at a second a
 * request is nearly a thousand users in one process. The cap is declared here
 * rather than discovered as a generator that ran out of memory, and a phase
 * whose rate needs more than this says so in `dropped_iterations`, which the
 * profiles hold to a limit. That is the honest report: the generator could not
 * offer what the profile declared, rather than the target being slow.
 */
const MAX_VIRTUAL_USERS = 500;

/** The phase the night's percentiles are taken over, or undefined. */
export function measuredPhase(walk) {
  return walk.find((phase) => phase.measured);
}

/**
 * The tag every request in a phase carries. A percentile spanning the climb to
 * the load, the load itself and the wind-down away from it is a mixture of
 * three systems, and the mixture moves whenever the climb's share of the run
 * moves — a change nobody made to the product. Tagging by phase is what lets
 * the summary carry the load's own numbers rather than the mixture's.
 */
export function phaseTag(name) {
  return { phase: name };
}

/**
 * Arrival-rate scenarios, one per declared phase, each starting where the one
 * before it ended.
 *
 * Arrival rate rather than virtual users, because virtual users are a closed
 * loop: each waits for its own reply before asking again, so a server that has
 * slowed is offered less work and the latency it reports understates the
 * damage. An arrival rate keeps offering, and the generator that cannot keep up
 * says so in `dropped_iterations` — which is why that is a number the night is
 * judged on rather than a curiosity in a log.
 *
 * A phase declaring no arrivals contributes no scenario: a drain is a phase the
 * machines walk down through, and there is no technician work in it to offer.
 */
export function arrivalScenarios(walk, journeysPerIteration) {
  const scenarios = {};
  let startTime = 0;
  for (const phase of walk) {
    const rate = phase.arrivals_per_second;
    if (rate > 0) {
      scenarios[scenarioName(phase.name)] = {
        executor: "constant-arrival-rate",
        rate: Math.max(1, Math.round(rate)),
        timeUnit: "1s",
        duration: `${Math.round(phase.seconds)}s`,
        startTime: `${Math.round(startTime)}s`,
        // Enough virtual users that the rate is met while each holds its own
        // presented address. Over-allocating costs memory and nothing else;
        // under-allocating turns a rate the generator could have offered into
        // dropped iterations, which reads as the target being slow.
        preAllocatedVUs: virtualUsersFor(rate, journeysPerIteration),
        maxVUs: virtualUsersFor(rate, journeysPerIteration) * 2,
        tags: phaseTag(phase.name),
      };
    }
    startTime += phase.seconds;
  }
  if (Object.keys(scenarios).length === 0) {
    throw new Error("the profile's walk offers no technician load in any phase");
  }
  return scenarios;
}

/**
 * Session scenarios, one per declared phase, each holding that phase's sessions
 * open for its duration.
 *
 * This half stays user-based on purpose: the unit here is a live session, and
 * one session per virtual user is what "twenty sessions are open" means. An
 * arrival rate would describe sessions being *opened* per second, which is a
 * different question from how many are held.
 */
export function sessionScenarios(walk) {
  const scenarios = {};
  let startTime = 0;
  for (const phase of walk) {
    if (phase.sessions > 0) {
      scenarios[scenarioName(phase.name)] = {
        executor: "constant-vus",
        vus: phase.sessions,
        duration: `${Math.round(phase.seconds)}s`,
        startTime: `${Math.round(startTime)}s`,
        tags: phaseTag(phase.name),
      };
    }
    startTime += phase.seconds;
  }
  if (Object.keys(scenarios).length === 0) {
    throw new Error("the profile's walk holds no session open in any phase");
  }
  return scenarios;
}

/**
 * How many virtual users a phase needs to keep offering its rate.
 *
 * One iteration is one technician's journey. A journey holds its user for as
 * long as its requests take, so the users needed are the arrivals a second
 * times how long one journey lasts — and a journey against a healthy server is
 * milliseconds. The allowance below assumes a slow one, a second per request,
 * so a generator does not start dropping iterations the moment the target has a
 * bad minute; what it must never do is hide the target being slow, which is
 * what `dropped_iterations` is gated for.
 */
function virtualUsersFor(rate, journeysPerIteration) {
  return Math.min(MAX_VIRTUAL_USERS, Math.max(2, Math.ceil(rate * journeysPerIteration)));
}
/** k6 refuses a scenario name that is not an identifier. */
function scenarioName(phase) {
  return `phase_${String(phase).replace(/[^A-Za-z0-9]/g, "_")}`;
}

/**
 * Thresholds on the measured phase's own sub-metric, alongside the run-wide
 * ones. Naming a sub-metric in a threshold is also what puts it in the summary
 * export, which is how the row the trend keeps comes to hold the load's numbers
 * rather than the whole run's.
 */
export function measuredThresholds(walk, metrics) {
  const window = measuredPhase(walk);
  const thresholds = {};
  if (!window) {
    return thresholds;
  }
  for (const [metric, marks] of Object.entries(metrics)) {
    thresholds[`${metric}{phase:${window.name}}`] = marks;
  }
  return thresholds;
}
