import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { fireAndForget } from '../../lib/fire-and-forget';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { useAuthStore } from '../../state/auth-store';
import { useAlertLimitsStore } from './state/alert-limits-store';

const FIELD = 'w-32 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-200';

/**
 * A customer's alert budget.
 *
 * On its own page rather than on a rule, because it is not a property of any
 * rule: it is the safety net under all of them. Both numbers were chosen from an
 * estimate of a rate nobody had measured, which is exactly why they are here —
 * a wrong guess that needs a release to correct is an outage.
 */
export function AlertLimits() {
  const limits = useAlertLimitsStore((s) => s.limits);
  const isLoading = useAlertLimitsStore((s) => s.isLoading);
  const error = useAlertLimitsStore((s) => s.error);
  const fetchLimits = useAlertLimitsStore((s) => s.fetchLimits);
  const saveLimits = useAlertLimitsStore((s) => s.saveLimits);
  const canEdit = useAuthStore((s) => s.user?.is_admin ?? false);

  // The fields show what somebody has typed, or the stored budget until they
  // type anything. Mirroring the stored value into state on every read would
  // overwrite a half-typed number the moment another read landed.
  const [typedCustomer, setTypedCustomer] = useState<string | null>(null);
  const [typedMachine, setTypedMachine] = useState<string | null>(null);

  useEffect(() => {
    fireAndForget(fetchLimits());
  }, [fetchLimits]);

  if (isLoading && !limits) return <LoadingSpinner />;
  if (!limits) {
    return (
      <div className="p-6">
        <p role="alert" className="text-sm text-red-400">
          {error ?? 'This budget could not be read.'}
        </p>
      </div>
    );
  }

  const customerHourly = typedCustomer ?? String(limits.organization_hourly);
  const machineHourly = typedMachine ?? String(limits.device_hourly);

  return (
    <div className="p-6">
      <Link to="/rules" className="text-xs text-blue-400 hover:text-blue-300">
        Rules
      </Link>
      <h1 className="text-xl font-bold mt-1">Alert limits</h1>
      <p className="text-sm text-gray-400 mb-4">
        How many alerts this customer may raise before the excess is refused. Nothing refused is
        lost quietly — what a ceiling turns away is counted and folds into one incident that says
        how much.
      </p>

      {error && (
        <p role="alert" className="mb-4 text-sm text-red-400">
          {error}
        </p>
      )}

      <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 flex flex-col gap-4 max-w-xl">
        <label className="flex flex-col gap-1">
          <span className="text-xs uppercase text-gray-500 font-semibold">
            This customer, per hour
          </span>
          <input
            type="number"
            className={FIELD}
            aria-label="This customer, per hour"
            value={customerHourly}
            disabled={!canEdit}
            onChange={(e) => { setTypedCustomer(e.target.value); }}
          />
          <span className="text-xs text-gray-500">
            Across every machine they have. At most {limits.max_organization_hourly}.
          </span>
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-xs uppercase text-gray-500 font-semibold">
            One machine, per hour
          </span>
          <input
            type="number"
            className={FIELD}
            aria-label="One machine, per hour"
            value={machineHourly}
            disabled={!canEdit}
            onChange={(e) => { setTypedMachine(e.target.value); }}
          />
          <span className="text-xs text-gray-500">
            Enforced on the machine itself, so it travels down with the rules. At most{' '}
            {limits.max_device_hourly}.
          </span>
        </label>

        {canEdit && (
          <div>
            <button
              type="button"
              onClick={() => {
                fireAndForget(saveLimits(Number(customerHourly), Number(machineHourly)));
              }}
              className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
            >
              Save
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
