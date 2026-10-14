import { create } from 'zustand';
import { api } from '../../../lib/api';
import { apiAction } from '../../../state/api-action';
import type { components } from '../../../types/api';

type AgentSession = components['schemas']['AgentSession'];

/** How a request to open a session on a host ended. */
export type SessionStart =
  | { kind: 'opened'; token: string; relayUrl: string; helpers: string[] }
  | { kind: 'held'; holderName: string; startedAt: string }
  | { kind: 'failed'; error: string };

interface SessionState {
  sessions: AgentSession[];
  /** Which device the list belongs to — the key that tells a device switch
   *  (clear first) apart from a periodic re-read of the same device (keep). */
  sessionsDeviceId: string | null;
  isLoading: boolean;
  error: string | null;
  fetchSessions: (deviceId: string) => Promise<void>;
  startSession: (deviceId: string) => Promise<SessionStart>;
  endDeviceSession: (deviceId: string) => Promise<boolean>;
}

/** The server's explanation in an error body, or a fallback naming the status. */
function errorText(error: unknown, status: number | undefined): string {
  const text = (error as { error?: unknown } | undefined)?.error;
  return typeof text === 'string' && text.length > 0 ? text : `Request failed with status ${status ?? 'unknown'}`;
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  sessionsDeviceId: null,
  isLoading: false,
  error: null,

  fetchSessions: async (deviceId) => {
    // Drop a previous device's sessions immediately; a re-read of the device
    // already on screen keeps its list up while the response is in flight.
    set((state) => (state.sessionsDeviceId === deviceId ? {} : { sessions: [], sessionsDeviceId: deviceId }));
    const res = await apiAction(set, () =>
      api.GET('/api/v1/sessions', { params: { query: { device_id: deviceId } } }),
    );
    if (res.ok) set({ sessions: res.data, sessionsDeviceId: deviceId });
  },

  startSession: async (deviceId) => {
    const { data, error, response } = await api.POST('/api/v1/sessions', {
      body: {
        device_id: deviceId,
        permissions: { desktop: true, terminal: true, file_read: true, file_write: true, input: true },
      },
    });
    if (data) {
      return {
        kind: 'opened',
        token: data.token,
        relayUrl: data.relay_url,
        helpers: data.ice_servers.flatMap((server) => server.urls),
      };
    }
    const refusal = error as { holder_name?: string; started_at?: string } | undefined;
    if (response?.status === 409 && refusal?.holder_name && refusal.started_at) {
      return { kind: 'held', holderName: refusal.holder_name, startedAt: refusal.started_at };
    }
    return { kind: 'failed', error: errorText(error, response?.status) };
  },

  endDeviceSession: async (deviceId) => {
    const res = await apiAction(
      set,
      () => api.DELETE('/api/v1/devices/{id}/session', { params: { path: { id: deviceId } } }),
      false,
    );
    if (res.ok) set({ sessions: [] });
    return res.ok;
  },
}));
