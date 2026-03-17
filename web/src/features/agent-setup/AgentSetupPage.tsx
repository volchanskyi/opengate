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

  const [platform, setPlatform] = useState<Platform>('linux/amd64');
  const [copied, setCopied] = useState(false);
  const [showManual, setShowManual] = useState(false);

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

  const handleCopy = async (text: string) => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCreateToken = async () => {
    await createEnrollmentToken({ label: 'Quick setup', max_uses: 0, expires_in_hours: 24 });
  };

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
                onClick={() => handleCopy(installCommand)}
                className="px-3 py-1 bg-gray-700 hover:bg-gray-600 rounded text-sm whitespace-nowrap"
              >
                {copied ? 'Copied!' : 'Copy'}
              </button>
            </div>
          </div>
        ) : user?.is_admin ? (
          <div className="bg-gray-800 border border-gray-700 rounded-lg p-4">
            <p className="text-sm text-gray-400 mb-3">
              Create an enrollment token to generate a one-liner install command.
            </p>
            <button
              onClick={handleCreateToken}
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
