import { test } from "@playwright/test";

/**
 * Records one reading beside the limit it is held to, as a test annotation the JSON report carries
 * into the nightly speed run's summary; recorded before the assertion, so a miss still shows.
 */
export function recordMeasurement(measurement: string, limit: string, reading: string, held: boolean): void {
  test.info().annotations.push({
    type: "measurement",
    description: JSON.stringify({ measurement, limit, reading, held }),
  });
}
