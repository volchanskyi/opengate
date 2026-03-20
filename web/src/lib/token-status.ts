export function isTokenExpired(expiresAt: string): boolean {
  return new Date(expiresAt) <= new Date();
}

export function isTokenExhausted(maxUses: number, useCount: number): boolean {
  return maxUses > 0 && useCount >= maxUses;
}

export function isTokenActive(expiresAt: string, maxUses: number, useCount: number): boolean {
  return !isTokenExpired(expiresAt) && !isTokenExhausted(maxUses, useCount);
}
