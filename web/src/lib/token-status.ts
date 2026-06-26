export function isTokenExpired(expiresAt: string): boolean {
  const expiry = new Date(expiresAt).getTime();
  // An unparseable expiry must never make a token look live (fail-safe).
  if (Number.isNaN(expiry)) return true;
  return expiry <= Date.now();
}

export function isTokenExhausted(maxUses: number, useCount: number): boolean {
  return maxUses > 0 && useCount >= maxUses;
}

export function isTokenActive(expiresAt: string, maxUses: number, useCount: number): boolean {
  return !isTokenExpired(expiresAt) && !isTokenExhausted(maxUses, useCount);
}
