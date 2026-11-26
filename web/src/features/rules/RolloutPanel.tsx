import { useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { rolloutWording } from './rule-summary';
import { useRuleStore } from './state/rule-store';

type Rollout = components['schemas']['RuleRollout'];
type Stage = components['schemas']['RuleRolloutStage'];

const FIELD = 'w-24 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-200';
const LABEL = 'text-xs uppercase text-gray-500 font-semibold';

// The form writes the stage and the shares, not the stop.
type ShareField = 'canary_percent' | 'staged_percent';

const SHARES: readonly (readonly [ShareField, string])[] = [
  ['canary_percent', 'First stage reaches'],
  ['staged_percent', 'Second stage reaches'],
];

const STAGES: readonly (readonly [Stage, string])[] = [
  ['canary', 'First hosts — the first share'],
  ['staged', 'Some hosts — the second share'],
  ['full', 'Everywhere'],
];

function shareValue(rollout: Rollout, field: ShareField): number {
  switch (field) {
    case 'canary_percent': return rollout.canary_percent;
    case 'staged_percent': return rollout.staged_percent;
  }
}

// A rule reaching nobody starts the picker at the first share, the smallest move from there.
function pickedStage(stage: Rollout['stage']): Stage {
  return stage === 'off' ? 'canary' : stage;
}

// The stop is a separate control from the on/off toggle so the two stay distinguishable afterwards.
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
  const [stage, setStage] = useState<Stage>(pickedStage(rollout.stage));

  const save = () => {
    fireAndForget(saveRollout(ruleId, {
      enabled: draft.enabled,
      stage,
      canary_percent: draft.canary_percent,
      staged_percent: draft.staged_percent,
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

      <label className="flex flex-col gap-1 mb-4">
        <span className={LABEL}>Reaches now</span>
        <select
          className="w-72 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-200"
          value={stage}
          disabled={!canEdit}
          aria-label="Reaches now"
          onChange={(e) => { setStage(e.target.value as Stage); }}
        >
          {STAGES.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>
      </label>

      <div className="grid grid-cols-2 gap-3">
        {SHARES.map(([field, label]) => (
          <label key={field} className="flex flex-col gap-1">
            <span className={LABEL}>{label}</span>
            <span className="flex items-center gap-2">
              <input
                type="number"
                className={FIELD}
                value={shareValue(draft, field)}
                disabled={!canEdit}
                aria-label={label}
                onChange={(e) => { setDraft({ ...draft, [field]: Number(e.target.value) }); }}
              />
              <span className="text-xs text-gray-500">% of the fleet</span>
            </span>
          </label>
        ))}
      </div>

      <p className="mt-2 text-xs text-gray-500">
        The rule moves from the first share to the second and then everywhere when an administrator
        moves it here.
      </p>

      {canEdit && (
        <div className="mt-4 flex items-center gap-3">
          <button
            type="button"
            onClick={save}
            className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
          >
            Save rollout
          </button>
        </div>
      )}

      {canEdit && (
        <div className="mt-6 pt-4 border-t border-gray-700">
          <h3 className="text-sm font-semibold text-red-300 mb-1">Stop this rule</h3>
          <p className="text-xs text-gray-500 mb-3">
            Takes the rule off every host it has reached, including hosts that are offline —
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
