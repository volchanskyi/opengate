import { describe, it, expect } from 'vitest';
import { isTokenExpired, isTokenExhausted, isTokenActive } from './token-status';

describe('isTokenExpired', () => {
  it('returns true for past date', () => {
    expect(isTokenExpired('2020-01-01T00:00:00Z')).toBe(true);
  });

  it('returns false for future date', () => {
    expect(isTokenExpired('2099-01-01T00:00:00Z')).toBe(false);
  });
});

describe('isTokenExhausted', () => {
  it('returns true when use_count >= max_uses', () => {
    expect(isTokenExhausted(5, 5)).toBe(true);
    expect(isTokenExhausted(5, 6)).toBe(true);
  });

  it('returns false when use_count < max_uses', () => {
    expect(isTokenExhausted(5, 4)).toBe(false);
  });

  it('returns false when max_uses is 0 (unlimited)', () => {
    expect(isTokenExhausted(0, 100)).toBe(false);
  });
});

describe('isTokenActive', () => {
  it('returns true for valid, non-exhausted token', () => {
    expect(isTokenActive('2099-01-01T00:00:00Z', 0, 0)).toBe(true);
    expect(isTokenActive('2099-01-01T00:00:00Z', 10, 5)).toBe(true);
  });

  it('returns false for expired token', () => {
    expect(isTokenActive('2020-01-01T00:00:00Z', 0, 0)).toBe(false);
  });

  it('returns false for exhausted token', () => {
    expect(isTokenActive('2099-01-01T00:00:00Z', 5, 5)).toBe(false);
  });
});
