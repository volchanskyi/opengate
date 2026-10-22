import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';
import { identityOf, setCsrfToken, SIGNED_IN_KEY } from '../lib/console-session';
import type { components } from '../types/api';

type User = components['schemas']['User'];
type ConsoleSession = components['schemas']['ConsoleSession'];

interface AuthState {
  user: User | null;
  tenantId: string | null;
  /** How long the session lasts without a request, as the server set it. */
  idleTimeoutSeconds: number | null;
  isLoading: boolean;
  hydrated: boolean;
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => Promise<void>;
  /** Forgets a session the server already ended, as an expired idle clock or a 401 says. */
  signedOutLocally: () => void;
  fetchMe: () => Promise<void>;
  hydrate: () => Promise<void>;
}

/** Writes the marker other tabs watch; it names the account, never a credential. */
function markSignedIn(identity: string | null): void {
  try {
    if (identity === null) {
      localStorage.removeItem(SIGNED_IN_KEY);
    } else {
      localStorage.setItem(SIGNED_IN_KEY, identity);
    }
  } catch {
    // Storage that cannot be written leaves other tabs unaware, which the server's 401 corrects.
  }
}

export const useAuthStore = create<AuthState>((set, get) => {
  const signedIn = (session: ConsoleSession) => {
    setCsrfToken(session.csrf_token);
    markSignedIn(identityOf(session.user.id, session.tenant_id));
    set({
      user: session.user,
      tenantId: session.tenant_id,
      idleTimeoutSeconds: session.idle_timeout_seconds,
    });
  };

  return {
    user: null,
    tenantId: null,
    idleTimeoutSeconds: null,
    isLoading: false,
    hydrated: false,
    error: null,

    login: async (email, password) => {
      const res = await apiAction(set, () =>
        api.POST('/api/v1/auth/session', { body: { email, password } }),
      );
      if (res.ok) {
        signedIn(res.data);
      }
    },

    register: async (email, password, displayName) => {
      const res = await apiAction(set, () =>
        api.POST('/api/v1/auth/session/register', {
          body: { email, password, display_name: displayName },
        }),
      );
      if (res.ok) {
        signedIn(res.data);
      }
    },

    logout: async () => {
      await api.DELETE('/api/v1/auth/session');
      get().signedOutLocally();
    },

    signedOutLocally: () => {
      setCsrfToken(null);
      markSignedIn(null);
      set({ user: null, tenantId: null, idleTimeoutSeconds: null, error: null });
    },

    fetchMe: async () => {
      const { data, error, response } = await api.GET('/api/v1/users/me');
      if (error) {
        if (response.status === 401) {
          get().signedOutLocally();
        }
        return;
      }
      set({ user: data });
    },

    hydrate: async () => {
      const { data, error } = await api.GET('/api/v1/auth/session');
      if (error) {
        get().signedOutLocally();
      } else {
        signedIn(data);
      }
      set({ hydrated: true });
    },
  };
});
