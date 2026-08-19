import { useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { holdLabel, rolloutWording } from './rule-summary';
import { useRuleStore } from './state/rule-store';

type Rollout = components['schemas']['RuleRollout'];

const FIELD = 'w-24 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-200';
const LABEL = 'text-xs uppercase text-gray-500 font-semibold';

/** The settings an operator may move. Not every field of a rollout: the reach it
 * has actually got to, and the stop, are not this form's to write. */
type PaceField = 'canary_percent' | 'staged_percent' | 'canary_hold_secs' | 'staged_hold_secs';

const SETTINGS: readonly (readonly [PaceField, string, string])[] = [
  ['canary_percent', 'First stage reaches', '% of the estate'],
  ['staged_percent', 'Second stage reaches', '% of the estate'],
  ['canary_hold_secs', 'First stage is held for', 'seconds'],
  ['staged_hold_secs', 'Second stage is held for', 'seconds'],
];

/** Reads one setting off a draft without indexing it by a variable. */
function paceValue(rollout: Rollout, field: PaceField): number {
  switch (field) {
    case 'canary_percent': return rollout.canary_percent;
    case 'staged_percent': return rollout.staged_percent;
    case 'canary_hold_secs': return rollout.canary_hold_secs;
    case 'staged_hold_secs': return rollout.staged_hold_secs;
  }
}

/**
 * How far a rule has reached, how fast it is allowed to spread, and the switch
 * that stops it.
 *
 * The stop is set apart from the on/off toggle deliberately. Switching a rule
 * off is an ordinary choice about what a customer wants watched; stopping it is
 * an intervention against a rule that is doing harm, and the two have to be
 * tellable apart afterwards — which they cannot be if they are the same control.
 */
export function RolloutPanel({
  ruleId,
  rollout,
  canEdit,
}: {
  readonly ruleId: string;
  readonly rollout: Rollout;
  readonly canEdit: boolean;
}) {
  const saveRollout = useRuleStore((s) => s.saveRollout);
  const setStopped = useRuleStore((s) => s.setStopped);
  const [draft, setDraft] = useState(rollout);

  const save = () => {
    fireAndForget(saveRollout(ruleId, {
      enabled: draft.enabled,
      canary_percent: draft.canary_percent,
      staged_percent: draft.staged_percent,
      canary_hold_secs: draft.canary_hold_secs,
      staged_hold_secs: draft.staged_hold_secs,
    }));
  };

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-1">Rollout</h2>
      <p className="text-sm text-gray-300 mb-3">
        {rolloutWording(rollout)}
        {rollout.canary_group ? ` · trying on ${rollout.canary_group}` : ''}
      </p>

      <label className="flex items-center gap-2 mb-4 text-sm text-gray-300">
        <input
          type="checkbox"
          checked={draft.enabled}
          disabled={!canEdit}
          onChange={(e) => { setDraft({ ...draft, enabled: e.target.checked }); }}
        />
        <span>This customer gets the rule</span>
      </label>

      <div className="grid grid-cols-2 gap-3">
        {SETTINGS.map(([field, label, unit]) => (
          <label key={field} className="flex flex-col gap-1">
            <span className={LABEL}>{label}</span>
            <span className="flex items-center gap-2">
              <input
                type="number"
                className={FIELD}
                value={paceValue(draft, field)}
                disabled={!canEdit}
                aria-label={label}
                onChange={(e) => { setDraft({ ...draft, [field]: Number(e.target.value) }); }}
              />
              <span className="text-xs text-gray-500">{unit}</span>
            </span>
          </label>
        ))}
      </div>

      <p className="mt-2 text-xs text-gray-500">
        Held for {holdLabel(rollout.canary_hold_secs)} then {holdLabel(rollout.staged_hold_secs)}.
        A rule that misbehaves is pulled back to a smaller population by itself; that cannot be
        switched off.
      </p>

      {canEdit && (
        <div className="mt-4 flex items-center gap-3">
          <button
            type="button"
            onClick={save}
            className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
          >
            Save pace
          </button>
        </div>
      )}

      {canEdit && (
        <div className="mt-6 pt-4 border-t border-gray-700">
          <h3 className="text-sm font-semibold text-red-300 mb-1">Stop this rule</h3>
          <p className="text-xs text-gray-500 mb-3">
            Takes the rule off every machine it has reached, including machines that are offline —
            they are stopped when they come back. Separate from switching it off, so the two can be
            told apart afterwards.
          </p>
          <div className="flex gap-3">
            <button
              type="button"
              onClick={() => { fireAndForget(setStopped(ruleId, 'organization', !rollout.kill)); }}
              className="px-3 py-1 rounded bg-red-800 hover:bg-red-700 text-sm"
            >
              {rollout.kill ? 'Let it run for this customer' : 'Stop for this customer'}
            </button>
            <button
              type="button"
              onClick={() => { fireAndForget(setStopped(ruleId, 'tenant', true)); }}
              className="px-3 py-1 rounded bg-red-900 hover:bg-red-800 text-sm"
            >
              Stop for every customer
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
