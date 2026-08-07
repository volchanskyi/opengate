import { useEffect, useState } from 'react';
import { useUpdateStore } from '../devices';
import { useAuthStore } from '../../state/auth-store';
import { isTokenActive } from '../../lib/token-status';
import { EnrollmentTokenForm } from './EnrollmentTokenForm';
import { fireAndForget } from '../../lib/fire-and-forget';

export function AgentSetupPage() {
  const user = useAuthStore((s) => s.user);
  const enrollmentTokens = useUpdateStore((s) => s.enrollmentTokens);
  const isLoading = useUpdateStore((s) => s.isLoading);
  const fetchEnrollmentTokens = useUpdateStore((s) => s.fetchEnrollmentTokens);
  const createEnrollmentToken = useUpdateStore((s) => s.createEnrollmentToken);
  const deleteEnrollmentToken = useUpdateStore((s) => s.deleteEnrollmentToken);

  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [showTokenForm, setShowTokenForm] = useState(false);

  useEffect(() => {
    if (user?.is_admin) {
      fireAndForget(fetchEnrollmentTokens());
    }
  }, [fetchEnrollmentTokens, user?.is_admin]);

  const activeToken = enrollmentTokens.find(
    (t) => isTokenActive(t.expires_at, t.max_uses, t.use_count)
  );

  const serverUrl = globalThis.location.origin;
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

  if (isLoading && enrollmentTokens.length === 0) {
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
        <h3 className="text-lg font-semibold mb-3">Quick Setup</h3>
        <QuickInstallContent
          installCommand={installCommand}
          isAdmin={user?.is_admin ?? false}
          copiedField={copiedField}
          onCopy={(text, field) => { fireAndForget(handleCopy(text, field)); }}
        />
      </section>

      {/* Enrollment Tokens (admin only) */}
      {user?.is_admin && (
        <EnrollmentTokenForm
          enrollmentTokens={enrollmentTokens}
          showTokenForm={showTokenForm}
          setShowTokenForm={setShowTokenForm}
          copiedField={copiedField}
          onCopy={(text, field) => { fireAndForget(handleCopy(text, field)); }}
          onCreateToken={handleCreateToken}
          onDeleteToken={handleDeleteToken}
        />
      )}

      {/* What happens next */}
      <section className="mb-8">
        <h3 className="text-lg font-semibold mb-3">What happens next</h3>
        <p className="text-sm text-gray-400">
          Once the agent is installed and running, it will connect to the server via QUIC and
          appear in your device list under the assigned site. You can then start remote sessions,
          manage the device, and push updates.
        </p>
      </section>

      <IntelAmtSetup />
    </div>
  );
}

/**
 * Standing BIOS/MEBx documentation for enabling Intel AMT. It belongs to setup,
 * not to any one device: the steps are the same everywhere, and a device page
 * shows a device's actual AMT state instead.
 */
function IntelAmtSetup() {
  const [expanded, setExpanded] = useState(false);

  return (
    <section>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className="text-lg font-semibold flex items-center gap-2"
      >
        <span className={`text-xs transition-transform ${expanded ? 'rotate-90' : ''}`} aria-hidden="true">&#9654;</span>
        {' '}Intel AMT Setup
      </button>
      {expanded && (
        <div className="mt-3 bg-gray-800 border border-gray-700 rounded-lg p-4 text-sm text-gray-400 space-y-3">
          <p>
            Intel AMT (Active Management Technology) enables out-of-band power management
            for supported hardware. To enable AMT on a device:
          </p>
          <ol className="list-decimal list-inside space-y-2">
            <li>
              <strong className="text-gray-300">Enable AMT in BIOS</strong> — Enter the BIOS/UEFI
              setup (usually F2/Del at boot) and enable Intel AMT / ME (Management Engine).
            </li>
            <li>
              <strong className="text-gray-300">Configure MEBx</strong> — Press Ctrl+P at boot to
              enter MEBx. Set a strong password and configure the network settings (DHCP or static IP).
            </li>
            <li>
              <strong className="text-gray-300">Enable remote access</strong> — In MEBx, enable
              &quot;Remote Setup And Configuration&quot; and ensure the AMT network interface is active.
            </li>
            <li>
              <strong className="text-gray-300">Verify connectivity</strong> — The device registers
              with the MPS server on its own once AMT is configured and the network is reachable.
              Power actions appear on the device page once the connection is up.
            </li>
          </ol>
          <p className="text-xs text-gray-500">
            Requires Intel vPro-compatible hardware with AMT firmware. Not all Intel processors support AMT.
          </p>
        </div>
      )}
    </section>
  );
}

function QuickInstallContent({
  installCommand,
  isAdmin,
  copiedField,
  onCopy,
}: Readonly<{
  installCommand: string | null;
  isAdmin: boolean;
  copiedField: string | null;
  onCopy: (text: string, field: string) => void;
}>) {
  if (installCommand) {
    return (
      <div>
        <p className="text-sm text-gray-400 mb-2">
          Run this command on the target machine to install the OpenGate agent:
        </p>
        <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 flex items-start gap-3">
          <code className="flex-1 text-sm text-green-400 break-all font-mono">
            {installCommand}
          </code>
          <button
            onClick={() => onCopy(installCommand, 'install')}
            className="px-3 py-1 bg-gray-700 hover:bg-gray-600 rounded text-sm whitespace-nowrap"
          >
            {copiedField === 'install' ? 'Copied!' : 'Copy'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <p className="text-sm text-gray-400">
      {isAdmin
        ? 'Create an enrollment token below to generate a one-liner install command.'
        : 'Ask your administrator to create an enrollment token for agent installation.'}
    </p>
  );
}
