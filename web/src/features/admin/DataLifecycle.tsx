import { useCallback, useEffect, useState } from 'react';
import type { components } from '../../types/api';
import { api } from '../../lib/api';
import { useAuthStore } from '../../state/auth-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { fireAndForget } from '../../lib/fire-and-forget';

type PurgeJob = components['schemas']['PurgeJob'];

const POLL_INTERVAL_MS = 2000;

/** Human labels for each purge-job state, most-to-least advanced. */
const STATE_LABEL: Record<string, string> = {
  requested: 'Requested',
  'central-logical-complete': 'Central stores logically erased',
  'central-physical-compaction-pending': 'Awaiting VictoriaMetrics compaction',
  'object-delete-pending': 'Deleting cold-tier objects',
  'edge-erase-pending': 'Awaiting edge erasure',
  complete: 'Complete',
};

function StoreFlag({ label, done }: { readonly label: string; readonly done: boolean }) {
  return (
    <li className="flex items-center gap-2">
      <span className={done ? 'text-green-400' : 'text-gray-500'}>{done ? '✓' : '○'}</span>
      <span className={done ? 'text-gray-200' : 'text-gray-400'}>{label}</span>
    </li>
  );
}

export function DataLifecycle() {
  const orgId = useAuthStore((s) => s.orgId);
  const addToast = useToastStore((s) => s.addToast);

  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [job, setJob] = useState<PurgeJob | null>(null);

  const pollJob = useCallback(async (jobId: string) => {
    const res = await api.GET('/api/v1/purge-jobs/{jobId}', { params: { path: { jobId } } });
    if (res.data) setJob(res.data);
  }, []);

  // Poll an in-flight job until it completes.
  useEffect(() => {
    if (!job || job.state === 'complete') return undefined;
    const id = setInterval(() => { fireAndForget(pollJob(job.id)); }, POLL_INTERVAL_MS);
    return () => { clearInterval(id); };
  }, [job, pollJob]);

  const handlePurge = async () => {
    if (!orgId) return;
    if (!confirm) {
      setConfirm(true);
      return;
    }
    setBusy(true);
    const res = await api.POST('/api/v1/orgs/{orgId}/purge', { params: { path: { orgId } } });
    setBusy(false);
    setConfirm(false);
    if (res.error || !res.data) {
      addToast('Failed to start tenant purge', 'error');
      return;
    }
    setJob(res.data);
    addToast('Tenant purge started', 'success');
  };

  let buttonLabel = 'Purge all tenant telemetry';
  if (busy) buttonLabel = 'Starting…';
  else if (confirm) buttonLabel = 'Confirm — erase everything';

  const inFlight = job !== null && job.state !== 'complete';

  return (
    <div className="max-w-2xl">
      <h2 className="text-lg font-semibold text-gray-100 mb-2">Data Lifecycle</h2>
      <p className="text-sm text-gray-400 mb-4">
        Permanently erase every device&apos;s centralized telemetry for this organization and
        deprovision its agents. This is irreversible — there is no undo, no grace window, and no
        export. Agents are denied re-enrollment; offline agents wipe their local store on next
        reconnect.
      </p>

      <div className="rounded border border-red-800 bg-red-950/30 p-4">
        <button
          type="button"
          disabled={busy || inFlight || !orgId}
          onClick={() => { fireAndForget(handlePurge()); }}
          className="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 rounded text-xs font-medium"
        >
          {buttonLabel}
        </button>
        {confirm && !busy && (
          <button
            type="button"
            onClick={() => { setConfirm(false); }}
            className="ml-2 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-xs font-medium"
          >
            Cancel
          </button>
        )}
      </div>

      {job && (
        <div className="mt-4 rounded border border-gray-700 bg-gray-800/40 p-4" aria-live="polite">
          <div className="text-sm text-gray-200 mb-2">
            Purge status: <span className="font-medium">{STATE_LABEL[job.state] ?? job.state}</span>
            {inFlight && <span className="ml-2 text-gray-400">(in progress…)</span>}
          </div>
          <ul className="text-sm space-y-1">
            <StoreFlag label="VictoriaMetrics series deleted" done={job.vm_deleted} />
            <StoreFlag label="Cold-tier objects deleted" done={job.object_deleted} />
            <StoreFlag label="Postgres rows deleted" done={job.pg_deleted} />
            <StoreFlag label="Central emptiness verified" done={job.verified} />
          </ul>
          {job.last_error && (
            <p className="mt-2 text-xs text-yellow-400">Last status: {job.last_error}</p>
          )}
        </div>
      )}
    </div>
  );
}
