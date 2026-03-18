import { useState } from 'react';
import type { components } from '../../types/api';

type AgentManifest = components['schemas']['AgentManifest'];

type Platform = 'linux/amd64' | 'linux/arm64';

interface InstallInstructionsProps {
  manifests: AgentManifest[];
}

export function InstallInstructions({ manifests }: InstallInstructionsProps) {
  const [platform, setPlatform] = useState<Platform>('linux/amd64');
  const [showManual, setShowManual] = useState(false);

  const [os, arch] = platform.split('/');
  const manifest = manifests.find((m) => m.os === os && m.arch === arch);

  return (
    <>
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
    </>
  );
}
