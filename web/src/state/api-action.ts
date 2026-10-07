interface ApiSuccess<T> {
  ok: true;
  data: T;
}

interface ApiFailure {
  ok: false;
}

type ApiResult<T> = ApiSuccess<T> | ApiFailure;

// `error` holds the parsed JSON error body only when the server sent one.
interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

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

export interface Progress {
  isLoading?: boolean;
  error?: string | null;
}

/**
 * Maps apiAction's progress reports onto a store whose slots have other names.
 * A field the call did not report is not forwarded, so "unchanged" differs from "cleared".
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
 * Wraps an API call with loading and error state; returns `{ ok: true, data }` or `{ ok: false }`.
 * Failure also keys off `response.ok`, since openapi-fetch leaves `error` unset for non-JSON bodies.
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
