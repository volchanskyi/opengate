// Stored URLs are not scheme-checked on the way in, and a `javascript:` or `data:` href
// runs in the app origin on click, so every href or src value passes this allowlist.

const ALLOWED_PROTOCOLS = new Set(['http:', 'https:']);

// Two leading slashes or backslashes, in any mix, retarget to another host; one is a local path.
const LEAVES_THE_ORIGIN = /^[/\\]{2}/;

/**
 * Returns url when it is a same-origin path or an http(s) URL, otherwise undefined.
 * The URL parser strips surrounding whitespace before reading the scheme, so " javascript:" fails.
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
