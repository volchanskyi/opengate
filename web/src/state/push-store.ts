import { create } from 'zustand';
import { api } from '../lib/api';
import { apiAction } from './api-action';

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
    const res = await apiAction(set, () => api.GET('/api/v1/push/vapid-key'), false);
    if (res.ok) set({ vapidKey: res.data.public_key });
  },

  subscribe: async (endpoint, p256dh, auth) => {
    const res = await apiAction(set, () =>
      api.POST('/api/v1/push/subscribe', { body: { endpoint, p256dh, auth } }), false,
    );
    if (res.ok) set({ isSubscribed: true });
  },

  unsubscribe: async (endpoint) => {
    const res = await apiAction(set, () =>
      api.DELETE('/api/v1/push/subscribe', { body: { endpoint } }), false,
    );
    if (res.ok) set({ isSubscribed: false });
  },
}));
