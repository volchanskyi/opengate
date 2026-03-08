import { create } from 'zustand';
import { api } from '../lib/api';

interface PushState {
  vapidKey: string | null;
  isSubscribed: boolean;
  error: string | null;
  fetchVapidKey: () => Promise<void>;
  syncSubscriptionStatus: () => Promise<void>;
  subscribe: (endpoint: string, p256dh: string, auth: string) => Promise<void>;
  unsubscribe: (endpoint: string) => Promise<void>;
}

export const usePushStore = create<PushState>((set) => ({
  vapidKey: null,
  isSubscribed: false,
  error: null,

  syncSubscriptionStatus: async () => {
    if (!('serviceWorker' in navigator) || !('PushManager' in globalThis)) return;
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    set({ isSubscribed: !!sub });
  },

  fetchVapidKey: async () => {
    const { data, error } = await api.GET('/api/v1/push/vapid-key');
    if (error) {
      set({ error: error.error });
      return;
    }
    set({ vapidKey: data.public_key });
  },

  subscribe: async (endpoint, p256dh, auth) => {
    set({ error: null });
    const { error } = await api.POST('/api/v1/push/subscribe', {
      body: { endpoint, p256dh, auth },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set({ isSubscribed: true });
  },

  unsubscribe: async (endpoint) => {
    set({ error: null });
    const { error } = await api.DELETE('/api/v1/push/subscribe', {
      body: { endpoint },
    });
    if (error) {
      set({ error: error.error });
      return;
    }
    set({ isSubscribed: false });
  },
}));
