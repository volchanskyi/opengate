import { useState } from 'react';

interface PublishFormData {
  version: string;
  os: string;
  arch: string;
  url: string;
  sha256: string;
}

interface ManifestPublishFormProps {
  showPublishForm: boolean;
  setShowPublishForm: (show: boolean) => void;
  onPublish: (form: PublishFormData) => Promise<void>;
}

export function ManifestPublishForm({
  showPublishForm,
  setShowPublishForm,
  onPublish,
}: ManifestPublishFormProps) {
  const [publishForm, setPublishForm] = useState<PublishFormData>({
    version: '',
    os: 'linux',
    arch: 'amd64',
    url: '',
    sha256: '',
  });

  const handlePublish = async (e: React.SyntheticEvent) => {
    e.preventDefault();
    await onPublish(publishForm);
    setPublishForm({ version: '', os: 'linux', arch: 'amd64', url: '', sha256: '' });
    setShowPublishForm(false);
  };

  return (
    <>
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-lg font-semibold">Published Manifests</h3>
        <button
          onClick={() => setShowPublishForm(!showPublishForm)}
          className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-sm"
        >
          Publish Version
        </button>
      </div>

      {showPublishForm && (
        <form onSubmit={handlePublish} className="bg-gray-800 border border-gray-700 rounded-lg p-4 mb-4 space-y-3">
          <div className="flex gap-4">
            <div className="flex-1">
              <label htmlFor="manifest-version" className="block text-sm text-gray-400 mb-1">Version</label>
              <input
                id="manifest-version"
                type="text"
                value={publishForm.version}
                onChange={(e) => setPublishForm({ ...publishForm, version: e.target.value })}
                className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
                placeholder="1.0.0"
                required
              />
            </div>
            <div>
              <label htmlFor="manifest-os" className="block text-sm text-gray-400 mb-1">OS</label>
              <select
                id="manifest-os"
                value={publishForm.os}
                onChange={(e) => setPublishForm({ ...publishForm, os: e.target.value })}
                className="bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
              >
                <option value="linux">linux</option>
              </select>
            </div>
            <div>
              <label htmlFor="manifest-arch" className="block text-sm text-gray-400 mb-1">Arch</label>
              <select
                id="manifest-arch"
                value={publishForm.arch}
                onChange={(e) => setPublishForm({ ...publishForm, arch: e.target.value })}
                className="bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
              >
                <option value="amd64">amd64</option>
                <option value="arm64">arm64</option>
              </select>
            </div>
          </div>
          <div>
            <label htmlFor="manifest-url" className="block text-sm text-gray-400 mb-1">URL</label>
            <input
              id="manifest-url"
              type="url"
              value={publishForm.url}
              onChange={(e) => setPublishForm({ ...publishForm, url: e.target.value })}
              className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
              placeholder="https://github.com/..."
              required
            />
          </div>
          <div>
            <label htmlFor="manifest-sha256" className="block text-sm text-gray-400 mb-1">SHA256</label>
            <input
              id="manifest-sha256"
              type="text"
              value={publishForm.sha256}
              onChange={(e) => setPublishForm({ ...publishForm, sha256: e.target.value })}
              className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm"
              placeholder="64-character hex digest"
              required
            />
          </div>
          <button type="submit" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm">
            Publish
          </button>
        </form>
      )}
    </>
  );
}
