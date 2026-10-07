/** Runs a promise without awaiting it and logs any rejection to the console. */
export function fireAndForget(value: Promise<unknown> | void | undefined): void {
  if (value && typeof value.catch === 'function') {
    value.catch((err: unknown) => {
      console.error('Unhandled async error:', err);
    });
  }
}
