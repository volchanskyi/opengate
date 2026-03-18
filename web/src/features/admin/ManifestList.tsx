import type { components } from '../../types/api';

type AgentManifest = components['schemas']['AgentManifest'];

interface ManifestListProps {
  manifests: AgentManifest[];
  onPush: (version: string, os: string, arch: string) => Promise<void>;
}

export function ManifestList({ manifests, onPush }: ManifestListProps) {
  if (manifests.length === 0) {
    return <p className="text-sm text-gray-500">No manifests published yet.</p>;
  }

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-gray-700 text-left text-gray-400">
          <th className="pb-2">Version</th>
          <th className="pb-2">OS</th>
          <th className="pb-2">Arch</th>
          <th className="pb-2">URL</th>
          <th className="pb-2">SHA256</th>
          <th className="pb-2">Actions</th>
        </tr>
      </thead>
      <tbody>
        {manifests.map((m, i) => (
          <tr key={`${m.version}-${m.os}-${m.arch}-${i}`} className="border-b border-gray-800">
            <td className="py-2">{m.version}</td>
            <td className="py-2">{m.os}</td>
            <td className="py-2">{m.arch}</td>
            <td className="py-2">
              <a
                href={m.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-400 hover:text-blue-300 text-xs"
                title={m.url}
              >
                {m.url.length > 50 ? m.url.slice(0, 50) + '...' : m.url}
              </a>
            </td>
            <td className="py-2 font-mono text-xs" title={m.sha256}>
              {m.sha256.slice(0, 12)}...
            </td>
            <td className="py-2">
              <button
                onClick={() => onPush(m.version, m.os, m.arch)}
                className="text-green-400 hover:text-green-300 text-xs"
              >
                Push to Agents
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
