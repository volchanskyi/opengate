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
 * A value that leaves the app's origin without naming a scheme.
 *
 * Two leading slashes retarget to another host, and the URL parser treats a
 * backslash as a slash for every scheme a browser follows — so `/\host/…`,
 * `\\host/…` and `\/host/…` all reach `host` exactly as `//host/…` does. The
 * pair is what makes it a retarget; a single leading slash is a path on this
 * origin, and a backslash further along resolves against it too.
 */
const LEAVES_THE_ORIGIN = /^[/\\]{2}/;

/**
 * Return url when it is safe to place in an href/src, otherwise undefined.
 *
 * Same-origin relative paths ("/api/…") are allowed. Anything that leaves the
 * origin without a scheme, does not parse, or parses to a protocol outside the
 * allowlist is rejected — callers render plain text instead of a link.
 *
 * Whitespace needs no handling of its own: the parser strips leading and
 * trailing spaces before reading a scheme, so " javascript:…" is rejected on
 * its protocol, and a value that is nothing but whitespace parses to nothing
 * at all.
 */
export function safeExternalUrl(url: string | undefined | null): string | undefined {
  if (!url || LEAVES_THE_ORIGIN.test(url)) {
    return undefined;
  }
  // A relative path cannot carry a scheme, so it is safe by construction.
  if (url.startsWith('/')) {
    return url;
  }
  try {
    const parsed = new URL(url);
    return ALLOWED_PROTOCOLS.has(parsed.protocol) ? url : undefined;
  } catch {
    return undefined;
  }
}
