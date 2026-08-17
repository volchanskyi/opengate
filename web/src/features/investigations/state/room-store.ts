import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction, progressAdapter } from '../../../state/api-action';
import { selectedOrganizationQuery } from '../../organizations';
import type { components } from '../../../types/api';

type IncidentDetail = components['schemas']['IncidentDetail'];
type AlertEvidence = components['schemas']['AlertEvidence'];
type Status = components['schemas']['IncidentStatus'];
type CauseCode = components['schemas']['IncidentCauseCode'];

interface RoomState {
  detail: IncidentDetail | null;
  loading: boolean;
  error: string | null;
  /** Why the last thing somebody tried was refused. */
  actionError: string | null;
  acting: boolean;
  /** Evidence per alert id, fetched only when an alert is opened. */
  evidence: Map<string, AlertEvidence>;
  evidenceLoading: Map<string, boolean>;
  evidenceErrors: Map<string, string>;

  /** Read a room from scratch. Anything the previous room held is dropped. */
  open: (id: string) => Promise<void>;
  /** Re-read the open room, keeping the evidence already fetched. */
  refresh: (id: string) => Promise<void>;
  leave: () => void;
  fetchEvidence: (incidentId: string, alertId: string) => Promise<void>;
  setStatus: (id: string, status: Status, cause?: CauseCode) => Promise<boolean>;
  setAssignee: (id: string, assigneeId: string | null) => Promise<boolean>;
  addComment: (id: string, body: string) => Promise<boolean>;
}

/** The customer being looked at, as every investigation read carries it. */
function scopeQuery() {
  const organizationId = selectedOrganizationQuery();
  return organizationId ? { organization_id: organizationId } : {};
}

const emptyEvidence = () => ({
  evidence: new Map<string, AlertEvidence>(),
  evidenceLoading: new Map<string, boolean>(),
  evidenceErrors: new Map<string, string>(),
});

export const useRoomStore = create<RoomState>((set, get) => {
  const roomProgress = progressAdapter(
    (error) => { set({ error }); },
    (loading) => { set({ loading }); },
  );

  const actionProgress = progressAdapter(
    (actionError) => { set({ actionError }); },
    (acting) => { set({ acting }); },
  );

  const read = (id: string) =>
    apiAction(roomProgress, () => api.GET('/api/v1/investigations/{id}', {
      params: { path: { id }, query: scopeQuery() },
    }));

  /**
   * A move writes a line into the room's history, and only the room read
   * returns it — so an accepted move is followed by a re-read. A refused one is
   * not: nothing changed, and re-reading would only cost a request.
   */
  const applyAccepted = async (id: string, incident: IncidentDetail['incident']) => {
    set((s) => (s.detail ? { detail: { ...s.detail, incident } } : {}));
    await get().refresh(id);
  };

  return {
    detail: null,
    loading: false,
    error: null,
    actionError: null,
    acting: false,
    ...emptyEvidence(),

    open: async (id) => {
      set({ detail: null, error: null, actionError: null, ...emptyEvidence() });
      const res = await read(id);
      if (res.ok) set({ detail: res.data });
    },

    refresh: async (id) => {
      const res = await read(id);
      if (res.ok) set({ detail: res.data });
    },

    leave: () => {
      set({ detail: null, loading: false, error: null, actionError: null, acting: false, ...emptyEvidence() });
    },

    fetchEvidence: async (incidentId, alertId) => {
      const { evidence, evidenceLoading } = get();
      if (evidence.has(alertId) || evidenceLoading.get(alertId)) return;

      set((s) => {
        const errors = new Map(s.evidenceErrors);
        errors.delete(alertId);
        return { evidenceLoading: new Map(s.evidenceLoading).set(alertId, true), evidenceErrors: errors };
      });

      const res = await apiAction(
        progressAdapter((error) => {
          if (error === null) return;
          set((s) => ({ evidenceErrors: new Map(s.evidenceErrors).set(alertId, error) }));
        }),
        () => api.GET('/api/v1/investigations/{id}/alerts/{alertId}/evidence', {
          params: { path: { id: incidentId, alertId }, query: scopeQuery() },
        }),
        false,
      );

      set((s) => ({
        evidenceLoading: new Map(s.evidenceLoading).set(alertId, false),
        evidence: res.ok ? new Map(s.evidence).set(alertId, res.data) : s.evidence,
      }));
    },

    setStatus: async (id, status, cause) => {
      const res = await apiAction(
        actionProgress,
        () => api.POST('/api/v1/investigations/{id}/status', {
          params: { path: { id }, query: scopeQuery() },
          body: { status, ...(cause ? { cause_code: cause } : {}) },
        }),
        false,
      );
      if (!res.ok) return false;
      await applyAccepted(id, res.data);
      return true;
    },

    setAssignee: async (id, assigneeId) => {
      const res = await apiAction(
        actionProgress,
        () => api.POST('/api/v1/investigations/{id}/assignee', {
          params: { path: { id }, query: scopeQuery() },
          // Handing a room back omits the field; an empty string is not a person.
          body: assigneeId ? { assignee_id: assigneeId } : {},
        }),
        false,
      );
      if (!res.ok) return false;
      await applyAccepted(id, res.data);
      return true;
    },

    addComment: async (id, body) => {
      const note = body.trim();
      if (note === '') return false;

      const res = await apiAction(
        actionProgress,
        () => api.POST('/api/v1/investigations/{id}/comments', {
          params: { path: { id }, query: scopeQuery() },
          body: { body: note },
        }),
        false,
      );
      if (!res.ok) return false;

      const created = res.data;
      set((s) => (s.detail
        ? { detail: { ...s.detail, events: [...s.detail.events, created], events_total: s.detail.events_total + 1 } }
        : {}));
      return true;
    },
  };
});
