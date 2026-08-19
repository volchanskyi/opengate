import { useState } from 'react';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';
import { selectorWording } from './rule-summary';
import { useRuleStore } from './state/rule-store';

type Rule = components['schemas']['Rule'];
type RuleBinding = components['schemas']['RuleBinding'];
type RuleClamp = components['schemas']['RuleClamp'];

const CELL = 'px-3 py-2 text-sm text-gray-300';
const HEAD = 'px-3 py-2 text-left text-xs font-semibold text-gray-400';

/**
 * How a rung reads to a person: what the code calls an organization is, to
 * whoever reads this screen, a customer. A Map rather than an object, because a
 * value off the wire indexing an object reaches the prototype chain.
 */
const LEVEL_WORDING = new Map<RuleBinding['level'], string>([
  ['device', 'One machine'],
  ['site', 'One office'],
  ['organization', 'The whole customer'],
  ['tenant', 'Every customer'],
]);

function Bounds({ rule, param }: { readonly rule: Rule; readonly param: string }) {
  const bounds = new Map(Object.entries(rule.tunable)).get(param);
  if (!bounds) return null;
  return (
    <span className="text-xs text-gray-500">
      {' '}
      (allowed {bounds.min}–{bounds.max}, ships at {bounds.shipped})
    </span>
  );
}

/**
 * What a version change had to move, and until somebody has seen it.
 *
 * A rule upgrade keeps the customer's tuning. When a new version narrows what it
 * accepts, the value moves to the nearest one it does allow and the rule keeps
 * firing at that — going quiet is the failure this guards against — but the move
 * was not the customer's decision, so it stays on the screen until acknowledged.
 */
function Clamps({
  ruleId,
  clamps,
  canEdit,
}: {
  readonly ruleId: string;
  readonly clamps: readonly RuleClamp[];
  readonly canEdit: boolean;
}) {
  const acknowledge = useRuleStore((s) => s.acknowledgeClamp);
  if (clamps.length === 0) return null;

  return (
    <div role="alert" className="mb-4 p-3 rounded border border-amber-700 bg-amber-950">
      <h3 className="text-sm font-semibold text-amber-200 mb-1">
        This rule&apos;s definition changed
      </h3>
      <ul className="text-sm text-amber-100">
        {clamps.map((clamp) => (
          <li key={clamp.id} className="flex items-center justify-between py-1">
            <span>
              Version {clamp.rule_version} no longer allows {clamp.param} at {clamp.from_value};
              it is running at {clamp.to_value}.
            </span>
            {canEdit && (
              <button
                type="button"
                onClick={() => { fireAndForget(acknowledge(ruleId, clamp.id)); }}
                className="ml-3 px-2 py-0.5 rounded bg-amber-800 hover:bg-amber-700 text-xs"
              >
                Understood
              </button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** The form for filing a new value, kept to what the rule declares adjustable. */
function NewValue({ rule, canEdit }: { readonly rule: Rule; readonly canEdit: boolean }) {
  const saveBinding = useRuleStore((s) => s.saveBinding);
  const params = Object.keys(rule.tunable).sort();
  const [param, setParam] = useState(params[0] ?? '');
  const [value, setValue] = useState('');
  const [levelKey, setLevelKey] = useState('');

  if (!canEdit || params.length === 0) return null;

  const submit = () => {
    if (!levelKey || value === '') return;
    fireAndForget(saveBinding(rule.id, {
      level: 'site',
      level_key: levelKey,
      params: { [param]: Number(value) },
    }));
    setValue('');
  };

  return (
    <div className="mt-4 flex flex-wrap items-end gap-2">
      <label className="flex flex-col gap-1">
        <span className="text-xs uppercase text-gray-500 font-semibold">Office</span>
        <input
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm w-72"
          placeholder="office id"
          aria-label="Office"
          value={levelKey}
          onChange={(e) => { setLevelKey(e.target.value); }}
        />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs uppercase text-gray-500 font-semibold">Setting</span>
        <select
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm"
          aria-label="Setting"
          value={param}
          onChange={(e) => { setParam(e.target.value); }}
        >
          {params.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs uppercase text-gray-500 font-semibold">Value</span>
        <input
          type="number"
          className="bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm w-28"
          aria-label="Value"
          value={value}
          onChange={(e) => { setValue(e.target.value); }}
        />
      </label>
      <button
        type="button"
        onClick={submit}
        className="px-3 py-1 rounded bg-blue-600 hover:bg-blue-500 text-sm"
      >
        Set for this office
      </button>
      <Bounds rule={rule} param={param} />
    </div>
  );
}

/**
 * The numbers a customer may retune, laid out by how narrowly each one is aimed.
 *
 * The order is the order resolution reads them in — the narrowest rung wins, and
 * two labels at one rung are settled by a precedence somebody set and can see.
 * Every invisible tie-break produces a threshold nobody can predict from the
 * screen, which is the class of question this ordering removes.
 */
export function TuningPanel({
  rule,
  bindings,
  clamps,
  canEdit,
}: {
  readonly rule: Rule;
  readonly bindings: readonly RuleBinding[];
  readonly clamps: readonly RuleClamp[];
  readonly canEdit: boolean;
}) {
  const removeBinding = useRuleStore((s) => s.removeBinding);

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-4">
      <h2 className="text-sm font-semibold text-gray-200 mb-3">Tuning</h2>
      <Clamps ruleId={rule.id} clamps={clamps} canEdit={canEdit} />

      {bindings.length === 0 ? (
        <p className="text-sm text-gray-400">
          Nothing is retuned. Every machine runs the values the rule ships with.
        </p>
      ) : (
        <table className="w-full">
          <thead>
            <tr>
              <th className={HEAD}>Aimed at</th>
              <th className={HEAD}>Which machines</th>
              <th className={HEAD}>Order</th>
              <th className={HEAD}>Values</th>
              <th className={HEAD} aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {bindings.map((binding) => (
              <tr key={binding.id} className="border-t border-gray-700">
                <td className={CELL}>{LEVEL_WORDING.get(binding.level) ?? binding.level}</td>
                <td className={CELL}>{selectorWording(binding.selector)}</td>
                <td className={`${CELL} tabular-nums`}>{binding.precedence}</td>
                <td className={CELL}>
                  {Object.entries(binding.params)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([name, value]) => (
                      <span key={name} className="mr-3">
                        {name} {value}
                        <Bounds rule={rule} param={name} />
                      </span>
                    ))}
                </td>
                <td className={CELL}>
                  {canEdit && (
                    <button
                      type="button"
                      onClick={() => { fireAndForget(removeBinding(rule.id, binding.id)); }}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      Remove
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <NewValue rule={rule} canEdit={canEdit} />
    </section>
  );
}
