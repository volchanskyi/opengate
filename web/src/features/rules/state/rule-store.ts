import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type RuleDetail = components['schemas']['RuleDetail'];
type RuleBindingInput = components['schemas']['RuleBindingInput'];
type RuleRolloutInput = components['schemas']['RuleRolloutInput'];
type RuleStopScope = components['schemas']['RuleStopScope'];
type ResolvedRule = components['schemas']['ResolvedRule'];

/**
 * One rule's page: what it does, what a customer has tuned, and everything an
 * administrator may change about it.
 *
 * Every write re-reads the rule rather than patching what is on screen. A rule's
 * page shows resolved state — how far it has reached, what it covers, what a
 * version change moved — and none of that can be worked out from the request
 * that was just sent, so a locally-patched page would quietly disagree with the
 * fleet.
 */
interface RuleState {
  detail: RuleDetail | null;
  /** The rule as one named machine is running it, when somebody has asked. */
  resolved: ResolvedRule | null;
  isLoading: boolean;
  error: string | null;

  fetchRule: (ruleId: string) => Promise<void>;
  resolveFor: (ruleId: string, deviceId: string) => Promise<void>;
  clearResolved: () => void;

  saveBinding: (ruleId: string, binding: RuleBindingInput) => Promise<boolean>;
  removeBinding: (ruleId: string, bindingId: string) => Promise<boolean>;
  saveRollout: (ruleId: string, rollout: RuleRolloutInput) => Promise<boolean>;
  setStopped: (ruleId: string, scope: RuleStopScope, stopped: boolean) => Promise<boolean>;
  acknowledgeClamp: (ruleId: string, clampId: string) => Promise<boolean>;
}

/** The customer the screen is showing, as a query the server narrows by. */
function customerQuery(): { organization_id?: string } {
  const organizationId = selectedOrganizationQuery();
  return organizationId ? { organization_id: organizationId } : {};
}

export const useRuleStore = create<RuleState>((set, get) => ({
  detail: null,
  resolved: null,
  isLoading: false,
  error: null,

  fetchRule: async (ruleId) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/rules/{rule_id}', {
        params: { path: { rule_id: ruleId }, query: customerQuery() },
      }),
    );
    if (!res.ok) return;
    set({ detail: res.data });
  },

  resolveFor: async (ruleId, deviceId) => {
    const res = await apiAction(
      progressAdapter((error) => { set({ error }); }),
      () =>
        api.GET('/api/v1/rules/{rule_id}/resolved', {
          params: { path: { rule_id: ruleId }, query: { device_id: deviceId } },
        }),
      false,
    );
    if (!res.ok) return;
    set({ resolved: res.data });
  },

  clearResolved: () => { set({ resolved: null }); },

  saveBinding: async (ruleId, binding) => {
    const res = await apiAction(set, () =>
      api.PUT('/api/v1/rules/{rule_id}/bindings', {
        params: { path: { rule_id: ruleId }, query: customerQuery() },
        body: binding,
      }), false);
    if (!res.ok) return false;
    await get().fetchRule(ruleId);
    return true;
  },

  removeBinding: async (ruleId, bindingId) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/rules/{rule_id}/bindings/{binding_id}', {
        params: { path: { rule_id: ruleId, binding_id: bindingId } },
      }), false);
    if (!res.ok) return false;
    await get().fetchRule(ruleId);
    return true;
  },

  saveRollout: async (ruleId, rollout) => {
    const res = await apiAction(set, () =>
      api.PUT('/api/v1/rules/{rule_id}/rollout', {
        params: { path: { rule_id: ruleId }, query: customerQuery() },
        body: rollout,
      }), false);
    if (!res.ok) return false;
    await get().fetchRule(ruleId);
    return true;
  },

  setStopped: async (ruleId, scope, stopped) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/rules/{rule_id}/stop', {
        params: { path: { rule_id: ruleId }, query: customerQuery() },
        body: { scope, stopped },
      }), false);
    if (!res.ok) return false;
    await get().fetchRule(ruleId);
    return true;
  },

  acknowledgeClamp: async (ruleId, clampId) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/rules/{rule_id}/clamps/{clamp_id}', {
        params: { path: { rule_id: ruleId, clamp_id: clampId } },
      }), false);
    if (!res.ok) return false;
    await get().fetchRule(ruleId);
    return true;
  },
}));
