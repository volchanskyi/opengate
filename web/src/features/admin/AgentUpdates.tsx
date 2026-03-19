import { useEffect, useState } from 'react';
import { useUpdateStore } from '../../state/update-store';
import { useToastStore } from '../../state/toast-store';
import { ManifestPublishForm } from './ManifestPublishForm';
import { ManifestList } from './ManifestList';

export function AgentUpdates() {
  const manifests = useUpdateStore((s) => s.manifests);
  const enrollmentTokens = useUpdateStore((s) => s.enrollmentTokens);
  const signingKey = useUpdateStore((s) => s.signingKey);
  const isLoading = useUpdateStore((s) => s.isLoading);
  const error = useUpdateStore((s) => s.error);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const fetchEnrollmentTokens = useUpdateStore((s) => s.fetchEnrollmentTokens);
  const fetchSigningKey = useUpdateStore((s) => s.fetchSigningKey);
  const publishManifest = useUpdateStore((s) => s.publishManifest);
  const pushUpdate = useUpdateStore((s) => s.pushUpdate);
  const createEnrollmentToken = useUpdateStore((s) => s.createEnrollmentToken);
  const deleteEnrollmentToken = useUpdateStore((s) => s.deleteEnrollmentToken);
  const addToast = useToastStore((s) => s.addToast);

  const [showPublishForm, setShowPublishForm] = useState(false);
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [tokenForm, setTokenForm] = useState({ label: '', max_uses: 0, expires_in_hours: 24 });
  const [pushResult, setPushResult] = useState<number | null>(null);
  const [revealedTokens, setRevealedTokens] = useState<Set<string>>(new Set());

  useEffect(() => {
    fetchManifests();
    fetchEnrollmentTokens();
    fetchSigningKey();
  }, [fetchManifests, fetchEnrollmentTokens, fetchSigningKey]);

  const handlePublish = async (form: { version: string; os: string; arch: string; url: string; sha256: string }) => {
    await publishManifest(form);
  };

  const handleCreateToken = async (e: React.SyntheticEvent) => {
    e.preventDefault();
    await createEnrollmentToken(tokenForm);
    setTokenForm({ label: '', max_uses: 0, expires_in_hours: 24 });
    setShowTokenForm(false);
  };

  const handlePush = async (version: string, os: string, arch: string) => {
    const count = await pushUpdate({ version, os, arch });
    if (count !== undefined) {
      setPushResult(count);
      addToast(`Update pushed to ${count} agent(s)`, 'success');
      setTimeout(() => setPushResult(null), 3000);
    }
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

  if (isLoading && manifests.length === 0 && enrollmentTokens.length === 0) {
    return <p className="text-gray-400">Loading...</p>;
  }

  return (
    <div className="space-y-8">
      <h2 className="text-xl font-bold">Agent Updates</h2>

      {error && (
        <div className="bg-red-900/30 border border-red-700 text-red-300 p-3 rounded text-sm">
          {error}
        </div>
      )}

      {pushResult !== null && (
        <div className="bg-green-900/30 border border-green-700 text-green-300 p-3 rounded text-sm">
          Update pushed to {pushResult} agent(s).
        </div>
      )}

      {/* Enrollment Tokens */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-lg font-semibold">Enrollment Tokens</h3>
          <button
            onClick={() => setShowTokenForm(!showTokenForm)}
            className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-sm"
          >
            Create Token
          </button>
        </div>

        {showTokenForm && (
          <form onSubmit={handleCreateToken} className="bg-gray-800 border border-gray-700 rounded-lg p-4 mb-4 space-y-3">
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
                      onClick={() => navigator.clipboard.writeText(t.token)}
                      className="text-blue-400 hover:text-blue-300 text-xs"
                    >
                      Copy
                    </button>
                    <button
                      onClick={() => deleteEnrollmentToken(t.id)}
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

      {/* Published Manifests */}
      <section>
        <ManifestPublishForm
          showPublishForm={showPublishForm}
          setShowPublishForm={setShowPublishForm}
          onPublish={handlePublish}
        />
        <ManifestList manifests={manifests} onPush={handlePush} />
      </section>

      {/* Signing Key */}
      {signingKey && (
        <section>
          <h3 className="text-lg font-semibold mb-3">Signing Key</h3>
          <div className="bg-gray-800 border border-gray-700 rounded-lg p-4">
            <p className="text-sm text-gray-400 mb-2">Ed25519 public key used to verify agent update signatures:</p>
            <code className="block bg-gray-900 border border-gray-700 rounded px-3 py-2 text-xs font-mono break-all">
              {signingKey}
            </code>
            <button
              type="button"
              onClick={() => {
                navigator.clipboard.writeText(signingKey);
                addToast('Signing key copied', 'info');
              }}
              className="mt-2 text-sm text-blue-400 hover:text-blue-300"
            >
              Copy to clipboard
            </button>
          </div>
        </section>
      )}
    </div>
  );
}
