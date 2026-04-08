import { useState } from 'react';
import { isTokenExpired, isTokenExhausted } from '../../lib/token-status';
import type { components } from '../../types/api';

type EnrollmentToken = components['schemas']['EnrollmentToken'];

interface EnrollmentTokenFormProps {
  enrollmentTokens: EnrollmentToken[];
  showTokenForm: boolean;
  setShowTokenForm: (show: boolean) => void;
  copiedField: string | null;
  onCopy: (text: string, field: string) => void;
  onCreateToken: (form: { label: string; max_uses: number; expires_in_hours: number }) => Promise<void>;
  onDeleteToken: (id: string) => Promise<void>;
}

export function EnrollmentTokenForm({
  enrollmentTokens,
  showTokenForm,
  setShowTokenForm,
  copiedField,
  onCopy,
  onCreateToken,
  onDeleteToken,
}: EnrollmentTokenFormProps) {
  const [tokenLabel, setTokenLabel] = useState('');
  const [tokenMaxUses, setTokenMaxUses] = useState(0);
  const [tokenExpiresHours, setTokenExpiresHours] = useState(24);

  const handleCreateToken = async () => {
    await onCreateToken({
      label: tokenLabel || 'Quick setup',
      max_uses: tokenMaxUses,
      expires_in_hours: tokenExpiresHours,
    });
    setShowTokenForm(false);
    setTokenLabel('');
    setTokenMaxUses(0);
    setTokenExpiresHours(24);
  };

  return (
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
              onClick={() => { void handleCreateToken(); }}
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
                    onClick={() => { void onDeleteToken(t.id); }}
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
                    onClick={() => onCopy(t.token, `token-${t.id}`)}
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
  );
}
