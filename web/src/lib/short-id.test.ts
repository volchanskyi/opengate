import { describe, it, expect } from 'vitest';
import { shortId } from './short-id';

describe('shortId', () => {
  it('keeps the leading block a person actually reads', () => {
    expect(shortId('6f2b9c31-1111-2222-3333-444455556666')).toBe('6f2b9c31');
  });

  it('leaves an already-short value alone', () => {
    expect(shortId('abc')).toBe('abc');
  });

  it('renders nothing as a dash rather than an empty gap', () => {
    expect(shortId('')).toBe('—');
    expect(shortId(undefined)).toBe('—');
    expect(shortId(null)).toBe('—');
  });
});
