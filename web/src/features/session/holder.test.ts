import { describe, expect, it } from 'vitest';
import { holderLine } from './holder';

describe('holderLine', () => {
  it('names the holder and the time the session started', () => {
    expect(holderLine('Alice', '2026-10-12T14:02:00Z')).toMatch(/^Session open — Alice, since \d{1,2}:\d{2}/);
  });
});
