/**
 * The console's forged-request value and idle clock. Both live in memory only: the session id
 * itself travels in an HttpOnly cookie no script can read.
 */

/** Names the signed-in account and tenant, never a credential, so other tabs notice a sign-out. */
export const SIGNED_IN_KEY = 'signed-in-as';

let csrf: string | null = null;
let lastRequestAt = Date.now();

/** Holds the forged-request value the sign-in returned, or clears it on sign-out. */
export function setCsrfToken(value: string | null): void {
  csrf = value;
}

/** The forged-request value every request that changes something carries. */
export function csrfToken(): string | null {
  return csrf;
}

/** Records a request, which renews the server's idle clock. */
export function noteRequest(at: number = Date.now()): void {
  lastRequestAt = at;
}

/** When the console last sent a request. */
export function lastRequest(): number {
  return lastRequestAt;
}

/** The identity a sign-in names, as the other tabs compare it. */
export function identityOf(userId: string, tenantId: string): string {
  return `${userId}/${tenantId}`;
}
