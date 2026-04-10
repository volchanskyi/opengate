/**
 * Run a promise as fire-and-forget, logging any rejection to the console.
 *
 * Use this for intentional fire-and-forget invocations (e.g. background
 * refreshes triggered from React components) where the caller has already
 * decided that the result does not need to be awaited but the error must
 * still be observed.
 *
 * Prefer this over the `void` operator: `void p` is flagged by SonarCloud's
 * `typescript:S3735`, while `p.catch(...)` is the idiomatic, lint-clean way
 * to mark a promise as handled. Errors handled inside the promise itself
 * (e.g. via store-level `apiAction` plumbing) still surface here as a
 * console error if anything else throws.
 */
export function fireAndForget(value: Promise<unknown> | void | undefined): void {
  if (value && typeof value.catch === 'function') {
    value.catch((err: unknown) => {
      console.error('Unhandled async error:', err);
    });
  }
}
