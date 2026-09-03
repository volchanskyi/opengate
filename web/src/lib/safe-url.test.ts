import { describe, it, expect } from 'vitest';
import { safeExternalUrl } from './safe-url';

describe('safeExternalUrl', () => {
  it.each([
    'https://example.com/agent',
    'http://internal.lan:8080/agent',
    'https://example.com/agent?v=1#frag',
    '/api/v1/updates/download/agent',
    '/',
  ])('allows %s', (url) => {
    expect(safeExternalUrl(url)).toBe(url);
  });

  it.each([
    ['javascript scheme', 'javascript:alert(1)'],
    ['uppercase javascript scheme', 'JavaScript:alert(1)'],
    ['data scheme', 'data:text/html,<script>alert(1)</script>'],
    ['vbscript scheme', 'vbscript:msgbox(1)'],
    ['file scheme', 'file:///etc/passwd'],
    ['blob scheme', 'blob:https://example.com/uuid'],
    ['scheme-relative', '//evil.example/agent'],
    ['bare word', 'not-a-url'],
    ['empty', ''],
    ['whitespace only', '   '],
  ])('rejects %s', (_label, url) => {
    expect(safeExternalUrl(url)).toBeUndefined();
  });

  it.each([undefined, null])('rejects %s', (url) => {
    expect(safeExternalUrl(url)).toBeUndefined();
  });

  // Leading whitespace and control characters are a classic way to smuggle a
  // scheme past a naive startsWith check.
  it.each([
    ' javascript:alert(1)',
    '\njavascript:alert(1)',
    '\tjavascript:alert(1)',
  ])('rejects %s with leading whitespace', (url) => {
    expect(safeExternalUrl(url)).toBeUndefined();
  });

  // The URL parser treats a backslash as a slash for every scheme a browser
  // follows, so "/\evil.example" in an href leaves the app origin exactly as
  // "//evil.example" does. An administrator's manifest link is the field this
  // guard exists for, and it is the one an attacker with that field would
  // reach for.
  it.each([
    ['backslash after the leading slash', '/\\evil.example/agent'],
    ['two backslashes', '\\\\evil.example/agent'],
    ['backslash then slash', '\\/evil.example/agent'],
    ['slash then backslash then slash', '/\\/evil.example/agent'],
  ])('rejects %s', (_label, url) => {
    expect(safeExternalUrl(url)).toBeUndefined();
  });

  // A backslash inside the path is not a retarget — it resolves against the
  // app's own origin — so it stays a link.
  it('allows a backslash inside a same-origin path', () => {
    expect(safeExternalUrl('/api/v1/updates/agent\\v2')).toBe('/api/v1/updates/agent\\v2');
  });
});
