import { describe, it, expect } from 'vitest';
import fc from 'fast-check';
import { isTokenExpired, isTokenExhausted, isTokenActive } from './token-status';

// Pinned runs + seed so any counterexample reproduces deterministically in the
// gauntlet (tests-determinism.md). No .skip / .only.
const RUNS = { numRuns: 500, seed: 0x0ac17a7e } as const;

describe('token-status properties', () => {
  it('isTokenExpired never throws and always returns a boolean', () => {
    fc.assert(
      fc.property(fc.string(), (s) => {
        const result = isTokenExpired(s);
        expect(typeof result).toBe('boolean');
      }),
      RUNS,
    );
  });

  it('isTokenExpired treats an unparseable expiry as expired (fail-safe)', () => {
    // A malformed expiresAt must never make a token look live. Without a NaN
    // guard `new Date("garbage") <= new Date()` is false → fail-open.
    fc.assert(
      fc.property(
        fc.string().filter((s) => Number.isNaN(new Date(s).getTime())),
        (garbage) => {
          expect(isTokenExpired(garbage)).toBe(true);
        },
      ),
      RUNS,
    );
  });

  it('isTokenExpired agrees with a wall-clock comparison for valid ISO dates', () => {
    fc.assert(
      fc.property(fc.date({ noInvalidDate: true }), (d) => {
        const iso = d.toISOString();
        expect(isTokenExpired(iso)).toBe(d.getTime() <= Date.now());
      }),
      RUNS,
    );
  });

  it('isTokenExhausted: maxUses <= 0 is always unlimited; otherwise iff useCount >= maxUses', () => {
    fc.assert(
      fc.property(fc.integer(), fc.integer(), (maxUses, useCount) => {
        const result = isTokenExhausted(maxUses, useCount);
        expect(typeof result).toBe('boolean');
        if (maxUses <= 0) {
          expect(result).toBe(false);
        } else {
          expect(result).toBe(useCount >= maxUses);
        }
      }),
      RUNS,
    );
  });

  it('isTokenActive ⟺ not expired AND not exhausted', () => {
    fc.assert(
      fc.property(
        fc.oneof(
          fc.string(),
          fc.date({ noInvalidDate: true }).map((d) => d.toISOString()),
        ),
        fc.integer(),
        fc.integer(),
        (expiresAt, maxUses, useCount) => {
          const expected =
            !isTokenExpired(expiresAt) && !isTokenExhausted(maxUses, useCount);
          expect(isTokenActive(expiresAt, maxUses, useCount)).toBe(expected);
        },
      ),
      RUNS,
    );
  });

  // Counterexample kept as an explicit case so it re-runs without fast-check.
  it('regression: empty / garbage expiry is inactive even with uses remaining', () => {
    expect(isTokenActive('', 0, 0)).toBe(false);
    expect(isTokenActive('not-a-date', 10, 0)).toBe(false);
  });
});
