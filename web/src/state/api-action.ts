interface ApiSuccess<T> {
  ok: true;
  data: T;
}

interface ApiFailure {
  ok: false;
}

type ApiResult<T> = ApiSuccess<T> | ApiFailure;

/**
 * Subset of an openapi-fetch result we depend on. `response` is the raw HTTP
 * Response (always present in production); `error` is the parsed JSON error
 * body only when the server actually sent one.
 */
interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

/** Derive a user-facing message from whatever the server returned. */
function failureMessage(error: unknown, response?: Response): string {
  if (typeof error === 'string' && error.length > 0) {
    return error;
  }
  if (typeof error === 'object' && error !== null) {
    const body = (error as Record<string, unknown>).error;
    if (typeof body === 'string' && body.length > 0) {
      return body;
    }
  }
  if (response) {
    return `Request failed with status ${response.status}`;
  }
  return 'Request failed';
}

/** What [apiAction] reports back as a call starts and finishes. */
export interface Progress {
  isLoading?: boolean;
  error?: string | null;
}

/**
 * Adapt [apiAction]'s progress reports to a store that names its slots something
 * other than `isLoading` and `error` — one holding several independent reads
 * needs a slot per read, and a message routed to the wrong one reports a failed
 * evidence fetch as a failed queue read.
 *
 * A field the call did not report is not forwarded, so "unchanged" stays
 * distinct from "cleared". `onLoading` is optional: a store that shows no
 * spinner for a call has no flag to keep.
 */
export function progressAdapter(
  onError: (error: string | null) => void,
  onLoading?: (loading: boolean) => void,
): (progress: Progress) => void {
  return ({ isLoading, error }) => {
    if (isLoading !== undefined) onLoading?.(isLoading);
    if (error !== undefined) onError(error);
  };
}

/**
 * Wraps an API call with loading/error state management.
 * Pass `loading: false` for mutation actions that don't show a loading spinner.
 * Returns `{ ok: true, data }` on success, `{ ok: false }` on error.
 *
 * Failure is keyed off `response.ok`, not just a populated `error`. openapi-fetch
 * only fills in `error` when it can parse a JSON error body, so an error response
 * with an empty or non-JSON body (e.g. a bare 409) leaves `error` falsy — keying
 * off `error` alone would misread those as success.
 */
export async function apiAction<T>(
  set: (partial: { isLoading?: boolean; error?: string | null }) => void,
  fn: () => Promise<FetchResult<T>>,
  loading = true,
): Promise<ApiResult<T>> {
  set(loading ? { isLoading: true, error: null } : { error: null });
  const { data, error, response } = await fn();
  if (response?.ok === false || error != null) {
    const message = failureMessage(error, response);
    set(loading ? { isLoading: false, error: message } : { error: message });
    return { ok: false };
  }
  if (loading) set({ isLoading: false });
  return { ok: true, data: data as T };
}
