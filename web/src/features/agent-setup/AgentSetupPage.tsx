import { useEffect, useState } from 'react';
import { useUpdateStore } from '../../state/update-store';
import { useAuthStore } from '../../state/auth-store';

type Platform = 'linux/amd64' | 'linux/arm64';

export function AgentSetupPage() {
  const user = useAuthStore((s) => s.user);
  const manifests = useUpdateStore((s) => s.manifests);
  const enrollmentTokens = useUpdateStore((s) => s.enrollmentTokens);
  const isLoading = useUpdateStore((s) => s.isLoading);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const fetchEnrollmentTokens = useUpdateStore((s) => s.fetchEnrollmentTokens);
  const createEnrollmentToken = useUpdateStore((s) => s.createEnrollmentToken);
  const deleteEnrollmentToken = useUpdateStore((s) => s.deleteEnrollmentToken);

  const [platform, setPlatform] = useState<Platform>('linux/amd64');
  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [showManual, setShowManual] = useState(false);
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [tokenLabel, setTokenLabel] = useState('');
  const [tokenMaxUses, setTokenMaxUses] = useState(0);
  const [tokenExpiresHours, setTokenExpiresHours] = useState(24);

  useEffect(() => {
    fetchManifests();
    if (user?.is_admin) {
      fetchEnrollmentTokens();
    }
  }, [fetchManifests, fetchEnrollmentTokens, user?.is_admin]);

  const [os, arch] = platform.split('/');
  const manifest = manifests.find((m) => m.os === os && m.arch === arch);
  const activeToken = enrollmentTokens.find(
    (t) => new Date(t.expires_at) > new Date() && (t.max_uses === 0 || t.use_count < t.max_uses)
  );

  const serverUrl = window.location.origin;
  const installCommand = activeToken
    ? `curl -sL ${serverUrl}/api/v1/server/install.sh | sudo bash -s -- ${activeToken.token}`
    : null;

  const handleCopy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const handleCreateToken = async () => {
    await createEnrollmentToken({
      label: tokenLabel || 'Quick setup',
      max_uses: tokenMaxUses,
      expires_in_hours: tokenExpiresHours,
    });
    setShowTokenForm(false);
    setTokenLabel('');
    setTokenMaxUses(0);
    setTokenExpiresHours(24);
  };

  const handleDeleteToken = async (id: string) => {
    await deleteEnrollmentToken(id);
  };

  const isTokenExpired = (expiresAt: string) => new Date(expiresAt) <= new Date();
  const isTokenExhausted = (maxUses: number, useCount: number) =>
    maxUses > 0 && useCount >= maxUses;

  if (isLoading && manifests.length === 0) {
    return (
      <div className="max-w-3xl mx-auto p-6">
        <p className="text-gray-400">Loading...</p>
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto p-6">
      <h2 className="text-2xl font-bold mb-6">Add Device</h2>

      {/* Quick Install */}
      <section className="mb-8">
        <h3 className="text-lg font-semibold mb-3">Quick Install</h3>
        {installCommand ? (
          <div>
            <p className="text-sm text-gray-400 mb-2">
              Run this command on the target machine to install the OpenGate agent:
            </p>
            <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 flex items-start gap-3">
              <code className="flex-1 text-sm text-green-400 break-all font-mono">
                {installCommand}
              </code>
              <button
                onClick={() => handleCopy(installCommand, 'install')}
                className="px-3 py-1 bg-gray-700 hover:bg-gray-600 rounded text-sm whitespace-nowrap"
              >
                {copiedField === 'install' ? 'Copied!' : 'Copy'}
              </button>
            </div>
          </div>
        ) : user?.is_admin ? (
          <div className="bg-gray-800 border border-gray-700 rounded-lg p-4">
            <p className="text-sm text-gray-400 mb-3">
              Create an enrollment token to generate a one-liner install command.
            </p>
            <button
              onClick={() => setShowTokenForm(true)}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm"
            >
              Create Token
            </button>
          </div>
        ) : (
          <p className="text-sm text-gray-400">
            Ask your administrator to create an enrollment token for agent installation.
          </p>
        )}
      </section>

      {/* Enrollment Tokens (admin only) */}
      {user?.is_admin && (
        <section className="mb-8">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-lg font-semibold">Enrollment Tokens</h3>
            {!showTokenForm && (
              <button
                onClick={() => setShowTokenForm(true)}
                className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-sm"
              >
                New Token
              </button>
            )}
          </div>

          {showTokenForm && (
            <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 mb-4 space-y-3">
              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label htmlFor="token-label" className="block text-xs text-gray-400 mb-1">Label</label>
                  <input
                    id="token-label"
                    type="text"
                    value={tokenLabel}
                    onChange={(e) => setTokenLabel(e.target.value)}
                    placeholder="Quick setup"
                    className="w-full px-3 py-1.5 bg-gray-900 border border-gray-600 rounded text-sm text-white"
                  />
                </div>
                <div>
                  <label htmlFor="token-max-uses" className="block text-xs text-gray-400 mb-1">Max uses (0 = unlimited)</label>
                  <input
                    id="token-max-uses"
                    type="number"
                    min={0}
                    value={tokenMaxUses}
                    onChange={(e) => setTokenMaxUses(parseInt(e.target.value, 10) || 0)}
                    className="w-full px-3 py-1.5 bg-gray-900 border border-gray-600 rounded text-sm text-white"
                  />
                </div>
                <div>
                  <label htmlFor="token-expires" className="block text-xs text-gray-400 mb-1">Expires in (hours)</label>
                  <input
                    id="token-expires"
                    type="number"
                    min={1}
                    value={tokenExpiresHours}
                    onChange={(e) => setTokenExpiresHours(parseInt(e.target.value, 10) || 24)}
                    className="w-full px-3 py-1.5 bg-gray-900 border border-gray-600 rounded text-sm text-white"
                  />
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  onClick={handleCreateToken}
                  className="px-4 py-1.5 bg-blue-600 hover:bg-blue-500 rounded text-sm"
                >
                  Create
                </button>
                <button
                  onClick={() => setShowTokenForm(false)}
                  className="px-4 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-sm"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}

          {enrollmentTokens.length === 0 ? (
            <p className="text-sm text-gray-500">No enrollment tokens created yet.</p>
          ) : (
            <div className="space-y-2">
              {enrollmentTokens.map((t) => {
                const expired = isTokenExpired(t.expires_at);
                const exhausted = isTokenExhausted(t.max_uses, t.use_count);
                const inactive = expired || exhausted;

                return (
                  <div
                    key={t.id}
                    className={`bg-gray-800 border rounded-lg p-3 ${
                      inactive ? 'border-gray-700 opacity-60' : 'border-gray-600'
                    }`}
                  >
                    <div className="flex items-start justify-between gap-3 mb-2">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="text-sm font-medium text-white truncate">
                          {t.label || 'Untitled'}
                        </span>
                        {expired && (
                          <span className="px-1.5 py-0.5 bg-red-900/50 text-red-400 rounded text-xs">
                            Expired
                          </span>
                        )}
                        {exhausted && !expired && (
                          <span className="px-1.5 py-0.5 bg-yellow-900/50 text-yellow-400 rounded text-xs">
                            Exhausted
                          </span>
                        )}
                        {!inactive && (
                          <span className="px-1.5 py-0.5 bg-green-900/50 text-green-400 rounded text-xs">
                            Active
                          </span>
                        )}
                      </div>
                      <button
                        onClick={() => handleDeleteToken(t.id)}
                        className="px-2 py-0.5 text-red-400 hover:text-red-300 hover:bg-red-900/30 rounded text-xs"
                        title="Delete token"
                      >
                        Delete
                      </button>
                    </div>
                    <div className="flex items-center gap-2 mb-2">
                      <code className="flex-1 text-xs text-gray-300 font-mono bg-gray-900 px-2 py-1 rounded truncate">
                        {t.token}
                      </code>
                      <button
                        onClick={() => handleCopy(t.token, `token-${t.id}`)}
                        className="px-2 py-0.5 bg-gray-700 hover:bg-gray-600 rounded text-xs whitespace-nowrap"
                      >
                        {copiedField === `token-${t.id}` ? 'Copied!' : 'Copy'}
                      </button>
                    </div>
                    <div className="flex gap-4 text-xs text-gray-500">
                      <span>
                        Uses: {t.use_count}{t.max_uses > 0 ? ` / ${t.max_uses}` : ' (unlimited)'}
                      </span>
                      <span>
                        Expires: {new Date(t.expires_at).toLocaleString()}
                      </span>
                      <span>
                        Created: {new Date(t.created_at).toLocaleString()}
                      </span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </section>
      )}

      {/* Platform Selector */}
      <section className="mb-8">
        <h3 className="text-lg font-semibold mb-3">Platform</h3>
        <div className="flex gap-2">
          {(['linux/amd64', 'linux/arm64'] as Platform[]).map((p) => (
            <button
              key={p}
              onClick={() => setPlatform(p)}
              className={`px-4 py-2 rounded text-sm ${
                platform === p
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-800 text-gray-400 hover:text-white border border-gray-700'
              }`}
            >
              {p === 'linux/amd64' ? 'Linux x86_64' : 'Linux ARM64'}
            </button>
          ))}
        </div>

        {manifest ? (
          <div className="mt-3 text-sm text-gray-400">
            <p>
              Version: <span className="text-white">{manifest.version}</span>
            </p>
            <a
              href={manifest.url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-blue-400 hover:text-blue-300 underline"
            >
              Download binary
            </a>
          </div>
        ) : (
          <p className="mt-3 text-sm text-gray-500">
            No agent binaries published for this platform yet.
          </p>
        )}
      </section>

      {/* Install Script Download */}
      <section className="mb-8">
        <h3 className="text-lg font-semibold mb-3">Install Script</h3>
        <div className="flex items-center gap-3">
          <a
            href="/api/v1/server/install.sh"
            download="install.sh"
            className="px-4 py-2 bg-gray-800 hover:bg-gray-700 border border-gray-700 rounded text-sm"
          >
            Download install.sh
          </a>
          <span className="text-sm text-gray-500">
            Linux installer with auto-detection, enrollment, and systemd setup
          </span>
        </div>
      </section>

      {/* Manual Install */}
      <section className="mb-8">
        <button
          onClick={() => setShowManual(!showManual)}
          className="text-lg font-semibold flex items-center gap-2"
        >
          <span className={`text-sm transition-transform ${showManual ? 'rotate-90' : ''}`}>
            &#9654;
          </span>
          Manual Install
        </button>

        {showManual && (
          <div className="mt-4 space-y-4 text-sm text-gray-300">
            <div>
              <h4 className="font-medium text-white mb-1">1. Download the agent binary</h4>
              {manifest ? (
                <code className="block bg-gray-800 p-3 rounded text-green-400 font-mono text-xs">
                  curl -fLo mesh-agent {manifest.url} && chmod +x mesh-agent
                </code>
              ) : (
                <p className="text-gray-500">No binary available for {platform}.</p>
              )}
            </div>

            <div>
              <h4 className="font-medium text-white mb-1">2. Save the CA certificate</h4>
              <p className="text-gray-400">
                The CA certificate is delivered automatically during enrollment. For manual setup,
                obtain it from your administrator.
              </p>
            </div>

            <div>
              <h4 className="font-medium text-white mb-1">3. Run the agent</h4>
              <code className="block bg-gray-800 p-3 rounded text-green-400 font-mono text-xs">
                ./mesh-agent --server-addr YOUR_SERVER:9090 --server-ca /path/to/ca.pem --data-dir /var/lib/opengate-agent
              </code>
            </div>

            <div>
              <h4 className="font-medium text-white mb-1">4. Set up systemd service</h4>
              <p className="text-gray-400">
                The quick install script handles this automatically. For manual setup, create a
                systemd unit file at <code className="text-gray-300">/etc/systemd/system/mesh-agent.service</code>.
              </p>
            </div>
          </div>
        )}
      </section>

      {/* What happens next */}
      <section>
        <h3 className="text-lg font-semibold mb-3">What happens next</h3>
        <p className="text-sm text-gray-400">
          Once the agent is installed and running, it will connect to the server via QUIC and
          appear in your device list under the assigned group. You can then start remote sessions,
          manage the device, and push updates.
        </p>
      </section>
    </div>
  );
}
