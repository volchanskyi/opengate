// The profile's own numbers, offered by the browser-side generator.
// The walk is handed in as JSON and the scenarios build their executors from it.

/**
 * The profile's walk, as the projection handed it over.
 * A generator with no walk refuses at init, since a default would offer an undeclared shape.
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

// The most virtual users a phase preallocates; maxVUs allows twice as many.
const MAX_VIRTUAL_USERS = 500;

/** The phase the night's percentiles are taken over, or undefined. */
export function measuredPhase(walk) {
  return walk.find((phase) => phase.measured);
}

/** The tag every request in a phase carries, so percentiles are taken per phase. */
export function phaseTag(name) {
  return { phase: name };
}

/**
 * Arrival-rate scenarios, one per phase declaring arrivals, each starting where the last ended.
 * Arrivals keep coming when the server slows; `dropped_iterations` reports a generator behind.
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
        // Enough virtual users that the rate is met while each holds its own presented address.
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
 * Session scenarios, one per declared phase, each holding that phase's sessions open for its
 * duration, one session per virtual user.
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
 * How many virtual users a phase needs to keep offering its rate: arrivals a second times how
 * long one journey lasts, assuming a second per request.
 */
function virtualUsersFor(rate, journeysPerIteration) {
  return Math.min(MAX_VIRTUAL_USERS, Math.max(2, Math.ceil(rate * journeysPerIteration)));
}
/** k6 requires a scenario name that is an identifier. */
function scenarioName(phase) {
  return `phase_${String(phase).replace(/[^A-Za-z0-9]/g, "_")}`;
}

/**
 * Thresholds on the measured phase's own sub-metric, alongside the run-wide ones.
 * Naming a sub-metric in a threshold also puts it in the summary export.
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
