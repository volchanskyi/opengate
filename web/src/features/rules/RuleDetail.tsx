import { useEffect } from 'react';
import { Link, useParams } from 'react-router';
import { fireAndForget } from '../../lib/fire-and-forget';
import { LoadingSpinner } from '../../components/LoadingSpinner';
import { useAuthStore } from '../../state/auth-store';
import { CoveragePanel } from './CoveragePanel';
import { NoiseBadge } from './NoiseBadge';
import { ResolvedFor } from './ResolvedFor';
import { RolloutPanel } from './RolloutPanel';
import { TuningPanel } from './TuningPanel';
import { WhatItDoes } from './WhatItDoes';
import { noiseWording } from './rule-summary';
import { useCatalogueStore } from './state/catalogue-store';
import { useRuleStore } from './state/rule-store';

/**
 * One rule's page.
 *
 * Everyone in the tenant reads it — a technician resolving something as a false
 * alarm has to be able to see the rule that produced it — and only an
 * administrator changes anything, so the controls are absent rather than
 * disabled for everybody else.
 */
export function RuleDetail() {
  const { ruleId = '' } = useParams();
  const detail = useRuleStore((s) => s.detail);
  const isLoading = useRuleStore((s) => s.isLoading);
  const error = useRuleStore((s) => s.error);
  const fetchRule = useRuleStore((s) => s.fetchRule);
  const clearResolved = useRuleStore((s) => s.clearResolved);
  const fleetSize = useCatalogueStore((s) => s.fleetSize);
  const fetchCatalogue = useCatalogueStore((s) => s.fetchCatalogue);
  const canEdit = useAuthStore((s) => s.user?.is_admin ?? false);

  useEffect(() => {
    clearResolved();
    fireAndForget(fetchRule(ruleId));
    fireAndForget(fetchCatalogue());
  }, [ruleId, fetchRule, fetchCatalogue, clearResolved]);

  if (isLoading && !detail) return <LoadingSpinner />;

  if (!detail) {
    return (
      <div className="p-6">
        <p role="alert" className="text-sm text-red-400">
          {error ?? 'This rule is not in the pack this server runs.'}
        </p>
        <Link to="/rules" className="text-sm text-blue-400 hover:text-blue-300">
          Back to rules
        </Link>
      </div>
    );
  }

  const { rule, bindings, clamps } = detail;

  return (
    <div className="p-6 flex flex-col gap-4">
      <header>
        <Link to="/rules" className="text-xs text-blue-400 hover:text-blue-300">
          Rules
        </Link>
        <div className="flex items-center gap-3 mt-1">
          <h1 className="text-xl font-bold">{rule.id}</h1>
          <NoiseBadge noise={rule.noise} />
          <span className="text-sm text-gray-400">{noiseWording(rule.noise)}</span>
        </div>
        <p className="text-sm text-gray-400">{rule.summary}</p>
        {error && (
          <p role="alert" className="mt-2 text-sm text-red-400">
            {error}
          </p>
        )}
      </header>

      <WhatItDoes rule={rule} />
      <TuningPanel rule={rule} bindings={bindings} clamps={clamps} canEdit={canEdit} />
      <ResolvedFor ruleId={rule.id} />
      <CoveragePanel coverage={rule.coverage} fleetSize={fleetSize} />
      <RolloutPanel ruleId={rule.id} rollout={rule.rollout} canEdit={canEdit} />
    </div>
  );
}
