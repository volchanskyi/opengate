/** "Session open — Alice, since 14:02": who holds a host and since when, in this browser's time. */
export function holderLine(holderName: string, startedAt: string): string {
  const since = new Date(startedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return `Session open — ${holderName}, since ${since}`;
}
