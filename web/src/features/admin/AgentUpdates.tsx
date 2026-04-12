import { useEffect, useState } from 'react';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
import { isTokenExpired, isTokenExhausted } from '../../lib/token-status';
import { fireAndForget } from '../../lib/fire-and-forget';

export function AgentUpdates() {
  const enrollmentTokens = useUpdateStore((s) => s.enrollmentTokens);
  const isLoading = useUpdateStore((s) => s.isLoading);
  const error = useUpdateStore((s) => s.error);
  const fetchEnrollmentTokens = useUpdateStore((s) => s.fetchEnrollmentTokens);
  const createEnrollmentToken = useUpdateStore((s) => s.createEnrollmentToken);
  const deleteEnrollmentToken = useUpdateStore((s) => s.deleteEnrollmentToken);
  const cleanupInactiveTokens = useUpdateStore((s) => s.cleanupInactiveTokens);
  const addToast = useToastStore((s) => s.addToast);

  const [showTokenForm, setShowTokenForm] = useState(false);
  const [tokenForm, setTokenForm] = useState({ label: '', max_uses: 0, expires_in_hours: 24 });
  const [revealedTokens, setRevealedTokens] = useState<Set<string>>(new Set());
  const [confirmCleanup, setConfirmCleanup] = useState(false);
  const [cleaningUp, setCleaningUp] = useState(false);

  const inactiveCount = enrollmentTokens.filter(
    (t) => isTokenExpired(t.expires_at) || isTokenExhausted(t.max_uses, t.use_count),
  ).length;

  useEffect(() => {
    fireAndForget(fetchEnrollmentTokens());
  }, [fetchEnrollmentTokens]);

  let cleanupButtonLabel = `Cleanup Tokens (${inactiveCount})`;
  if (cleaningUp) cleanupButtonLabel = 'Cleaning...';
  else if (confirmCleanup) cleanupButtonLabel = `Confirm (${inactiveCount})`;

  const handleCleanup = async () => {
    if (!confirmCleanup) {
      setConfirmCleanup(true);
      return;
    }
    setCleaningUp(true);
    const count = await cleanupInactiveTokens();
    addToast(`Removed ${count} inactive token${count !== 1 ? 's' : ''}`, 'success');
    setCleaningUp(false);
    setConfirmCleanup(false);
  };

  const handleCreateToken = async (e: React.SyntheticEvent) => {
    e.preventDefault();
    await createEnrollmentToken(tokenForm);
    setTokenForm({ label: '', max_uses: 0, expires_in_hours: 24 });
    setShowTokenForm(false);
  };

  const toggleReveal = (id: string) => {
    setRevealedTokens((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const maskToken = (token: string) => token.slice(0, 8) + '...' + token.slice(-4);

  if (isLoading && enrollmentTokens.length === 0) {
    return <p className="text-gray-400">Loading...</p>;
  }

  return (
    <div className="space-y-8">
      <h2 className="text-xl font-bold">Agent Settings</h2>

      {error && (
        <div className="bg-red-900/30 border border-red-700 text-red-300 p-3 rounded text-sm">
          {error}
        </div>
      )}

      {/* Enrollment Tokens */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-lg font-semibold">Enrollment Tokens</h3>
          <div className="flex items-center gap-2">
            {inactiveCount > 0 && (
              <button
                onClick={() => { fireAndForget(handleCleanup()); }}
                disabled={cleaningUp}
                className="px-3 py-1 bg-red-600 hover:bg-red-500 disabled:opacity-50 rounded text-sm"
              >
                {cleanupButtonLabel}
              </button>
            )}
            <button
              onClick={() => setShowTokenForm(!showTokenForm)}
              className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-sm"
            >
              Create Token
            </button>
          </div>
        </div>

        {showTokenForm && (
          <form onSubmit={(e) => { fireAndForget(handleCreateToken(e)); }} className="bg-gray-800 border border-gray-700 rounded-lg p-4 mb-4 space-y-3">
            <div>
              <label htmlFor="token-label" className="block text-sm text-gray-400 mb-1">Label</label>
              <input
                id="token-label"
                type="text"
                value={tokenForm.label}
                onChange={(e) => setTokenForm({ ...tokenForm, label: e.target.value })}
                className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
                placeholder="e.g. Production rollout"
              />
            </div>
            <div className="flex gap-4">
              <div className="flex-1">
                <label htmlFor="token-max-uses" className="block text-sm text-gray-400 mb-1">Max Uses (0 = unlimited)</label>
                <input
                  id="token-max-uses"
                  type="number"
                  value={tokenForm.max_uses}
                  onChange={(e) => setTokenForm({ ...tokenForm, max_uses: Number.parseInt(e.target.value) || 0 })}
                  className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
                  min={0}
                />
              </div>
              <div className="flex-1">
                <label htmlFor="token-expires-hours" className="block text-sm text-gray-400 mb-1">Expires In (hours)</label>
                <input
                  id="token-expires-hours"
                  type="number"
                  value={tokenForm.expires_in_hours}
                  onChange={(e) => setTokenForm({ ...tokenForm, expires_in_hours: Number.parseInt(e.target.value) || 24 })}
                  className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
                  min={1}
                />
              </div>
            </div>
            <button type="submit" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm">
              Create
            </button>
          </form>
        )}

        {enrollmentTokens.length === 0 ? (
          <p className="text-sm text-gray-500">No enrollment tokens yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700 text-left text-gray-400">
                <th className="pb-2">Label</th>
                <th className="pb-2">Status</th>
                <th className="pb-2">Token</th>
                <th className="pb-2">Uses</th>
                <th className="pb-2">Expires</th>
                <th className="pb-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {enrollmentTokens.map((t) => (
                <tr key={t.id} className="border-b border-gray-800">
                  <td className="py-2">{t.label || '\u2014'}</td>
                  <td className="py-2">
                    {isTokenExpired(t.expires_at) ? (
                      <span className="px-1.5 py-0.5 bg-red-900/50 text-red-400 rounded text-xs">Expired</span>
                    ) : isTokenExhausted(t.max_uses, t.use_count) ? (
                      <span className="px-1.5 py-0.5 bg-yellow-900/50 text-yellow-400 rounded text-xs">Exhausted</span>
                    ) : (
                      <span className="px-1.5 py-0.5 bg-green-900/50 text-green-400 rounded text-xs">Active</span>
                    )}
                  </td>
                  <td className="py-2 font-mono text-xs">
                    <button onClick={() => toggleReveal(t.id)} className="text-gray-300 hover:text-white">
                      {revealedTokens.has(t.id) ? t.token : maskToken(t.token)}
                    </button>
                  </td>
                  <td className="py-2">
                    {t.use_count}{t.max_uses > 0 ? `/${t.max_uses}` : ''}
                  </td>
                  <td className="py-2 text-xs">{new Date(t.expires_at).toLocaleString()}</td>
                  <td className="py-2 flex gap-2">
                    <button
                      onClick={() => { fireAndForget(navigator.clipboard.writeText(t.token)); }}
                      className="text-blue-400 hover:text-blue-300 text-xs"
                    >
                      Copy
                    </button>
                    <button
                      onClick={() => { fireAndForget(deleteEnrollmentToken(t.id)); }}
                      className="text-red-400 hover:text-red-300 text-xs"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

    </div>
  );
}
