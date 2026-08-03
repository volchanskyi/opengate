/**
 * URL scheme allowlist for values that reach an href/src attribute.
 *
 * Server-stored URLs (agent manifest download links) are written by an
 * administrator and never scheme-checked on the way in, so the check belongs
 * here: a `javascript:` or `data:` value in an href executes in the app origin
 * the moment a user clicks it, turning a stored field into script execution.
 */

const ALLOWED_PROTOCOLS = new Set(['http:', 'https:']);

/**
 * Return url when it is safe to place in an href/src, otherwise undefined.
 *
 * Same-origin relative paths ("/api/…") are allowed; scheme-relative values
 * ("//host/…") are not, because they silently retarget to another origin.
 * Anything that does not parse, or parses to a protocol outside the allowlist,
 * is rejected — callers render plain text instead of a link.
 */
export function safeExternalUrl(url: string | undefined | null): string | undefined {
  if (!url) {
    return undefined;
  }
  const trimmed = url.trim();
  if (trimmed === '' || trimmed.startsWith('//')) {
    return undefined;
  }
  // A relative path cannot carry a scheme, so it is safe by construction.
  if (trimmed.startsWith('/')) {
    return trimmed;
  }
  try {
    const parsed = new URL(trimmed);
    return ALLOWED_PROTOCOLS.has(parsed.protocol) ? trimmed : undefined;
  } catch {
    return undefined;
  }
}
