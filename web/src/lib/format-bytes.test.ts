import { describe, it, expect } from 'vitest';
import { formatBytes } from './format-bytes';

describe('formatBytes', () => {
  it('renders nothing as a plain byte count rather than "0.0 B"', () => {
    expect(formatBytes(0)).toBe('0 B');
  });

  it('renders a sub-kilobyte count in bytes', () => {
    expect(formatBytes(512)).toBe('512 B');
  });

  it('steps up one unit per power of 1024', () => {
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1024 ** 2)).toBe('1.0 MB');
    expect(formatBytes(1024 ** 3)).toBe('1.0 GB');
    expect(formatBytes(1024 ** 4)).toBe('1.0 TB');
  });

  it('keeps one decimal below 100 in a unit', () => {
    expect(formatBytes(1536)).toBe('1.5 KB');
  });

  it('drops the decimal from 100 up, where it is noise', () => {
    expect(formatBytes(150 * 1024)).toBe('150 KB');
  });

  it('clamps at the largest unit instead of running off the table', () => {
    expect(formatBytes(1024 ** 5)).toBe('1024 TB');
  });
});
