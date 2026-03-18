import { useEffect, useState } from 'react';
import { useUpdateStore } from '../../state/update-store';
import { useAuthStore } from '../../state/auth-store';
import { EnrollmentTokenForm } from './EnrollmentTokenForm';
import { InstallInstructions } from './InstallInstructions';

export function AgentSetupPage() {
  const user = useAuthStore((s) => s.user);
  const manifests = useUpdateStore((s) => s.manifests);
  const enrollmentTokens = useUpdateStore((s) => s.enrollmentTokens);
  const isLoading = useUpdateStore((s) => s.isLoading);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const fetchEnrollmentTokens = useUpdateStore((s) => s.fetchEnrollmentTokens);
  const createEnrollmentToken = useUpdateStore((s) => s.createEnrollmentToken);
  const deleteEnrollmentToken = useUpdateStore((s) => s.deleteEnrollmentToken);

  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [showTokenForm, setShowTokenForm] = useState(false);

  useEffect(() => {
    fetchManifests();
    if (user?.is_admin) {
      fetchEnrollmentTokens();
    }
  }, [fetchManifests, fetchEnrollmentTokens, user?.is_admin]);

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

  const handleCreateToken = async (form: { label: string; max_uses: number; expires_in_hours: number }) => {
    await createEnrollmentToken(form);
  };

  const handleDeleteToken = async (id: string) => {
    await deleteEnrollmentToken(id);
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
        <EnrollmentTokenForm
          enrollmentTokens={enrollmentTokens}
          showTokenForm={showTokenForm}
          setShowTokenForm={setShowTokenForm}
          copiedField={copiedField}
          onCopy={handleCopy}
          onCreateToken={handleCreateToken}
          onDeleteToken={handleDeleteToken}
        />
      )}

      <InstallInstructions manifests={manifests} />

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
