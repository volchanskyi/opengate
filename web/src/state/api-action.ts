type ErrorResponse = { error: string };

interface ApiSuccess<T> {
  ok: true;
  data: T;
}

interface ApiFailure {
  ok: false;
}

type ApiResult<T> = ApiSuccess<T> | ApiFailure;

/**
 * Wraps an API call with loading/error state management.
 * Pass `loading: false` for mutation actions that don't show a loading spinner.
 * Returns `{ ok: true, data }` on success, `{ ok: false }` on error.
 */
export async function apiAction<T>(
  set: (partial: { isLoading?: boolean; error?: string | null }) => void,
  fn: () => Promise<{ data?: T; error?: ErrorResponse }>,
  loading = true,
): Promise<ApiResult<T>> {
  set(loading ? { isLoading: true, error: null } : { error: null });
  const { data, error } = await fn();
  if (error) {
    set(loading ? { isLoading: false, error: error.error } : { error: error.error });
    return { ok: false };
  }
  if (loading) set({ isLoading: false });
  return { ok: true, data: data as T };
}
